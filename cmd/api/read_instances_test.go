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
	"github.com/nuntz/sema/internal/vectorstore"
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
	writer, reader := &server{store: repository, snapshotStore: repository}, &server{store: repository, snapshotStore: repository}
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

// Item detail, Similar, Search and the include-read grid used to read `R#`
// rows eventually consistent instead of the Request snapshot, so an Item read
// on one instance could still show as unread when another instance served it.
func TestViewsReportAnItemReadOnAnotherInstance(t *testing.T) {
	for _, view := range []struct {
		name  string
		path  string
		query map[string]string
		items func(t *testing.T, body string) []domain.Item
	}{
		{"item detail", "/api/items/news-1", nil, func(t *testing.T, body string) []domain.Item {
			var item domain.Item
			decodeJSONBody(t, body, &item)
			return []domain.Item{item}
		}},
		{"similar", "/api/items/news-2/similar", nil, func(t *testing.T, body string) []domain.Item {
			var got struct {
				Items []domain.Item `json:"items"`
			}
			decodeJSONBody(t, body, &got)
			return got.Items
		}},
		{"search matches and related", "/api/search", map[string]string{"q": "pulumi"}, func(t *testing.T, body string) []domain.Item {
			var got struct {
				Matches searchGroup `json:"matches"`
				Related searchGroup `json:"related"`
			}
			decodeJSONBody(t, body, &got)
			return append(got.Matches.Window, got.Related.Window...)
		}},
		{"grid including read", "/api/items", map[string]string{"include_read": "true"}, func(t *testing.T, body string) []domain.Item {
			var got struct {
				Items []domain.Item `json:"items"`
			}
			decodeJSONBody(t, body, &got)
			return got.Items
		}},
	} {
		t.Run(view.name, func(t *testing.T) {
			db, table := dynamotest.NewTable(t)
			repository := store.New(db, nil, table, "", "")
			putSearchableItems(t, repository, time.Now().UTC(), map[string]string{"news-1": "pulumi notes", "news-2": "lambda notes", "news-3": "other notes"})
			instance := func() *server {
				return &server{store: repository, snapshotStore: repository, embedder: searchEmbedder{}, vectors: newsVectors{}}
			}
			writer, reader := instance(), instance()
			mustOK(t, reader, http.MethodGet, "/api/items", nil, "")

			mustOK(t, writer, http.MethodPost, "/api/items/read-batch", nil, `{"ids":["news-1","news-3"]}`)
			got, event := handleSignedIn(t, reader, http.MethodGet, view.path, view.query)

			if got.StatusCode != http.StatusOK {
				t.Fatalf("%s = %d: %s", view.path, got.StatusCode, got.Body)
			}
			items := view.items(t, got.Body)
			if len(items) == 0 {
				t.Fatalf("%s returned no Items: %s", view.path, got.Body)
			}
			for _, item := range items {
				if want := item.ItemID != "news-2"; item.Read != want {
					t.Errorf("%s reports %s read = %v, want %v", view.name, item.ItemID, item.Read, want)
				}
			}
			// The reader's Read markers predate the write, so resolving Read
			// state from its Snapshot finds them stale and reloads them.
			// DynamoDB Local reads `R#` rows consistently either way, so this
			// is what tells the Snapshot path from the old direct one.
			if event["ReadCache"] != "stale" {
				t.Errorf("ReadCache = %q, want stale: %s resolves Read state outside the Request snapshot", event["ReadCache"], view.name)
			}
		})
	}
}

// putSearchableItems seeds a "news" Feed with Items whose titles Search matches.
func putSearchableItems(t *testing.T, repository *store.Store, now time.Time, titles map[string]string) {
	t.Helper()
	ctx := context.Background()
	if err := repository.PutFeed(ctx, domain.Feed{
		PK: domain.UserPK("user"), SK: domain.FeedSK("news"), FeedID: "news", URL: "https://example.com/news", NextFetchAt: domain.Timestamp(now),
	}); err != nil {
		t.Fatalf("put feed: %v", err)
	}
	for id, title := range titles {
		written, err := repository.PutItem(ctx, domain.Item{
			PK: domain.UserPK("user"), SK: domain.ItemSK(now, id), FeedPK: "F#news", ItemID: id, FeedID: "news",
			URL: "https://example.com/" + id, Title: title, SearchText: domain.DeriveSearchText(title, ""),
			PublishedTS: domain.Timestamp(now), FetchedTS: domain.Timestamp(now), Score: 1, Size: "M", TTL: now.Add(domain.Retention).Unix(),
		})
		if err != nil || !written {
			t.Fatalf("put %s = %v, %v", id, written, err)
		}
	}
}

// newsVectors finds every seeded Item close to any query or Item.
type newsVectors struct{ vectorstore.Store }

func (newsVectors) Get(context.Context, string) ([]float32, error) { return []float32{1}, nil }

func (newsVectors) Query(context.Context, string, []float32, int, int64) ([]vectorstore.Match, error) {
	return []vectorstore.Match{{Key: "news-1", Similarity: 90}, {Key: "news-2", Similarity: 80}, {Key: "news-3", Similarity: 70}}, nil
}

func decodeJSONBody(t *testing.T, body string, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), into); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}
