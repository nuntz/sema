package store

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nuntz/sema/internal/domain"
)

func putUnreadTestItems(t *testing.T, ctx context.Context, repository *Store, scores map[string]float64) {
	t.Helper()
	putIntegrationFeed(t, ctx, repository, "user", "feed")
	now := time.Now().UTC()
	for id, score := range scores {
		written, err := repository.PutItem(ctx, domain.Item{
			PK: domain.UserPK("user"), SK: domain.ItemSK(now, id), FeedPK: "F#feed", ItemID: id, FeedID: "feed",
			URL: "https://example.com/" + id, Title: id, PublishedTS: domain.Timestamp(now), FetchedTS: domain.Timestamp(now),
			Score: score, Size: "M", TTL: now.Add(domain.Retention).Unix(),
		})
		if err != nil || !written {
			t.Fatalf("put %s = %v, %v", id, written, err)
		}
	}
}

func unreadInterestPage(t *testing.T, ctx context.Context, repository *Store, cursor string, limit int) ([]string, string, QueryStats) {
	t.Helper()
	ctx, stats := WithQueryStats(ctx)
	items, next, _, err := repository.ItemsForFeeds(ctx, "user", domain.OrderInterest, cursor, limit, false, false, nil, nil, domain.FetchWindow{}, nil)
	if err != nil {
		t.Fatalf("unread interest page: %v", err)
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ItemID)
	}
	return ids, next, *stats
}

func TestReadStateWritersKeepUnreadIndexInStep(t *testing.T) {
	ctx, repository := newIntegrationStore(t)
	putUnreadTestItems(t, ctx, repository, map[string]float64{"a": 0.9, "b": 0.7, "c": 0.5, "d": 0.3, "e": 0.1})
	if err := repository.SetRead(ctx, "user", []string{"a", "c", "d"}, true); err != nil {
		t.Fatal(err)
	}
	if err := repository.SetRead(ctx, "user", []string{"c"}, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.SetHeart(ctx, "user", "b", true); err != nil {
		t.Fatal(err)
	}
	if audit, err := repository.ReconcileUnreadMembership(ctx, "user", false); err != nil || audit != (UnreadMembership{Live: 5}) {
		t.Fatalf("audit = %+v, %v", audit, err)
	}
}

func TestUnreadInterestPageScansOnlyUnreadItems(t *testing.T) {
	ctx, repository := newIntegrationStore(t)
	putUnreadTestItems(t, ctx, repository, map[string]float64{"a": 0.9, "b": 0.7, "c": 0.5, "d": 0.3, "e": 0.1})
	if err := repository.SetRead(ctx, "user", []string{"a", "c", "d"}, true); err != nil {
		t.Fatal(err)
	}

	ids, cursor, stats := unreadInterestPage(t, ctx, repository, "", 20)
	if len(ids) != 2 || ids[0] != "b" || ids[1] != "e" || cursor != "" {
		t.Fatalf("unread page = %v, cursor %q", ids, cursor)
	}
	if stats.Queries != 1 || stats.Scanned != 2 {
		t.Fatalf("stats = %+v, want 1 query scanning 2 rows", stats)
	}
}

func TestMarkUnreadRestoresItemAtItsScore(t *testing.T) {
	ctx, repository := newIntegrationStore(t)
	putUnreadTestItems(t, ctx, repository, map[string]float64{"a": 0.9, "b": 0.5, "c": 0.1})
	if err := repository.SetRead(ctx, "user", []string{"a", "b"}, true); err != nil {
		t.Fatal(err)
	}
	if err := repository.SetRead(ctx, "user", []string{"b"}, false); err != nil {
		t.Fatal(err)
	}
	if ids, _, _ := unreadInterestPage(t, ctx, repository, "", 20); len(ids) != 2 || ids[0] != "b" || ids[1] != "c" {
		t.Fatalf("after unread = %v", ids)
	}
	markers, err := repository.LoadReadMarkers(ctx, "user")
	if err != nil || len(markers) != 1 || !markers["a"] {
		t.Fatalf("markers = %v, %v", markers, err)
	}
}

func TestMarkUnreadDoesNotRecreateVanishedLiveRow(t *testing.T) {
	ctx, repository := newIntegrationStore(t)
	putUnreadTestItems(t, ctx, repository, map[string]float64{"gone": 0.5})
	if err := repository.SetRead(ctx, "user", []string{"gone"}, true); err != nil {
		t.Fatal(err)
	}
	live, err := repository.LiveItems(ctx, "user")
	if err != nil || len(live) != 1 {
		t.Fatalf("live = %#v, %v", live, err)
	}
	if _, err := repository.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(repository.table), Key: key(live[0].PK, live[0].SK),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.SetRead(ctx, "user", []string{"gone"}, false); err != nil {
		t.Fatal(err)
	}
	row, err := repository.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(repository.table), Key: key(live[0].PK, live[0].SK), ConsistentRead: aws.Bool(true),
	})
	if err != nil || len(row.Item) != 0 {
		t.Fatalf("vanished row = %#v, %v", row.Item, err)
	}
	if markers, err := repository.LoadReadMarkers(ctx, "user"); err != nil || len(markers) != 0 {
		t.Fatalf("markers = %v, %v", markers, err)
	}
}

