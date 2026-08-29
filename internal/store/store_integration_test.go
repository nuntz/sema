package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/nuntz/sema/internal/domain"
)

// newIntegrationStore creates an isolated table on DynamoDB Local and returns a
// store bound to it. The table is dropped when the test finishes.
func newIntegrationStore(t *testing.T) (context.Context, *Store) {
	t.Helper()
	endpoint := os.Getenv("DYNAMODB_ENDPOINT")
	if endpoint == "" {
		t.Skip("DYNAMODB_ENDPOINT is not configured")
	}
	ctx := context.Background()
	awsConfig, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		config.WithBaseEndpoint(endpoint),
	)
	if err != nil {
		t.Fatal(err)
	}
	db := dynamodb.NewFromConfig(awsConfig)
	table := fmt.Sprintf("sema-test-%d", time.Now().UnixNano())
	_, err = db.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:   aws.String(table),
		BillingMode: types.BillingModePayPerRequest,
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("PK"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("SK"), KeyType: types.KeyTypeRange},
		},
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("PK"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("SK"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("score"), AttributeType: types.ScalarAttributeTypeN},
			{AttributeName: aws.String("feed_pk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("published_ts"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("gsi1pk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("next_fetch_at"), AttributeType: types.ScalarAttributeTypeS},
		},
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
			{IndexName: aws.String("by-score"), KeySchema: []types.KeySchemaElement{{AttributeName: aws.String("PK"), KeyType: types.KeyTypeHash}, {AttributeName: aws.String("score"), KeyType: types.KeyTypeRange}}, Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll}},
			{IndexName: aws.String("by-feed"), KeySchema: []types.KeySchemaElement{{AttributeName: aws.String("feed_pk"), KeyType: types.KeyTypeHash}, {AttributeName: aws.String("published_ts"), KeyType: types.KeyTypeRange}}, Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll}},
			{IndexName: aws.String("by-next-fetch"), KeySchema: []types.KeySchemaElement{{AttributeName: aws.String("gsi1pk"), KeyType: types.KeyTypeHash}, {AttributeName: aws.String("next_fetch_at"), KeyType: types.KeyTypeRange}}, Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.DeleteTable(context.Background(), &dynamodb.DeleteTableInput{TableName: aws.String(table)})
	})
	if err := dynamodb.NewTableExistsWaiter(db).Wait(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(table)}, 10*time.Second); err != nil {
		t.Fatal(err)
	}

	return ctx, New(db, nil, table, "", "")
}

func putIntegrationFeed(t *testing.T, ctx context.Context, repository *Store, userID, feedID string) {
	t.Helper()
	if err := repository.PutFeed(ctx, domain.Feed{
		PK: domain.UserPK(userID), SK: domain.FeedSK(feedID), FeedID: feedID,
		URL: "https://example.com/feed", NextFetchAt: domain.Timestamp(time.Now()),
	}); err != nil {
		t.Fatalf("put feed: %v", err)
	}
}

