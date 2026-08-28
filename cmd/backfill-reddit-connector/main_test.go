package main

import (
	"context"
	"testing"

	"github.com/nuntz/sema/internal/domain"
)

type fakeRedditStore struct {
	feeds  []domain.Feed
	writes int
}

func (*fakeRedditStore) UserIDs(context.Context) ([]string, error) {
	return []string{"user"}, nil
}

func (f *fakeRedditStore) Feeds(context.Context, string) ([]domain.Feed, error) {
	return append([]domain.Feed(nil), f.feeds...), nil
}

func (f *fakeRedditStore) PutFeed(_ context.Context, feed domain.Feed) error {
	f.writes++
	for index := range f.feeds {
		if f.feeds[index].FeedID == feed.FeedID {
			f.feeds[index] = feed
		}
	}
	return nil
}

func TestBackfillRedditConnectorDryRunAndIdempotence(t *testing.T) {
	legacyURL := "https://old.reddit.com/r/Castles/top.rss?t=day"
	feedID := domain.FeedID(legacyURL)
	guid, itemURL := "t3_abc", "https://www.reddit.com/r/castles/comments/abc/post/"
	before := domain.ItemID(feedID, guid, itemURL)
	repository := &fakeRedditStore{feeds: []domain.Feed{{
		FeedID: feedID, URL: legacyURL, Connector: domain.ConnectorRSS,
		Title: "top scoring links : castles", FetchIntervalH: 6,
		ETag: `"legacy"`, LastModified: "yesterday",
	}}}

	total, affected, err := run(context.Background(), repository, false)
	if err != nil || total != 1 || affected != 1 || repository.writes != 0 {
		t.Fatalf("dry run = total %d affected %d writes %d err %v", total, affected, repository.writes, err)
	}
	if _, _, err := run(context.Background(), repository, true); err != nil {
		t.Fatal(err)
	}
	feed := repository.feeds[0]
	if repository.writes != 1 || feed.Connector != domain.ConnectorReddit || feed.URL != "https://www.reddit.com/r/castles/top.rss?t=day" || feed.Title != "r/castles" || feed.FetchIntervalH != 24 {
		t.Fatalf("migrated feed = %#v, writes %d", feed, repository.writes)
	}
	if feed.ETag != "" || feed.LastModified != "" {
		t.Fatalf("validators were not reset: %#v", feed)
	}
	_, affected, err = run(context.Background(), repository, true)
	if err != nil || affected != 0 || repository.writes != 1 {
		t.Fatalf("second apply = affected %d writes %d err %v", affected, repository.writes, err)
	}
	after := domain.ItemID(feed.FeedID, guid, itemURL)
	if before != after {
		t.Fatalf("item id changed: %s -> %s", before, after)
	}
}

func TestMigrateFeedSkipsUnsupportedRedditAndUnrelatedFeeds(t *testing.T) {
	for _, feed := range []domain.Feed{
		{URL: "https://example.com/feed.xml"},
		{URL: "https://www.reddit.com/user/sema/.rss"},
		{URL: "https://www.reddit.com/r/castles/comments/abc/post/"},
	} {
		if _, changed := migrateFeed(feed); changed {
			t.Fatalf("migrateFeed(%q) changed an unsupported feed", feed.URL)
		}
	}
}
