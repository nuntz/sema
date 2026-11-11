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

// sharedStoryStore is one user's Story led by a news Item with a wire Item
// as its second source, plus the Read markers every API instance in a test
// sees.
type sharedStoryStore struct {
	*sharedFeedStore
	markers map[string]bool
	signals map[string]int
	kept    map[string]bool
}

func newSharedStoryStore() *sharedStoryStore {
	db := &sharedStoryStore{sharedFeedStore: newSharedFeedStore(testFeed("news"), testFeed("wire")), markers: map[string]bool{}, signals: map[string]int{}, kept: map[string]bool{}}
	expires := time.Now().Add(time.Hour).Unix()
	items := map[string]domain.Item{
		"lead":   {ItemID: "lead", FeedID: "news", TTL: expires, Vector: []byte{1}},
		"member": {ItemID: "member", FeedID: "wire", TTL: expires, Vector: []byte{1}},
	}
	db.clusters = func(context.Context, string) ([]domain.Cluster, error) {
		return []domain.Cluster{{StoryID: "story", MemberIDs: []string{"lead", "member"}}}, nil
	}
	db.resolve = func(_ context.Context, _ string, ids []string, snapshot map[string]bool) ([]domain.Item, error) {
		resolved := make([]domain.Item, 0, len(ids))
		for _, id := range ids {
			item := items[id]
			// Like ResolveRead, read the markers directly without a snapshot.
			if snapshot == nil {
				item.Read = db.markers[id]
			} else {
				item.Read = snapshot[id]
			}
			if db.kept[id] {
				item.ArchiveSK = "A#" + id
			}
			resolved = append(resolved, item)
		}
		return resolved, nil
	}
	db.item = func(_ context.Context, _, id string) (domain.Item, error) { return items[id], nil }
	db.loadMarkers = func(context.Context, string) (map[string]bool, error) { return maps.Clone(db.markers), nil }
	db.setRead = func(_ context.Context, _ string, ids []string, read bool) error {
		for _, id := range ids {
			if read {
				db.markers[id] = true
			} else {
				delete(db.markers, id)
			}
		}
		return nil
	}
	db.signalValues = func(_ context.Context, _ string, ids []string) (map[string]int, error) {
		values := map[string]int{}
		for _, id := range ids {
			if value := db.signals[id]; value != 0 {
				values[id] = value
			}
		}
		return values, nil
	}
	db.setSignal = func(_ context.Context, _ string, item domain.Item, value int) error {
		db.signals[item.ItemID] = value
		return nil
	}
	db.setHeart = func(_ context.Context, _, id string, kept bool) (string, int, error) {
		db.kept[id] = kept
		return "A#" + id, len(db.kept), nil
	}
	return db
}

// renderedStories returns the Stories s serves, keyed by Story ID.
func renderedStories(t *testing.T, s *server, query map[string]string) map[string][]domain.Item {
	t.Helper()
	result := s.getStories(context.Background(), s.requestSnapshot("user"), query)
	mustStatus(t, result.StatusCode, result.Body, http.StatusOK)
	var body struct {
		Stories []struct {
			StoryID string        `json:"story_id"`
			Items   []domain.Item `json:"items"`
		} `json:"stories"`
	}
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
		t.Fatalf("stories: %v: %s", err, result.Body)
	}
	stories := map[string][]domain.Item{}
	for _, story := range body.Stories {
		stories[story.StoryID] = story.Items
	}
	return stories
}

// Lambda serves concurrent requests from separate warm instances, each with
// its own Story cache. Unread-only Stories served after an acknowledged Read
// must reflect it whichever instance took the Read.
func TestReadReachesAnotherInstancesUnreadStories(t *testing.T) {
	db := newSharedStoryStore()
	writer, reader := writerAndWarmReader(t, db.sharedFeedStore)
	if got := renderedStories(t, reader, map[string]string{}); len(got["story"]) != 2 {
		t.Fatalf("warm unread stories = %v, want the Story", got)
	}
	got := writer.readBatch(context.Background(), writer.requestSnapshot("user"), `{"ids":["lead","member"]}`)
	mustStatus(t, got.StatusCode, got.Body, http.StatusOK)
	if got := renderedStories(t, reader, map[string]string{}); len(got) != 0 {
		t.Fatalf("unread stories = %v after another instance read the Story, want none", got)
	}
}

// Stories served with Read Items included still show each Member's Read
// state, and the client replaces its optimistic copy with them.
func TestReadReachesAnotherInstancesStoriesWithReadIncluded(t *testing.T) {
	db := newSharedStoryStore()
	writer, reader := writerAndWarmReader(t, db.sharedFeedStore)
	query := map[string]string{"include_read": "true"}
	if got := renderedStories(t, reader, query)["story"]; len(got) != 2 || got[0].Read {
		t.Fatalf("warm Story = %+v, want an unread Lead", got)
	}
	got := writer.readBatch(context.Background(), writer.requestSnapshot("user"), `{"ids":["lead","member"]}`)
	mustStatus(t, got.StatusCode, got.Body, http.StatusOK)
	for _, item := range renderedStories(t, reader, query)["story"] {
		if !item.Read {
			t.Fatalf("%s unread after another instance read the Story", item.ItemID)
		}
	}
}