func TestDynamoAccessPatterns(t *testing.T) {
	ctx, repository := newIntegrationStore(t)
	if err := repository.EnsureUser(ctx, "user", "reader@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(repository.table), Key: key(domain.UserPK("user"), "PROFILE"),
		UpdateExpression: aws.String("SET read_boundary_ts = :boundary"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":boundary": &types.AttributeValueMemberS{Value: domain.Timestamp(time.Now())},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.EnsureUser(ctx, "user", "reader@example.com"); err != nil {
		t.Fatal(err)
	}
	putIntegrationFeed(t, ctx, repository, "user", "feed")
	profile, err := repository.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(repository.table), Key: key(domain.UserPK("user"), "PROFILE"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := profile.Item["read_boundary_ts"]; exists {
		t.Fatal("read_boundary_ts was not removed from profile")
	}
	user, err := repository.User(ctx, "user")
	if err != nil || user.OrderPref != domain.OrderInterest {
		t.Fatalf("profile = %#v, %v", user, err)
	}
	now := time.Now().UTC()
	items := []domain.Item{
		{PK: domain.UserPK("user"), SK: domain.ItemSK(now.Add(-time.Hour), "old"), FeedPK: "F#feed", ItemID: "old", FeedID: "feed", URL: "https://example.com/old", Title: "Old", SearchText: "old story", PublishedTS: domain.Timestamp(now.Add(-time.Hour)), FetchedTS: domain.Timestamp(now), Score: 0.2, Size: "S", TTL: now.Add(domain.Retention).Unix(), Vector: []byte{1, 2}},
		{PK: domain.UserPK("user"), SK: domain.ItemSK(now, "new"), FeedPK: "F#feed", ItemID: "new", FeedID: "feed", URL: "https://example.com/new", Title: "New", SearchText: "new story", PublishedTS: domain.Timestamp(now), FetchedTS: domain.Timestamp(now), Score: 0.9, Size: "L", TTL: now.Add(domain.Retention).Unix(), Vector: []byte{3, 4}},
		{PK: domain.UserPK("user"), SK: domain.ItemSK(now.Add(-8*24*time.Hour), "expired"), FeedPK: "F#feed", ItemID: "expired", FeedID: "feed", URL: "https://example.com/expired", Title: "Expired", PublishedTS: domain.Timestamp(now.Add(-8 * 24 * time.Hour)), FetchedTS: domain.Timestamp(now), Score: 1, Size: "L", TTL: now.Add(-time.Hour).Unix()},
	}
	for _, item := range items {
		written, putErr := repository.PutItem(ctx, item)
		if putErr != nil || !written {
			t.Fatalf("put item = %v, %v", written, putErr)
		}
	}
	if written, err := repository.PutItem(ctx, items[0]); err != nil || written {
		t.Fatalf("conditional dedupe = %v, %v", written, err)
	}
	live, err := repository.LiveItems(ctx, "user")
	if err != nil || len(live) != 2 {
		t.Fatalf("live items = %#v, %v", live, err)
	}
	if err := repository.LoadItemVectors(ctx, "user", live); err != nil {
		t.Fatalf("load live item vectors: %v", err)
	}
	for _, item := range live {
		if len(item.Vector) == 0 || item.SearchText == "" {
			t.Fatalf("live item lost full-row fields: %#v", item)
		}
	}
	republished := items[0]
	republished.SK = domain.ItemSK(now.Add(30*time.Second), republished.ItemID)
	republished.PublishedTS = domain.Timestamp(now.Add(30 * time.Second))
	if written, err := repository.PutItem(ctx, republished); err != nil || written {
		t.Fatalf("stable identity dedupe = %v, %v", written, err)
	}
	chrono, _, err := repository.Items(ctx, "user", domain.OrderChrono, "", 100, true)
	if err != nil || len(chrono) != 2 || chrono[0].ItemID != "new" {
		t.Fatalf("chrono = %#v, %v", chrono, err)
	}
	interest, _, err := repository.Items(ctx, "user", domain.OrderInterest, "", 100, true)
	if err != nil || len(interest) != 2 || interest[0].ItemID != "new" {
		t.Fatalf("interest = %#v, %v", interest, err)
	}
	for _, order := range []domain.Order{domain.OrderChrono, domain.OrderInterest} {
		first, cursor, _, err := repository.ItemsForFeeds(ctx, "user", order, "", 1, false, nil)
		if err != nil || len(first) != 1 || first[0].ItemID != "new" || cursor == "" {
			t.Fatalf("first unread page (%s) = %#v, cursor %q, %v", order, first, cursor, err)
		}
		if len(first[0].Vector) != 0 || first[0].SearchText != "" {
			t.Fatalf("first unread page (%s) included excluded fields: %#v", order, first[0])
		}
		second, cursor, _, err := repository.ItemsForFeeds(ctx, "user", order, cursor, 1, false, nil)
		if err != nil || len(second) != 1 || second[0].ItemID != "old" || cursor != "" {
			t.Fatalf("second unread page (%s) = %#v, cursor %q, %v", order, second, cursor, err)
		}
		if len(second[0].Vector) != 0 || second[0].SearchText != "" {
			t.Fatalf("second unread page (%s) included excluded fields: %#v", order, second[0])
		}
	}
	if err := repository.SetRead(ctx, "user", []string{"old"}, true); err != nil {
		t.Fatal(err)
	}
	if err := repository.ResolveRead(ctx, "user", chrono); err != nil || chrono[0].Read || !chrono[1].Read {
		t.Fatalf("read state = %#v, %v", chrono, err)
	}
	unread, _, err := repository.Items(ctx, "user", domain.OrderChrono, "", 100, false)
	if err != nil || len(unread) != 1 || unread[0].ItemID != "new" || unread[0].Read {
		t.Fatalf("unread items = %#v, %v", unread, err)
	}
	if err := repository.SetRead(ctx, "user", []string{"old"}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.SetRead(ctx, "user", []string{"new"}, true); err != nil {
		t.Fatal(err)
	}
	for _, order := range []domain.Order{domain.OrderChrono, domain.OrderInterest} {
		unread, cursor, err := repository.Items(ctx, "user", order, "", 1, false)
		if err != nil || len(unread) != 1 || unread[0].ItemID != "old" || cursor != "" {
			t.Fatalf("read-heavy page (%s) = %#v, cursor %q, %v", order, unread, cursor, err)
		}
	}
	if err := repository.SetSignal(ctx, "user", items[1], 1); err != nil {
		t.Fatal(err)
	}
	signals, err := repository.Signals(ctx, "user")
	if err != nil || len(signals) != 1 || signals[0].Value != 1 {
		t.Fatalf("signals = %#v, %v", signals, err)
	}
	values, err := repository.SignalValues(ctx, "user", []string{"new", "missing"})
	if err != nil || len(values) != 1 || values["new"] != 1 {
		t.Fatalf("signal values = %#v, %v", values, err)
	}
	assertUserCounts(t, repository, "user", 0, 1)
}

func TestHeartArchiveLifecycle(t *testing.T) {
	ctx, repository := newIntegrationStore(t)
	if err := repository.EnsureUser(ctx, "keeper", "keeper@example.com"); err != nil {
		t.Fatal(err)
	}
	putIntegrationFeed(t, ctx, repository, "keeper", "feed")
	objects := &archiveObjectStore{objects: map[string]bool{}}
	repository.s3 = objects
	repository.bucket = "content"
	repository.contentURL = "/content"
	now := time.Now().UTC()
	item := domain.Item{
		PK: domain.UserPK("keeper"), SK: domain.ItemSK(now, "kept"), FeedPK: "F#feed", ItemID: "kept", FeedID: "feed",
		FeedTitle: "A Feed", FaviconKey: FaviconKey("feed"), URL: "https://example.com/kept", Title: "Kept",
		Summary: "A summary", PublishedTS: domain.Timestamp(now), FetchedTS: domain.Timestamp(now),
		BodyKey: BodyKey("keeper", "kept"), HasBody: true, MediaKey: MediaKey("keeper", "kept", ".webp"), MediaW: 1200, MediaH: 800,
		Score: 0.7, Size: "L", Vector: []byte{1, 2, 3}, TTL: now.Add(domain.Retention).Unix(),
	}
	item.MediaVariants = []domain.MediaVariant{
		{Key: MediaVariantKey(item.MediaKey, 384), Width: 384, Height: 256},
		{Key: item.MediaKey, Width: 1200, Height: 800},
	}
	objects.objects[item.BodyKey] = true
	objects.objects[item.MediaKey] = true
	objects.objects[item.MediaVariants[0].Key] = true
	if written, err := repository.PutItem(ctx, item); err != nil || !written {
		t.Fatalf("put item = %v, %v", written, err)
	}

	archiveSK, count, err := repository.SetHeart(ctx, "keeper", item.ItemID, true)
	if err != nil || archiveSK == "" || count != 1 {
		t.Fatalf("heart = %q, %d, %v", archiveSK, count, err)
	}
	assertUserCounts(t, repository, "keeper", 1, 1)
	archiveVariantKey := MediaVariantKey(ArchiveMediaKey("keeper", "kept"), 384)
	if !objects.objects[ArchiveBodyKey("keeper", "kept")] || !objects.objects[ArchiveMediaKey("keeper", "kept")] || !objects.objects[archiveVariantKey] {
		t.Fatalf("archive objects = %#v", objects.objects)
	}
	archived, err := repository.ArchiveItem(ctx, "keeper", item.ItemID)
	if err != nil || archived.SK != archiveSK || archived.TTL != 0 || !archived.HasBody || archived.BodyKey != ArchiveBodyKey("keeper", "kept") || archived.MediaKey != ArchiveMediaKey("keeper", "kept") || len(archived.MediaVariants) != 2 || archived.MediaVariants[0].Key != archiveVariantKey {
		t.Fatalf("archive item = %#v, %v", archived, err)
	}
	raw, err := repository.db.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(repository.table), Key: key(domain.UserPK("keeper"), archiveSK)})
	if err != nil {
		t.Fatal(err)
	}
	if _, hasTTL := raw.Item["ttl"]; hasTTL {
		t.Fatal("archive row unexpectedly has ttl")
	}
	if _, err := repository.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(repository.table), Key: key(domain.UserPK("keeper"), item.SK),
		UpdateExpression:          aws.String("SET #ttl = :expired"),
		ExpressionAttributeNames:  map[string]string{"#ttl": "ttl"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":expired": &types.AttributeValueMemberN{Value: fmt.Sprint(now.Add(-time.Hour).Unix())}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Item(ctx, "keeper", item.ItemID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired live item = %v", err)
	}
	if durable, err := repository.ArchiveItem(ctx, "keeper", item.ItemID); err != nil || durable.BodyKey != ArchiveBodyKey("keeper", "kept") {
		t.Fatalf("archive after live expiry = %#v, %v", durable, err)
	}
	resolved, err := repository.ResolveItemIDs(ctx, "keeper", []string{item.ItemID, item.ItemID, "missing"})
	if err != nil || len(resolved) != 1 || resolved[0].SK != archiveSK || !resolved[0].Archived || !resolved[0].Hearted || resolved[0].Read {
		t.Fatalf("resolve expired live item = %#v, %v", resolved, err)
	}
	if _, err := repository.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(repository.table), Key: key(domain.UserPK("keeper"), item.SK),
		UpdateExpression:          aws.String("SET #ttl = :active"),
		ExpressionAttributeNames:  map[string]string{"#ttl": "ttl"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":active": &types.AttributeValueMemberN{Value: fmt.Sprint(item.TTL)}},
	}); err != nil {
		t.Fatal(err)
	}
	live, err := repository.Item(ctx, "keeper", item.ItemID)
	if err != nil || live.ArchiveSK != archiveSK {
		t.Fatalf("live archive pointer = %#v, %v", live, err)
	}
	signals, err := repository.Signals(ctx, "keeper")
	if err != nil || len(signals) != 1 || signals[0].Value != 1 || signals[0].Source != "heart" {
		t.Fatalf("heart signals = %#v, %v", signals, err)
	}
	if err := repository.SetSignal(ctx, "keeper", live, 1); err != nil {
		t.Fatal(err)
	}
	signals, err = repository.Signals(ctx, "keeper")
	if err != nil || len(signals) != 1 || signals[0].Source != "" {
		t.Fatalf("explicit signal did not replace heart source = %#v, %v", signals, err)
	}
	assertUserCounts(t, repository, "keeper", 1, 1)
	if err := repository.SetSignal(ctx, "keeper", live, 0); err != nil {
		t.Fatal(err)
	}
	signals, err = repository.Signals(ctx, "keeper")
	if err != nil || len(signals) != 1 || signals[0].Value != 1 || signals[0].Source != "heart" {
		t.Fatalf("cleared explicit signal did not restore heart = %#v, %v", signals, err)
	}
	assertUserCounts(t, repository, "keeper", 1, 1)
	secondSK, count, err := repository.SetHeart(ctx, "keeper", item.ItemID, true)
	if err != nil || secondSK != archiveSK || count != 1 {
		t.Fatalf("second heart = %q, %d, %v", secondSK, count, err)
	}

	if err := repository.SetSignal(ctx, "keeper", item, -1); err != nil {
		t.Fatal(err)
	}
	if _, count, err = repository.SetHeart(ctx, "keeper", item.ItemID, false); err != nil || count != 0 {
		t.Fatalf("unheart = %d, %v", count, err)
	}
	assertUserCounts(t, repository, "keeper", 0, 1)
	if _, err := repository.ArchiveItem(ctx, "keeper", item.ItemID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("archive after unheart = %v", err)
	}
	if objects.objects[ArchiveBodyKey("keeper", "kept")] || objects.objects[ArchiveMediaKey("keeper", "kept")] || objects.objects[archiveVariantKey] {
		t.Fatalf("objects survived unheart = %#v", objects.objects)
	}
	live, err = repository.Item(ctx, "keeper", item.ItemID)
	if err != nil || live.ArchiveSK != "" {
		t.Fatalf("live pointer after unheart = %#v, %v", live, err)
	}
	signals, err = repository.Signals(ctx, "keeper")
	if err != nil || len(signals) != 1 || signals[0].Value != -1 || signals[0].Source != "" {
		t.Fatalf("explicit signal after unheart = %#v, %v", signals, err)
	}
	if _, count, err = repository.SetHeart(ctx, "keeper", item.ItemID, false); err != nil || count != 0 {
		t.Fatalf("second unheart = %d, %v", count, err)
	}
	assertUserCounts(t, repository, "keeper", 0, 1)
}

func TestHeartToleratesMissingContentAndRejectsExpiredItem(t *testing.T) {
	ctx, repository := newIntegrationStore(t)
	if err := repository.EnsureUser(ctx, "keeper", "keeper@example.com"); err != nil {
		t.Fatal(err)
	}
	putIntegrationFeed(t, ctx, repository, "keeper", "feed")
	repository.s3 = &archiveObjectStore{objects: map[string]bool{}}
	repository.bucket = "content"
	now := time.Now().UTC()
	missing := domain.Item{
		PK: domain.UserPK("keeper"), SK: domain.ItemSK(now, "missing"), FeedPK: "F#feed", ItemID: "missing", FeedID: "feed",
		URL: "https://example.com/missing", Title: "Missing", PublishedTS: domain.Timestamp(now), FetchedTS: domain.Timestamp(now),
		BodyKey: BodyKey("keeper", "missing"), HasBody: true, MediaKey: MediaKey("keeper", "missing", ".webp"),
		Score: 0.2, Size: "S", TTL: now.Add(domain.Retention).Unix(),
	}
	if written, err := repository.PutItem(ctx, missing); err != nil || !written {
		t.Fatalf("put missing item = %v, %v", written, err)
	}
	if _, _, err := repository.SetHeart(ctx, "keeper", missing.ItemID, true); err != nil {
		t.Fatal(err)
	}
	assertUserCounts(t, repository, "keeper", 1, 1)
	archived, err := repository.ArchiveItem(ctx, "keeper", missing.ItemID)
	if err != nil || archived.HasBody || archived.BodyKey != "" || archived.MediaKey != "" {
		t.Fatalf("missing-content archive = %#v, %v", archived, err)
	}

	expired := missing
	expired.ItemID = "expired"
	expired.SK = domain.ItemSK(now.Add(-8*24*time.Hour), expired.ItemID)
	expired.TTL = now.Add(-time.Hour).Unix()
	if written, err := repository.PutItem(ctx, expired); err != nil || !written {
		t.Fatalf("put expired item = %v, %v", written, err)
	}
	if _, _, err := repository.SetHeart(ctx, "keeper", expired.ItemID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("heart expired item = %v", err)
	}
}

func TestArchivePaginationIsNewestHeartFirst(t *testing.T) {
	ctx, repository := newIntegrationStore(t)
	if err := repository.EnsureUser(ctx, "keeper", "keeper@example.com"); err != nil {
		t.Fatal(err)
	}
	putIntegrationFeed(t, ctx, repository, "keeper", "feed")
	now := time.Now().UTC()
	for index, id := range []string{"first", "second", "third"} {
		published := now.Add(time.Duration(index) * time.Second)
		item := domain.Item{
			PK: domain.UserPK("keeper"), SK: domain.ItemSK(published, id), FeedPK: "F#feed", ItemID: id, FeedID: "feed",
			URL: "https://example.com/" + id, Title: id, PublishedTS: domain.Timestamp(published), FetchedTS: domain.Timestamp(now),
			SearchText: id + " story", Score: 0.5, Size: "M", Vector: []byte(id), TTL: now.Add(domain.Retention).Unix(),
		}
		if written, err := repository.PutItem(ctx, item); err != nil || !written {
			t.Fatalf("put %s = %v, %v", id, written, err)
		}
		if _, _, err := repository.SetHeart(ctx, "keeper", id, true); err != nil {
			t.Fatalf("heart %s: %v", id, err)
		}
	}
	first, cursor, err := repository.Archives(ctx, "keeper", "", 2)
	if err != nil || len(first) != 2 || first[0].ItemID != "third" || first[1].ItemID != "second" || cursor == "" {
		t.Fatalf("first archive page = %#v, cursor %q, %v", first, cursor, err)
	}
	for _, item := range first {
		if len(item.Vector) != 0 || item.SearchText != "" {
			t.Fatalf("first archive page included excluded fields: %#v", item)
		}
	}
	second, cursor, err := repository.Archives(ctx, "keeper", cursor, 2)
	if err != nil || len(second) != 1 || second[0].ItemID != "first" || cursor != "" {
		t.Fatalf("second archive page = %#v, cursor %q, %v", second, cursor, err)
	}
	if len(second[0].Vector) != 0 || second[0].SearchText != "" {
		t.Fatalf("second archive page included excluded fields: %#v", second[0])
	}
	assertUserCounts(t, repository, "keeper", 3, 3)
}

func assertUserCounts(t *testing.T, repository *Store, userID string, hearts, signals int) {
	t.Helper()
	user, err := repository.User(context.Background(), userID)
	if err != nil || user.HeartCount != hearts || user.SignalCount != signals {
		t.Fatalf("profile counts = hearts %d, signals %d, %v; want %d, %d", user.HeartCount, user.SignalCount, err, hearts, signals)
	}
}

type archiveObjectStore struct {
	objects map[string]bool
}

func (s *archiveObjectStore) CopyObject(_ context.Context, input *s3.CopyObjectInput, _ ...func(*s3.Options)) (*s3.CopyObjectOutput, error) {
	copySource, _ := url.PathUnescape(aws.ToString(input.CopySource))
	source := strings.TrimPrefix(copySource, aws.ToString(input.Bucket)+"/")
	if !s.objects[source] {
		return nil, &smithy.GenericAPIError{Code: "NoSuchKey", Message: "missing"}
	}
	s.objects[aws.ToString(input.Key)] = true
	return &s3.CopyObjectOutput{}, nil
}

func (s *archiveObjectStore) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	delete(s.objects, aws.ToString(input.Key))
	return &s3.DeleteObjectOutput{}, nil
}

