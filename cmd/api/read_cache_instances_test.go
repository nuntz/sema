package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

// Lambda serves concurrent requests from separate warm instances, each with
// its own read cache. A count served after a settled read-batch must reflect
// that write whichever instance took it.
func TestFeedCountsReflectReadsTakenByAnotherInstance(t *testing.T) {
	markers := map[string]bool{}
	db := &fakeAPIStore{
		loadMarkers: func(context.Context, string) (map[string]bool, error) {
			copied := make(map[string]bool, len(markers))
			for id, read := range markers {
				copied[id] = read
			}
			return copied, nil
		},
		setRead: func(_ context.Context, _ string, ids []string, read bool) error {
			for _, id := range ids {
				markers[id] = read
			}
			return nil
		},
		feeds: func(context.Context, string) ([]domain.Feed, error) {
			return []domain.Feed{{FeedID: "news-feed"}}, nil
		},
		feedItemCounts: func(_ context.Context, _ string, _ domain.FetchWindow, snapshot map[string]bool) (map[string]domain.FeedItemCount, error) {
			count := domain.FeedItemCount{All: 2}
			for _, id := range []string{"news-1", "news-2"} {
				if !snapshot[id] {
					count.Unread++
				}
			}
			return map[string]domain.FeedItemCount{"news-feed": count}, nil
		},
		item: func(_ context.Context, _ string, id string) (domain.Item, error) {
			return domain.Item{ItemID: id, FeedID: "news-feed", TTL: time.Now().Add(time.Hour).Unix()}, nil
		},
	}
	ctx := context.Background()
	writer, reader := &server{store: db}, &server{store: db}
	if _, err := reader.loadReadMarkers(ctx, "user"); err != nil {
		t.Fatal(err)
	}
	if result := writer.readBatch(ctx, "user", `{"ids":["news-1","news-2"]}`); result.StatusCode != 200 {
		t.Fatalf("read batch: %s", result.Body)
	}
	result := reader.getFeedItemCounts(ctx, "user", map[string]string{})
	var body struct {
		Feeds map[string]domain.FeedItemCount `json:"feeds"`
	}
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
		t.Fatalf("counts %d: %s", result.StatusCode, result.Body)
	}
	if got := body.Feeds["news-feed"].Unread; got != 0 {
		t.Fatalf("news-feed unread = %d after another instance took the read, want 0", got)
	}
}

// An instance patches its copy forward after its own write, but drops it when
// another instance's write landed in between.
func TestOwnReadWriteKeepsCacheOnlyWithoutInterleavedWrites(t *testing.T) {
	loads := 0
	db := &fakeAPIStore{
		loadMarkers: func(context.Context, string) (map[string]bool, error) { loads++; return map[string]bool{}, nil },
		feeds:       func(context.Context, string) ([]domain.Feed, error) { return nil, nil },
		item: func(_ context.Context, _ string, id string) (domain.Item, error) {
			return domain.Item{ItemID: id, FeedID: "feed", TTL: time.Now().Add(time.Hour).Unix()}, nil
		},
	}
	ctx := context.Background()
	self, other := &server{store: db}, &server{store: db}
	load := func(want int) {
		t.Helper()
		if _, err := self.loadReadMarkers(ctx, "user"); err != nil {
			t.Fatal(err)
		}
		if loads != want {
			t.Fatalf("marker loads = %d, want %d", loads, want)
		}
	}
	load(1)
	if err := self.setRead(ctx, "user", []string{"a"}, true); err != nil {
		t.Fatal(err)
	}
	load(1)
	if err := other.setRead(ctx, "user", []string{"b"}, true); err != nil {
		t.Fatal(err)
	}
	if err := self.setRead(ctx, "user", []string{"c"}, true); err != nil {
		t.Fatal(err)
	}
	load(2)
}
