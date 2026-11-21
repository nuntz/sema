package main

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/dynamotest"
	"github.com/nuntz/sema/internal/store"
)

func TestRemovalDryRunApplyAndRepeat(t *testing.T) {
	db, table := dynamotest.NewTable(t)
	ctx := context.Background()
	repository := store.New(db, nil, table, "", "")
	now := time.Now().UTC()
	for _, feed := range []domain.Feed{
		{FeedID: "golang", Connector: domain.ConnectorReddit, URL: "https://www.reddit.com/r/golang/.rss", Title: "r/golang", CustomTitle: "Go threads", FaviconKey: "favicons/golang.png"},
		{FeedID: "blog", Connector: domain.ConnectorRSS, URL: "https://example.com/feed", Title: "Example blog", FaviconKey: "favicons/blog.png"},
	} {
		feed.PK, feed.SK, feed.NextFetchAt = domain.UserPK("user"), domain.FeedSK(feed.FeedID), domain.Timestamp(now)
		if err := repository.PutFeed(ctx, feed); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []domain.Item{
		// Ingested before the Reddit Connector existed, under the Feed's original title.
		{ItemID: "stale", FeedID: "golang", Title: "Generics in practice", FeedTitle: "r/golang"},
		{ItemID: "frozen", FeedID: "golang", Title: "Iterators", Connector: domain.ConnectorReddit, FeedTitle: "Go threads", FaviconKey: "favicons/golang.png"},
		{ItemID: "article", FeedID: "blog", Title: "An article", Connector: domain.ConnectorRSS, FeedTitle: "Old blog title"},
	} {
		item.PK, item.SK, item.HeartedTS = domain.UserPK("user"), domain.ArchiveSK(now, item.ItemID), domain.Timestamp(now)
		row, err := attributevalue.MarshalMap(item)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(table), Item: row}); err != nil {
			t.Fatal(err)
		}
	}
	kept := func() map[string]domain.Item {
		t.Helper()
		items, err := repository.ArchiveItems(ctx, "user")
		if err != nil {
			t.Fatal(err)
		}
		byID := make(map[string]domain.Item, len(items))
		for _, item := range items {
			byID[item.ItemID] = item
		}
		return byID
	}
	feedIDs := func() []string {
		t.Helper()
		feeds, err := repository.Feeds(ctx, "user")
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, feed := range feeds {
			ids = append(ids, feed.FeedID)
		}
		return ids
	}
	feedVersion := func() int64 {
		t.Helper()
		version, err := repository.FeedVersion(ctx, "user")
		if err != nil {
			t.Fatal(err)
		}
		return version
	}
	before := kept()

	var output bytes.Buffer
	counts, err := run(ctx, repository, false, &output)
	if err != nil || counts != (removal{FeedsRemoved: 1, KeptItemsFrozen: 1}) {
		t.Fatalf("dry run = %+v %v", counts, err)
	}
	if !strings.Contains(output.String(), "user=user reddit_feeds_removed=1 kept_items_frozen=1 apply=false") {
		t.Fatalf("dry run output = %s", output.String())
	}
	if got := feedIDs(); !reflect.DeepEqual(got, []string{"blog", "golang"}) || feedVersion() != 0 || !reflect.DeepEqual(kept(), before) {
		t.Fatalf("dry run wrote: feeds=%v version=%d", got, feedVersion())
	}

	output.Reset()
	counts, err = run(ctx, repository, true, &output)
	if err != nil || counts != (removal{FeedsRemoved: 1, KeptItemsFrozen: 1}) {
		t.Fatalf("apply = %+v %v", counts, err)
	}
	if !strings.Contains(output.String(), "user=user reddit_feeds_removed=1 kept_items_frozen=1 apply=true") {
		t.Fatalf("apply output = %s", output.String())
	}
	after := kept()
	want := before["stale"]
	want.Connector, want.FeedTitle, want.FaviconKey = domain.ConnectorReddit, "Go threads", "favicons/golang.png"
	if !reflect.DeepEqual(after["stale"], want) {
		t.Fatalf("stale = %+v, want %+v", after["stale"], want)
	}
	for _, id := range []string{"frozen", "article"} {
		if !reflect.DeepEqual(after[id], before[id]) {
			t.Fatalf("%s = %+v, want %+v", id, after[id], before[id])
		}
	}
	if got := feedIDs(); !reflect.DeepEqual(got, []string{"blog"}) || feedVersion() != 1 {
		t.Fatalf("apply left feeds=%v version=%d", got, feedVersion())
	}

	output.Reset()
	counts, err = run(ctx, repository, true, &output)
	if err != nil || counts != (removal{}) {
		t.Fatalf("repeat = %+v %v", counts, err)
	}
	if got := feedIDs(); !reflect.DeepEqual(got, []string{"blog"}) || feedVersion() != 1 || !reflect.DeepEqual(kept(), after) {
		t.Fatalf("repeat wrote: feeds=%v version=%d", got, feedVersion())
	}
}