func (s *archiveObjectStore) GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader(""))}, nil
}

func (s *archiveObjectStore) HeadObject(_ context.Context, input *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	if !s.objects[aws.ToString(input.Key)] {
		return nil, &smithy.GenericAPIError{Code: "NotFound", Message: "missing"}
	}
	return &s3.HeadObjectOutput{}, nil
}

func (s *archiveObjectStore) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	s.objects[aws.ToString(input.Key)] = true
	return &s3.PutObjectOutput{}, nil
}

// A table with no rows must still produce empty JSON arrays. The API encodes
// these slices straight into its responses, and Go renders a nil slice as
// null, which the client cannot consume.
func TestDynamoEmptyDatabase(t *testing.T) {
	ctx, repository := newIntegrationStore(t)

	for _, order := range []domain.Order{domain.OrderChrono, domain.OrderInterest} {
		items, cursor, err := repository.Items(ctx, "nobody", order, "", 100, false)
		if err != nil {
			t.Fatalf("items(%s) = %v", order, err)
		}
		if len(items) != 0 || cursor != "" {
			t.Fatalf("items(%s) = %#v, cursor %q", order, items, cursor)
		}
		assertEncodesAsArray(t, fmt.Sprintf("items(%s)", order), items)
	}

	feeds, err := repository.Feeds(ctx, "nobody")
	if err != nil || len(feeds) != 0 {
		t.Fatalf("feeds = %#v, %v", feeds, err)
	}
	assertEncodesAsArray(t, "feeds", feeds)

	signals, err := repository.Signals(ctx, "nobody")
	if err != nil || len(signals) != 0 {
		t.Fatalf("signals = %#v, %v", signals, err)
	}
	assertEncodesAsArray(t, "signals", signals)

	if _, err := repository.Item(ctx, "nobody", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("item = %v, want ErrNotFound", err)
	}
}

