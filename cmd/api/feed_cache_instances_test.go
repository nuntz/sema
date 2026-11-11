package main

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
)

// sharedFeedStore is one user's Feeds, seen by every API instance in a test.
type sharedFeedStore struct {
	*fakeAPIStore
	rows  map[string]domain.Feed
	loads int
}

func newSharedFeedStore(feeds ...domain.Feed) *sharedFeedStore {
	shared := &sharedFeedStore{fakeAPIStore: &fakeAPIStore{}, rows: map[string]domain.Feed{}}
	for _, feed := range feeds {
		shared.rows[feed.FeedID] = feed
	}
	shared.feed = func(_ context.Context, _, feedID string) (domain.Feed, error) {
		feed, ok := shared.rows[feedID]
		if !ok {
			return domain.Feed{}, store.ErrNotFound
		}
		return feed, nil
	}
	shared.feeds = func(context.Context, string) ([]domain.Feed, error) {
		shared.loads++
		ids := slices.Sorted(maps.Keys(shared.rows))
		feeds := make([]domain.Feed, 0, len(ids))
		for _, id := range ids {
			feeds = append(feeds, shared.rows[id])
		}
		return feeds, nil
	}
	shared.putFeed = func(_ context.Context, feed domain.Feed) error {
		shared.rows[feed.FeedID] = feed
		return nil
	}
	shared.deleteFeed = func(_ context.Context, _, feedID string) error {
		delete(shared.rows, feedID)
		return nil
	}
	return shared
}

// withNewsAndWireStory gives the store one Story led by a news Item, with a
// wire Item as its second source.
func withNewsAndWireStory(db *sharedFeedStore) {
	expires := time.Now().Add(time.Hour).Unix()
	db.clusters = func(context.Context, string) ([]domain.Cluster, error) {
		return []domain.Cluster{{StoryID: "story", MemberIDs: []string{"lead", "member"}}}, nil
	}
	db.resolve = func(context.Context, string, []string, map[string]bool) ([]domain.Item, error) {
		return []domain.Item{{ItemID: "lead", FeedID: "news", TTL: expires}, {ItemID: "member", FeedID: "wire", TTL: expires}}, nil
	}
}

func testFeed(id string, tags ...string) domain.Feed {
	return domain.Feed{PK: domain.UserPK("user"), SK: domain.FeedSK(id), FeedID: id, URL: "https://example.com/" + id + ".xml", Title: id, Tags: tags}
}

// writerAndWarmReader returns a writer and a reader serving the same user from one
// store, the reader already holding a warm copy of the user's Feeds.
func writerAndWarmReader(t *testing.T, db *sharedFeedStore) (writer, reader *server) {
	t.Helper()
	writer = &server{store: db, queue: &apiQueue{}}
	reader = &server{store: db, queue: &apiQueue{}}
	if _, err := reader.allowedFeedIDs(context.Background(), "user", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := reader.getFeeds(context.Background(), "user"); got.StatusCode != http.StatusOK {
		t.Fatalf("warm feeds: %d %s", got.StatusCode, got.Body)
	}
	return writer, reader
}

func mustStatus(t *testing.T, got int, body string, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("status = %d, want %d: %s", got, want, body)
	}
}

// Lambda serves concurrent requests from separate warm instances, each with
// its own Feed cache. A request served after an acknowledged Feed edit must
// reflect it whichever instance took the edit.
func TestTagEditReachesAnotherInstancesScope(t *testing.T) {
	db := newSharedFeedStore(testFeed("news"), testFeed("wire", "tech"))
	writer, reader := writerAndWarmReader(t, db)
	got := writer.feedRoute(context.Background(), "user", http.MethodPatch, "news", `{"tags":["tech"]}`)
	mustStatus(t, got.StatusCode, got.Body, http.StatusOK)
	allowed, err := reader.allowedFeedIDs(context.Background(), "user", "tech", "")
	if err != nil || !allowed["news"] || !allowed["wire"] {
		t.Fatalf("tech scope = %v, err = %v", allowed, err)
	}
}

func TestMuteReachesAnotherInstancesScope(t *testing.T) {
	db := newSharedFeedStore(testFeed("news"), testFeed("wire"))
	writer, reader := writerAndWarmReader(t, db)
	got := writer.feedRoute(context.Background(), "user", http.MethodPatch, "news", `{"muted":true}`)
	mustStatus(t, got.StatusCode, got.Body, http.StatusOK)
	allowed, err := reader.allowedFeedIDs(context.Background(), "user", "", "")
	if err != nil || allowed["news"] || !allowed["wire"] {
		t.Fatalf("all scope = %v, err = %v", allowed, err)
	}
}

func TestAddedFeedReachesAnotherInstancesCounts(t *testing.T) {
	db := newSharedFeedStore(testFeed("news"))
	added := domain.FeedID("https://example.com/added.xml")
	db.feedItemCounts = func(context.Context, string, domain.FetchWindow, map[string]bool) (map[string]domain.FeedItemCount, error) {
		return map[string]domain.FeedItemCount{"news": {All: 1}, added: {All: 2}}, nil
	}
	writer, reader := writerAndWarmReader(t, db)
	if got := reader.getFeedItemCounts(context.Background(), reader.requestSnapshot("user"), map[string]string{}); got.StatusCode != http.StatusOK {
		t.Fatalf("warm counts: %s", got.Body)
	}
	got := writer.addFeed(context.Background(), "user", `{"feed_url":"https://example.com/added.xml"}`)
	mustStatus(t, got.StatusCode, got.Body, http.StatusAccepted)
	result := reader.getFeedItemCounts(context.Background(), reader.requestSnapshot("user"), map[string]string{})
	var body struct {
		Feeds map[string]domain.FeedItemCount `json:"feeds"`
	}
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
		t.Fatalf("counts %d: %s", result.StatusCode, result.Body)
	}
	if body.Feeds[added].All != 2 {
		t.Fatalf("counts = %v, missing the Feed added on another instance", body.Feeds)
	}
}