// listedItems returns the IDs of the unread Items s lists outside Stories.
func listedItems(t *testing.T, s *server) []string {
	t.Helper()
	result := s.getItems(context.Background(), s.requestSnapshot("user"), map[string]string{"exclude_stories": "true"})
	mustStatus(t, result.StatusCode, result.Body, http.StatusOK)
	var body struct {
		Items []domain.Item `json:"items"`
	}
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
		t.Fatalf("items: %v: %s", err, result.Body)
	}
	ids := []string{}
	for _, item := range body.Items {
		ids = append(ids, item.ItemID)
	}
	return ids
}

// The unread item list leaves out Items its Stories show. Marking a Lead
// unread on another instance brings its Story back, so the list must stop
// showing that Lead on its own.
func TestUnreadOnAnotherInstanceHidesStoryMembersFromItemList(t *testing.T) {
	db := newSharedStoryStore()
	db.markers = map[string]bool{"lead": true, "member": true}
	db.itemsForFeeds = func(_ context.Context, _ string, _ domain.Order, _ string, _ int, _, _ bool, _, exclude map[string]bool, _ domain.FetchWindow, snapshot map[string]bool) ([]domain.Item, string, *domain.Item, error) {
		var items []domain.Item
		for _, id := range []string{"lead", "member"} {
			if !snapshot[id] && !exclude[id] {
				items = append(items, domain.Item{ItemID: id})
			}
		}
		return items, "", nil, nil
	}
	writer, reader := writerAndWarmReader(t, db.sharedFeedStore)
	if got := listedItems(t, reader); len(got) != 0 {
		t.Fatalf("warm unread items = %v, want none", got)
	}
	got := writer.readBatch(context.Background(), writer.requestSnapshot("user"), `{"ids":["lead"],"read":false}`)
	mustStatus(t, got.StatusCode, got.Body, http.StatusOK)
	if got := listedItems(t, reader); len(got) != 0 {
		t.Fatalf("unread items = %v after another instance brought the Story back, want none", got)
	}
}

// leadAfter warms reader's Stories, has writer apply action to the Lead,
// then returns the Lead as reader serves it.
func leadAfter(t *testing.T, action, body string) domain.Item {
	t.Helper()
	db := newSharedStoryStore()
	writer, reader := writerAndWarmReader(t, db.sharedFeedStore)
	query := map[string]string{"include_read": "true"}
	if got := renderedStories(t, reader, query)["story"]; len(got) != 2 || got[0].Signal != 0 || got[0].Hearted {
		t.Fatalf("warm Story = %+v, want a neutral Lead", got)
	}
	got := writer.itemRoute(context.Background(), writer.requestSnapshot("user"), http.MethodPost, "lead/"+action, body)
	mustStatus(t, got.StatusCode, got.Body, http.StatusOK)
	story := renderedStories(t, reader, query)["story"]
	if len(story) == 0 || story[0].ItemID != "lead" {
		t.Fatalf("Story = %+v, want it led by lead", story)
	}
	return story[0]
}

// Stories served after an acknowledged Boost, Bury or Keep must show it
// whichever instance took it; the client replaces its optimistic copy with them.
func TestBoostReachesAnotherInstancesStories(t *testing.T) {
	if lead := leadAfter(t, "signal", `{"value":1}`); lead.Signal != 1 {
		t.Fatalf("Lead signal = %d after a Boost on another instance, want 1", lead.Signal)
	}
}

func TestKeepReachesAnotherInstancesStories(t *testing.T) {
	if lead := leadAfter(t, "heart", `{"hearted":true}`); !lead.Hearted {
		t.Fatal("Lead not Kept after a Keep on another instance")
	}
}

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

func testFeed(id string, tags ...string) domain.Feed {
	return domain.Feed{PK: domain.UserPK("user"), SK: domain.FeedSK(id), FeedID: id, URL: "https://example.com/" + id + ".xml", Title: id, Tags: tags}
}

// writerAndWarmReader returns a writer and a reader serving the same user from one
// store, the reader already holding a warm copy of the user's Feeds.
func writerAndWarmReader(t *testing.T, db *sharedFeedStore) (writer, reader *server) {
	t.Helper()
	writer = &server{store: db, queue: &apiQueue{}}
	reader = &server{store: db, queue: &apiQueue{}}
	if _, err := reader.allowedFeedIDs(context.Background(), reader.requestSnapshot("user"), "", ""); err != nil {
		t.Fatal(err)
	}
	if got := reader.getFeeds(context.Background(), reader.requestSnapshot("user")); got.StatusCode != http.StatusOK {
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