func TestDynamoRecordBehaviourEvents(t *testing.T) {
	ctx, repository := newIntegrationStore(t)
	now := time.Now().UTC()
	item := domain.Item{
		ItemID:       "event-item",
		FeedID:       "event-feed",
		Title:        "Event item",
		Vector:       []byte{1, 2, 3},
		ModelVersion: "test-model",
	}
	dwell := int64(31_000)
	if err := repository.RecordBehaviour(ctx, "reader", item, BehaviourEvent{
		Opened: true, DwellMS: &dwell, ClickedThrough: true, Shared: true,
	}); err != nil {
		t.Fatalf("record all behaviour events: %v", err)
	}

	row, err := repository.Behaviour(ctx, "reader", item.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if !row.Opened || !row.ClickedThrough || !row.Shared {
		t.Fatalf("behaviour flags = opened:%t clicked:%t shared:%t", row.Opened, row.ClickedThrough, row.Shared)
	}
	if row.DwellMS != dwell {
		t.Fatalf("dwell_ms = %d, want %d", row.DwellMS, dwell)
	}
	if row.ItemID != item.ItemID || row.FeedID != item.FeedID || row.Title != item.Title || row.ModelVersion != item.ModelVersion {
		t.Fatalf("behaviour metadata = %#v", row)
	}
	if string(row.Vector) != string(item.Vector) {
		t.Fatalf("vector = %v, want %v", row.Vector, item.Vector)
	}
	if row.TTL < now.Add(89*24*time.Hour).Unix() {
		t.Fatalf("ttl = %d, want roughly 90 days from now", row.TTL)
	}

	// A later partial event cannot clear flags or reduce dwell time. It also
	// exercises the common non-share expression, where #shared must be absent.
	lowerDwell := int64(10_000)
	if err := repository.RecordBehaviour(ctx, "reader", item, BehaviourEvent{DwellMS: &lowerDwell}); err != nil {
		t.Fatalf("record lower dwell: %v", err)
	}
	row, err = repository.Behaviour(ctx, "reader", item.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if row.DwellMS != dwell || !row.Opened || !row.ClickedThrough || !row.Shared {
		t.Fatalf("behaviour merge was not monotonic: %#v", row)
	}
}

func assertEncodesAsArray(t *testing.T, label string, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", label, err)
	}
	if string(encoded) != "[]" {
		t.Fatalf("%s encoded as %s, want []", label, encoded)
	}
}
