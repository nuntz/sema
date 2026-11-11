package requestsnapshot_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/nuntz/sema/cmd/api/internal/requestsnapshot"
	"github.com/nuntz/sema/internal/domain"
)

// rendered is what a test's render function returns: which render call made
// it, and the Read markers it was given, so a test can tell a cached render
// from a new one.
type rendered struct {
	render int
	read   []string
}

// renderer counts its renders across every Cache in a test.
type renderer struct{ renders int }

func (r *renderer) render(_ context.Context, read map[string]bool) (rendered, error) {
	r.renders++
	ids := make([]string, 0, len(read))
	for id := range read {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return rendered{render: r.renders, read: ids}, nil
}

type storyFilters struct {
	Tag        string
	UnreadOnly bool
}

func (r *renderer) stories(t *testing.T, snapshot *requestsnapshot.Snapshot, filters any) rendered {
	t.Helper()
	got, err := requestsnapshot.Stories(context.Background(), snapshot, filters, r.render)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func wantRender(t *testing.T, got rendered, render int, read ...string) {
	t.Helper()
	if got.render != render || !slices.Equal(got.read, append([]string{}, read...)) {
		t.Fatalf("Stories = render %d with Read %v, want render %d with Read %v", got.render, got.read, render, read)
	}
}

func TestStoriesRenderFromTheSnapshotsReadMarkersAndAreReused(t *testing.T) {
	tb := newTable(t)
	tb.putLiveItems(t, "user", "a")
	clock := newClock()
	cache := requestsnapshot.NewCache(tb.store, clock.Now)
	setRead(t, cache.Snapshot("user"), true, "a")
	r := &renderer{}

	wantRender(t, r.stories(t, cache.Snapshot("user"), storyFilters{}), 1, "a")
	wantRender(t, r.stories(t, cache.Snapshot("user"), storyFilters{}), 1, "a")
}

// writes are each kind of write a Story shows, made on a Snapshot of "user"
// who has a live Item "a".
var writes = []struct {
	name  string
	write func(*requestsnapshot.Snapshot) error
}{
	{"read", func(s *requestsnapshot.Snapshot) error { return s.SetRead(context.Background(), []string{"a"}, true) }},
	{"feed edit", func(s *requestsnapshot.Snapshot) error {
		return s.SaveFeeds(context.Background(), feed("user", "feed", "Renamed"))
	}},
	{"feed delete", func(s *requestsnapshot.Snapshot) error { return s.DeleteFeed(context.Background(), "feed") }},
	{"keep", func(s *requestsnapshot.Snapshot) error {
		_, _, err := s.SetKeep(context.Background(), "a", true)
		return err
	}},
	{"signal", func(s *requestsnapshot.Snapshot) error {
		return s.SetSignal(context.Background(), domain.Item{ItemID: "a"}, 0)
	}},
}

func TestStoryWriteOnOneInstanceRendersAgainOnAnother(t *testing.T) {
	for _, write := range writes {
		t.Run(write.name, func(t *testing.T) {
			tb := newTable(t)
			tb.putLiveItems(t, "user", "a")
			clock := newClock()
			self, other := requestsnapshot.NewCache(tb.store, clock.Now), requestsnapshot.NewCache(tb.store, clock.Now)
			r := &renderer{}
			r.stories(t, self.Snapshot("user"), storyFilters{})

			if err := write.write(other.Snapshot("user")); err != nil {
				t.Fatal(err)
			}

			if got := r.stories(t, self.Snapshot("user"), storyFilters{}); got.render != 2 {
				t.Fatalf("Stories after another instance's %s = render %d, want a new render", write.name, got.render)
			}
		})
	}
}

func TestRequestRendersStoriesAgainAfterItsOwnWrite(t *testing.T) {
	for _, write := range writes {
		t.Run(write.name, func(t *testing.T) {
			tb := newTable(t)
			tb.putLiveItems(t, "user", "a")
			clock := newClock()
			request := requestsnapshot.NewCache(tb.store, clock.Now).Snapshot("user")
			r := &renderer{}
			r.stories(t, request, storyFilters{})

			if err := write.write(request); err != nil {
				t.Fatal(err)
			}

			if got := r.stories(t, request, storyFilters{}); got.render != 2 {
				t.Fatalf("Stories after the request's own %s = render %d, want a new render", write.name, got.render)
			}
		})
	}
}

func TestRequestSeesOneStoryRender(t *testing.T) {
	for _, write := range writes {
		t.Run(write.name, func(t *testing.T) {
			tb := newTable(t)
			tb.putLiveItems(t, "user", "a")
			clock := newClock()
			self, other := requestsnapshot.NewCache(tb.store, clock.Now), requestsnapshot.NewCache(tb.store, clock.Now)
			request := self.Snapshot("user")
			r := &renderer{}
			wantRender(t, r.stories(t, request, storyFilters{}), 1)

			if err := write.write(other.Snapshot("user")); err != nil {
				t.Fatal(err)
			}

			wantRender(t, r.stories(t, request, storyFilters{}), 1)
		})
	}
}

func TestStoriesAreRenderedPerFilters(t *testing.T) {
	tb := newTable(t)
	clock := newClock()
	cache := requestsnapshot.NewCache(tb.store, clock.Now)
	r := &renderer{}
	wantRender(t, r.stories(t, cache.Snapshot("user"), map[string]bool{"news": true, "wire": true}), 1)
	// Encoded filters sort map keys, so the order a caller built them in
	// doesn't matter.
	wantRender(t, r.stories(t, cache.Snapshot("user"), map[string]bool{"wire": true, "news": true}), 1)

	wantRender(t, r.stories(t, cache.Snapshot("user"), map[string]bool{"news": true}), 2)
	wantRender(t, r.stories(t, cache.Snapshot("other"), map[string]bool{"news": true, "wire": true}), 3)
	wantRender(t, r.stories(t, cache.Snapshot("user"), map[string]bool{"news": true, "wire": true}), 1)
}

func TestStoriesExpireAfterFourMinutes(t *testing.T) {
	tb := newTable(t)
	clock := newClock()
	cache := requestsnapshot.NewCache(tb.store, clock.Now)
	r := &renderer{}
	r.stories(t, cache.Snapshot("user"), storyFilters{})

	clock.Advance(4*time.Minute - time.Second)
	wantRender(t, r.stories(t, cache.Snapshot("user"), storyFilters{}), 1)

	clock.Advance(time.Second)
	wantRender(t, r.stories(t, cache.Snapshot("user"), storyFilters{}), 2)
}

func TestStoryCacheEvictsItsOldestRenderBeyond128(t *testing.T) {
	tb := newTable(t)
	clock := newClock()
	cache := requestsnapshot.NewCache(tb.store, clock.Now)
	r := &renderer{}
	r.stories(t, cache.Snapshot("user"), storyFilters{})
	for i := range 127 {
		clock.Advance(time.Millisecond)
		r.stories(t, cache.Snapshot("user"), storyFilters{Tag: fmt.Sprint("tag-", i)})
	}
	wantRender(t, r.stories(t, cache.Snapshot("user"), storyFilters{}), 1)

	clock.Advance(time.Millisecond)
	r.stories(t, cache.Snapshot("user"), storyFilters{Tag: "one-too-many"})

	wantRender(t, r.stories(t, cache.Snapshot("user"), storyFilters{}), 130)
}

func TestStoryCacheOutcomes(t *testing.T) {
	for _, write := range writes {
		t.Run(write.name, func(t *testing.T) {
			tb := newTable(t)
			tb.putLiveItems(t, "user", "a")
			clock := newClock()
			self, other := requestsnapshot.NewCache(tb.store, clock.Now), requestsnapshot.NewCache(tb.store, clock.Now)
			r := &renderer{}
			outcome := func() string {
				t.Helper()
				request := self.Snapshot("user")
				r.stories(t, request, storyFilters{})
				return request.CacheOutcomes()["StoryCache"]
			}
			if got := outcome(); got != "miss" {
				t.Fatalf("first load = %q, want miss", got)
			}
			if got := outcome(); got != "hit" {
				t.Fatalf("second load = %q, want hit", got)
			}
			if err := write.write(other.Snapshot("user")); err != nil {
				t.Fatal(err)
			}
			if got := outcome(); got != "stale" {
				t.Fatalf("load after another instance's %s = %q, want stale", write.name, got)
			}
			clock.Advance(4 * time.Minute)
			if got := outcome(); got != "miss" {
				t.Fatalf("load after the TTL = %q, want miss", got)
			}
		})
	}
}

func TestStoryCacheOutcomeIsTheRequestsFirst(t *testing.T) {
	tb := newTable(t)
	clock := newClock()
	cache := requestsnapshot.NewCache(tb.store, clock.Now)
	r := &renderer{}
	r.stories(t, cache.Snapshot("user"), storyFilters{})
	request := cache.Snapshot("user")

	r.stories(t, request, storyFilters{})
	r.stories(t, request, storyFilters{Tag: "news"})

	if got := request.CacheOutcomes()["StoryCache"]; got != "hit" {
		t.Fatalf("StoryCache = %q, want the first load's hit", got)
	}
	if got := cache.Snapshot("user").CacheOutcomes()["StoryCache"]; got != "" {
		t.Fatalf("request that never loaded Stories recorded StoryCache = %q", got)
	}
}