func TestKeptCopyStaysOutOfUnreadPage(t *testing.T) {
	ctx, repository := newIntegrationStore(t)
	putUnreadTestItems(t, ctx, repository, map[string]float64{"kept": 0.8, "other": 0.2})
	if _, _, err := repository.SetHeart(ctx, "user", "kept", true); err != nil {
		t.Fatal(err)
	}
	if ids, _, stats := unreadInterestPage(t, ctx, repository, "", 20); len(ids) != 2 || ids[0] != "kept" || ids[1] != "other" || stats.Scanned != 2 {
		t.Fatalf("unread page = %v, stats %+v", ids, stats)
	}
	if archived, err := repository.ArchiveItem(ctx, "user", "kept"); err != nil || archived.UnreadPK != "" {
		t.Fatalf("kept copy = %+v, %v", archived, err)
	}
}

func TestUnreadInterestCursorWalksTiesOnce(t *testing.T) {
	ctx, repository := newIntegrationStore(t)
	putUnreadTestItems(t, ctx, repository, map[string]float64{"t1": 0.5, "t2": 0.5, "t3": 0.5, "low": 0.1})
	seen := map[string]int{}
	cursor := ""
	for page := 0; page < 10; page++ {
		ids, next, _ := unreadInterestPage(t, ctx, repository, cursor, 1)
		for _, id := range ids {
			seen[id]++
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if len(seen) != 4 || seen["t1"] != 1 || seen["t2"] != 1 || seen["t3"] != 1 || seen["low"] != 1 {
		t.Fatalf("seen = %v", seen)
	}
}

func TestUnreadInterestCursorSurvivesReadingItsItem(t *testing.T) {
	ctx, repository := newIntegrationStore(t)
	putUnreadTestItems(t, ctx, repository, map[string]float64{"a": 0.9, "b": 0.5, "c": 0.1})
	first, cursor, _ := unreadInterestPage(t, ctx, repository, "", 1)
	if len(first) != 1 || first[0] != "a" || cursor == "" {
		t.Fatalf("first = %v, cursor %q", first, cursor)
	}
	if err := repository.SetRead(ctx, "user", []string{"a"}, true); err != nil {
		t.Fatal(err)
	}
	if rest, next, _ := unreadInterestPage(t, ctx, repository, cursor, 20); len(rest) != 2 || rest[0] != "b" || rest[1] != "c" || next != "" {
		t.Fatalf("rest = %v, next %q", rest, next)
	}
}

func TestUnreadInterestResumesByScoreCursor(t *testing.T) {
	ctx, repository := newIntegrationStore(t)
	putUnreadTestItems(t, ctx, repository, map[string]float64{"a": 0.9, "b": 0.5, "c": 0.1})
	// A cursor issued by the by-score query before the switch carries PK, SK
	// and score only.
	all, _, _, err := repository.ItemsForFeeds(ctx, "user", domain.OrderInterest, "", 1, true, false, nil, nil, domain.FetchWindow{}, nil)
	if err != nil || len(all) != 1 || all[0].ItemID != "a" {
		t.Fatalf("by-score page = %#v, %v", all, err)
	}
	score := all[0].Score
	legacy, err := encodeCursor(map[string]types.AttributeValue{
		"PK": &types.AttributeValueMemberS{Value: all[0].PK}, "SK": &types.AttributeValueMemberS{Value: all[0].SK},
		"score": &types.AttributeValueMemberN{Value: strconv.FormatFloat(score, 'g', -1, 64)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rest, _, _ := unreadInterestPage(t, ctx, repository, legacy, 20); len(rest) != 2 || rest[0] != "b" || rest[1] != "c" {
		t.Fatalf("resumed = %v", rest)
	}
}

func TestReconcileUnreadMembershipRepairsPreDeployRows(t *testing.T) {
	ctx, repository := newIntegrationStore(t)
	putUnreadTestItems(t, ctx, repository, map[string]float64{"a": 0.9, "b": 0.5, "c": 0.1})
	if err := repository.SetRead(ctx, "user", []string{"b"}, true); err != nil {
		t.Fatal(err)
	}
	live, err := repository.LiveItems(ctx, "user")
	if err != nil {
		t.Fatal(err)
	}
	// Rows written before deploy 1 have no flag; a stale flag survives on b.
	for _, item := range live {
		expression := "REMOVE unread_pk"
		var values map[string]types.AttributeValue
		if item.ItemID == "b" {
			expression = "SET unread_pk = :pk"
			values = map[string]types.AttributeValue{":pk": &types.AttributeValueMemberS{Value: item.PK}}
		}
		if _, err := repository.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
			TableName: aws.String(repository.table), Key: key(item.PK, item.SK),
			UpdateExpression: aws.String(expression), ExpressionAttributeValues: values,
		}); err != nil {
			t.Fatal(err)
		}
	}

	dry, err := repository.ReconcileUnreadMembership(ctx, "user", false)
	if err != nil || dry != (UnreadMembership{Live: 3, Missing: 2, Surplus: 1}) {
		t.Fatalf("dry run = %+v, %v", dry, err)
	}
	if again, err := repository.ReconcileUnreadMembership(ctx, "user", false); err != nil || again != dry {
		t.Fatalf("dry run wrote flags: %+v, %v", again, err)
	}
	applied, err := repository.ReconcileUnreadMembership(ctx, "user", true)
	if err != nil || applied != (UnreadMembership{Live: 3, Missing: 2, Surplus: 1, Repaired: 3}) {
		t.Fatalf("apply = %+v, %v", applied, err)
	}
	audit, err := repository.ReconcileUnreadMembership(ctx, "user", true)
	if err != nil || audit != (UnreadMembership{Live: 3}) {
		t.Fatalf("audit after apply = %+v, %v", audit, err)
	}
	if ids, _, stats := unreadInterestPage(t, ctx, repository, "", 20); len(ids) != 2 || ids[0] != "a" || ids[1] != "c" || stats.Scanned != 2 {
		t.Fatalf("after apply = %v, stats %+v", ids, stats)
	}
}

func TestReconcileUnreadMembershipTreatsExpiredMarkerAsUnread(t *testing.T) {
	ctx, repository := newIntegrationStore(t)
	putUnreadTestItems(t, ctx, repository, map[string]float64{"reingested": 0.5})
	live, err := repository.LiveItems(ctx, "user")
	if err != nil || len(live) != 1 {
		t.Fatalf("live = %#v, %v", live, err)
	}
	// A marker from the item's previous life outlives its TTL until DynamoDB
	// deletes it; a pre-deploy row also lacks the flag.
	marker, err := attributevalue.MarshalMap(domain.Read{PK: live[0].PK, SK: domain.ReadSK("reingested"), ReadAt: domain.Timestamp(time.Now()), TTL: time.Now().Add(-time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(repository.table), Item: marker}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(repository.table), Key: key(live[0].PK, live[0].SK), UpdateExpression: aws.String("REMOVE unread_pk"),
	}); err != nil {
		t.Fatal(err)
	}

	applied, err := repository.ReconcileUnreadMembership(ctx, "user", true)
	if err != nil || applied != (UnreadMembership{Live: 1, Missing: 1, Repaired: 1}) {
		t.Fatalf("apply = %+v, %v", applied, err)
	}
	if audit, err := repository.ReconcileUnreadMembership(ctx, "user", false); err != nil || audit != (UnreadMembership{Live: 1}) {
		t.Fatalf("audit = %+v, %v", audit, err)
	}
}