func TestDeletedFeedLeavesAnotherInstancesFeedList(t *testing.T) {
	db := newSharedFeedStore(testFeed("news"), testFeed("wire"))
	writer, reader := writerAndWarmReader(t, db)
	got := writer.feedRoute(context.Background(), "user", http.MethodDelete, "news", "")
	mustStatus(t, got.StatusCode, got.Body, http.StatusNoContent)
	result := reader.getFeeds(context.Background(), "user")
	var body struct {
		Feeds []domain.Feed `json:"feeds"`
	}
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
		t.Fatalf("feeds %d: %s", result.StatusCode, result.Body)
	}
	var ids []string
	for _, feed := range body.Feeds {
		ids = append(ids, feed.FeedID)
	}
	if !slices.Equal(ids, []string{"wire"}) {
		t.Fatalf("feeds = %v, want only wire", ids)
	}
}

func TestRenameReachesAnotherInstancesCachedStories(t *testing.T) {
	db := newSharedFeedStore(testFeed("news"), testFeed("wire"))
	withNewsAndWireStory(db)
	writer, reader := writerAndWarmReader(t, db)
	storyTitles := func() map[string]string {
		t.Helper()
		result := reader.getStories(context.Background(), reader.requestSnapshot("user"), map[string]string{"include_read": "true"})
		var body struct {
			Stories []struct {
				Items []domain.Item `json:"items"`
			} `json:"stories"`
		}
		if err := json.Unmarshal([]byte(result.Body), &body); err != nil || len(body.Stories) != 1 {
			t.Fatalf("stories %d: %s", result.StatusCode, result.Body)
		}
		titles := map[string]string{}
		for _, item := range body.Stories[0].Items {
			titles[item.FeedID] = item.FeedTitle
		}
		return titles
	}
	if got := storyTitles()["news"]; got != "news" {
		t.Fatalf("warm story title = %q", got)
	}
	got := writer.feedRoute(context.Background(), "user", http.MethodPatch, "news", `{"custom_title":"Morning News"}`)
	mustStatus(t, got.StatusCode, got.Body, http.StatusOK)
	if got := storyTitles()["news"]; got != "Morning News" {
		t.Fatalf("story title = %q after a rename on another instance", got)
	}
}

// The worker writes every Feed after each fetch without moving the version,
// so those writes leave other instances' caches warm until the TTL.
func TestWorkerFeedWriteKeepsAnotherInstancesCache(t *testing.T) {
	db := newSharedFeedStore(testFeed("news"))
	_, reader := writerAndWarmReader(t, db)
	loads := db.loads
	feed := db.rows["news"]
	feed.Title = "Fetched title"
	db.rows["news"] = feed
	ctx, fields := withRequestFields(context.Background())
	if _, err := reader.allowedFeedIDs(ctx, "user", "", ""); err != nil {
		t.Fatal(err)
	}
	if db.loads != loads || fields["FeedCache"] != "hit" {
		t.Fatalf("worker write reloaded the cache: loads %d -> %d, FeedCache = %q", loads, db.loads, fields["FeedCache"])
	}
}

// An instance reports why it reloaded, as ReadCache does.
func TestFeedCacheAnnotatesStaleCopies(t *testing.T) {
	db := newSharedFeedStore(testFeed("news"))
	writer, reader := writerAndWarmReader(t, db)
	got := writer.feedRoute(context.Background(), "user", http.MethodPatch, "news", `{"tags":["tech"]}`)
	mustStatus(t, got.StatusCode, got.Body, http.StatusOK)
	ctx, fields := withRequestFields(context.Background())
	if _, err := reader.allowedFeedIDs(ctx, "user", "", ""); err != nil {
		t.Fatal(err)
	}
	if fields["FeedCache"] != "stale" {
		t.Fatalf("FeedCache = %q, want stale", fields["FeedCache"])
	}
	ctx, fields = withRequestFields(context.Background())
	if _, err := (&server{store: db}).allowedFeedIDs(ctx, "user", "", ""); err != nil {
		t.Fatal(err)
	}
	if fields["FeedCache"] != "miss" {
		t.Fatalf("cold FeedCache = %q, want miss", fields["FeedCache"])
	}
}

// A Story request reads Feeds for its Scope, its cache key and its
// presentation, but checks the version once.
func TestRequestReadsFeedVersionOnce(t *testing.T) {
	db := newSharedFeedStore(testFeed("news"), testFeed("wire"))
	withNewsAndWireStory(db)
	s := &server{store: db}
	result := s.getStories(withFeedVersionMemo(context.Background()), s.requestSnapshot("user"), map[string]string{"include_read": "true"})
	mustStatus(t, result.StatusCode, result.Body, http.StatusOK)
	if db.feedVersionReads != 1 {
		t.Fatalf("FeedVersion reads = %d, want 1", db.feedVersionReads)
	}
}
