package main

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/dynamotest"
	"github.com/nuntz/sema/internal/store"
)

// Lambda serves a user's requests from separate warm instances. A Feed edit
// acknowledged by one instance used to show its old state on another that
// still held the Feed list (#18): a Tag's Scope missed the newly Tagged Feed's
// Items, and the Feed list showed the old title.
func TestEditedFeedDoesNotShowItsOldStateOnAnotherInstance(t *testing.T) {
	db, table := dynamotest.NewTable(t)
	repository := store.New(db, nil, table, "", "")
	now := time.Now().UTC()
	putTaggedFeedItems(t, repository, now, "news", "news-1")
	putTaggedFeedItems(t, repository, now, "wire", "wire-1")
	writer, reader := &server{store: repository, snapshotStore: repository}, &server{store: repository, snapshotStore: repository}
	if got := scopeItemIDs(t, reader, "wire"); !slices.Equal(got, []string{"wire-1"}) {
		t.Fatalf("wire Scope before the edit = %v", got)
	}
	if got := listedFeeds(t, reader)["news"]; got.CustomTitle != "" {
		t.Fatalf("news before the edit = %+v", got)
	}

	mustOK(t, writer, http.MethodPatch, "/api/feeds/news", nil, `{"custom_title":"Renamed","tags":["news","wire"]}`)

	if got := scopeItemIDs(t, reader, "wire"); !slices.Equal(got, []string{"news-1", "wire-1"}) {
		t.Fatalf("wire Scope after Tagging news = %v, want news-1 and wire-1", got)
	}
	if got := listedFeeds(t, reader)["news"]; got.CustomTitle != "Renamed" || !slices.Equal(got.Tags, []string{"news", "wire"}) {
		t.Fatalf("news after the edit = title %q, tags %v", got.CustomTitle, got.Tags)
	}
}

// scopeItemIDs returns the IDs of the Items in a Tag's Scope, sorted.
func scopeItemIDs(t *testing.T, s *server, tag string) []string {
	t.Helper()
	got, _ := handleSignedIn(t, s, http.MethodGet, "/api/items", map[string]string{"tag": tag, "include_read": "true"})
	if got.StatusCode != http.StatusOK {
		t.Fatalf("items = %d: %s", got.StatusCode, got.Body)
	}
	var body struct {
		Items []domain.Item `json:"items"`
	}
	decodeJSONBody(t, got.Body, &body)
	ids := make([]string, 0, len(body.Items))
	for _, item := range body.Items {
		ids = append(ids, item.ItemID)
	}
	slices.Sort(ids)
	return ids
}

// listedFeeds returns the Feed list by Feed ID.
func listedFeeds(t *testing.T, s *server) map[string]domain.Feed {
	t.Helper()
	got, _ := handleSignedIn(t, s, http.MethodGet, "/api/feeds", nil)
	if got.StatusCode != http.StatusOK {
		t.Fatalf("feeds = %d: %s", got.StatusCode, got.Body)
	}
	var body struct {
		Feeds []domain.Feed `json:"feeds"`
	}
	decodeJSONBody(t, got.Body, &body)
	feeds := make(map[string]domain.Feed, len(body.Feeds))
	for _, feed := range body.Feeds {
		feeds[feed.FeedID] = feed
	}
	return feeds
}
