package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/dynamotest"
	"github.com/nuntz/sema/internal/store"
)

// Lambda serves a user's requests from separate warm instances. Reading a Tag
// chip's Items in its Scope on one instance, then hopping Scope, used to load
// counts from another instance that brought the chip's old count back.
func TestTagChipCountStaysDownAfterReadingItsScopeAndHopping(t *testing.T) {
	db, table := dynamotest.NewTable(t)
	repository := store.New(db, nil, table, "", "")
	now := time.Now().UTC()
	putTaggedFeedItems(t, repository, now, "news", "news-1", "news-2")
	putTaggedFeedItems(t, repository, now, "wire", "wire-1")
	writer, reader := &server{store: repository}, &server{store: repository}
	if got := unreadCounts(t, reader); got["news"] != 2 || got["wire"] != 1 {
		t.Fatalf("unread counts before reading = %v", got)
	}

	mustOK(t, writer, http.MethodGet, "/api/items", map[string]string{"tag": "news"}, "")
	mustOK(t, writer, http.MethodPost, "/api/items/read-batch", nil, `{"ids":["news-1","news-2"]}`)
	mustOK(t, reader, http.MethodGet, "/api/items", map[string]string{"tag": "wire"}, "")

	if got := unreadCounts(t, reader); got["news"] != 0 || got["wire"] != 1 {
		t.Fatalf("unread counts after the Scope hop = %v, want news 0 and wire 1", got)
	}
}

func putTaggedFeedItems(t *testing.T, repository *store.Store, now time.Time, feedID string, itemIDs ...string) {
	t.Helper()
	ctx := context.Background()
	if err := repository.PutFeed(ctx, domain.Feed{
		PK: domain.UserPK("user"), SK: domain.FeedSK(feedID), FeedID: feedID, Tags: []string{feedID},
		URL: "https://example.com/" + feedID, NextFetchAt: domain.Timestamp(now),
	}); err != nil {
		t.Fatalf("put feed %s: %v", feedID, err)
	}
	for _, id := range itemIDs {
		written, err := repository.PutItem(ctx, domain.Item{
			PK: domain.UserPK("user"), SK: domain.ItemSK(now, id), FeedPK: "F#" + feedID, ItemID: id, FeedID: feedID,
			URL: "https://example.com/" + id, Title: id, PublishedTS: domain.Timestamp(now), FetchedTS: domain.Timestamp(now),
			Score: 1, Size: "M", TTL: now.Add(domain.Retention).Unix(),
		})
		if err != nil || !written {
			t.Fatalf("put %s = %v, %v", id, written, err)
		}
	}
}

func mustOK(t *testing.T, s *server, method, path string, query map[string]string, body string) {
	t.Helper()
	if got, _ := handleSignedInWithBody(t, s, method, path, query, body); got.StatusCode != http.StatusOK {
		t.Fatalf("%s %s = %d: %s", method, path, got.StatusCode, got.Body)
	}
}

// unreadCounts returns each Feed's unread count, which the Tag chips sum.
func unreadCounts(t *testing.T, s *server) map[string]int {
	t.Helper()
	got, _ := handleSignedIn(t, s, http.MethodGet, "/api/feeds/counts", nil)
	var body struct {
		Feeds map[string]domain.FeedItemCount `json:"feeds"`
	}
	if got.StatusCode != http.StatusOK {
		t.Fatalf("counts = %d: %s", got.StatusCode, got.Body)
	}
	if err := json.Unmarshal([]byte(got.Body), &body); err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int, len(body.Feeds))
	for feedID, count := range body.Feeds {
		counts[feedID] = count.Unread
	}
	return counts
}
