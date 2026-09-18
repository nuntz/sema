package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

func TestUnreadItemsCacheAndReadBatch(t *testing.T) {
	queries := 0
	var writeErr error
	row := domain.Item{ItemID: "item", FeedID: "feed", TTL: time.Now().Add(time.Hour).Unix()}
	db := &fakeAPIStore{
		loadMarkers: func(context.Context, string) (map[string]bool, error) { queries++; return map[string]bool{}, nil },
		itemsForFeeds: func(_ context.Context, _ string, _ domain.Order, _ string, _ int, _, _ bool, _, _ map[string]bool, _ domain.FetchWindow, snapshot map[string]bool) ([]domain.Item, string, *domain.Item, error) {
			if snapshot == nil {
				t.Fatal("missing snapshot")
			}
			if snapshot["item"] {
				return nil, "", &row, nil
			}
			return []domain.Item{row}, "", nil, nil
		},
		feeds:   func(context.Context, string) ([]domain.Feed, error) { return []domain.Feed{{FeedID: "feed"}}, nil },
		item:    func(context.Context, string, string) (domain.Item, error) { return row, nil },
		setRead: func(context.Context, string, []string, bool) error { return writeErr },
	}
	s := &server{store: db}
	ctx := context.Background()
	load := func(want int) {
		t.Helper()
		result := s.getItems(ctx, "user", map[string]string{"include_read": "false"})
		var page struct {
			Items []domain.Item `json:"items"`
		}
		if result.StatusCode != 200 {
			t.Fatalf("items: %s", result.Body)
		}
		if err := json.Unmarshal([]byte(result.Body), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != want {
			t.Fatalf("items = %#v, want %d", page.Items, want)
		}
	}
	load(1)
	load(1)
	if queries != 1 {
		t.Fatalf("marker queries = %d", queries)
	}
	snapshot, err := s.loadReadMarkers(ctx, "user")
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		body string
		want int
	}{
		{`{"ids":["item"]}`, 0},
		{`{"ids":["item"],"read":false}`, 1},
	} {
		result := s.readBatch(ctx, "user", change.body)
		if result.StatusCode != 200 {
			t.Fatalf("read batch: %s", result.Body)
		}
		if snapshot["item"] {
			t.Fatal("mutation changed an existing request snapshot")
		}
		load(change.want)
		if queries != 1 {
			t.Fatalf("mutation reloaded markers: %d", queries)
		}
	}
	if snapshot["item"] {
		t.Fatal("mutation changed an existing request snapshot")
	}
	if result := s.itemRoute(ctx, "user", "POST", "item/read", `{"read":true}`); result.StatusCode != 200 {
		t.Fatalf("item read: %s", result.Body)
	}
	load(0)
	if queries != 1 {
		t.Fatalf("per-item mutation reloaded markers: %d", queries)
	}

	writeErr = errors.New("write failed")
	if result := s.readBatch(ctx, "user", `{"ids":["item"]}`); result.StatusCode != 500 {
		t.Fatalf("failed mutation: %s", result.Body)
	}
	load(1)
	if queries != 2 {
		t.Fatalf("failed mutation did not invalidate cache: %d", queries)
	}
}

func TestReadMarkerCacheExpiryAndBound(t *testing.T) {
	calls := 0
	source := &fakeAPIStore{}
	source.loadMarkers = func(context.Context, string) (map[string]bool, error) {
		calls++
		return map[string]bool{"read": true}, nil
	}
	s := &server{store: source}
	ctx := context.Background()
	for i := 0; i < 129; i++ {
		if _, err := s.loadReadMarkers(ctx, fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.readCache) != 128 {
		t.Fatalf("cache size = %d", len(s.readCache))
	}
	if _, ok := s.readCache["0"]; ok {
		t.Fatal("oldest user was not evicted")
	}
	entry := s.readCache["128"]
	entry.loaded = time.Now().Add(-readCacheTTL)
	s.readCache["128"] = entry
	if _, err := s.loadReadMarkers(ctx, "128"); err != nil {
		t.Fatal(err)
	}
	if calls != 130 {
		t.Fatalf("expired entry not reloaded: %d", calls)
	}
}

func TestRequestReadMarkerSnapshot(t *testing.T) {
	source := &fakeAPIStore{loadMarkers: func(context.Context, string) (map[string]bool, error) { return map[string]bool{"read": true}, nil }}
	s := &server{store: source}
	first, err := s.loadReadMarkers(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.setRead(context.Background(), "user", []string{"read"}, false); err != nil {
		t.Fatal(err)
	}
	if !first["read"] {
		t.Fatal("mutation changed a request snapshot")
	}
	second, err := s.loadReadMarkers(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	if second["read"] {
		t.Fatal("mutation did not update cache")
	}
}
