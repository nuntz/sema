package requestsnapshot_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nuntz/sema/cmd/api/internal/requestsnapshot"
	"github.com/nuntz/sema/internal/domain"
)

func feed(userID, feedID, title string) domain.Feed {
	return domain.Feed{
		PK: domain.UserPK(userID), SK: domain.FeedSK(feedID), FeedID: feedID,
		URL: "https://example.com/" + feedID, CustomTitle: title,
		NextFetchAt: domain.Timestamp(time.Now().UTC()),
	}
}

func feeds(t *testing.T, snapshot *requestsnapshot.Snapshot) []domain.Feed {
	t.Helper()
	got, _, err := snapshot.Feeds(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func saveFeeds(t *testing.T, snapshot *requestsnapshot.Snapshot, feeds ...domain.Feed) {
	t.Helper()
	if err := snapshot.SaveFeeds(context.Background(), feeds...); err != nil {
		t.Fatal(err)
	}
}

// wantTitles checks the Feeds by ID and custom title.
func wantTitles(t *testing.T, got []domain.Feed, want map[string]string) {
	t.Helper()
	titles := make(map[string]string, len(got))
	for _, feed := range got {
		titles[feed.FeedID] = feed.CustomTitle
	}
	if len(titles) != len(want) || len(got) != len(want) {
		t.Fatalf("Feeds = %v, want %v", titles, want)
	}
	for id, title := range want {
		if got, ok := titles[id]; !ok || got != title {
			t.Fatalf("Feeds = %v, want %v", titles, want)
		}
	}
}

func TestFeedEditOnOneInstanceIsSeenByAnother(t *testing.T) {
	tb := newTable(t)
	clock := newClock()
	self, other := requestsnapshot.NewCache(tb.store, clock.Now), requestsnapshot.NewCache(tb.store, clock.Now)
	saveFeeds(t, other.Snapshot("user"), feed("user", "news", "Old"))
	wantTitles(t, feeds(t, self.Snapshot("user")), map[string]string{"news": "Old"})

	saveFeeds(t, other.Snapshot("user"), feed("user", "news", "New"))

	wantTitles(t, feeds(t, self.Snapshot("user")), map[string]string{"news": "New"})
}

// putFeedWithoutBump writes a Feed the way the feed worker does: the row
// changes but FEED_VERSION doesn't, so only a reload sees it and a kept copy
// hides it.
func (tb table) putFeedWithoutBump(t *testing.T, feed domain.Feed) {
	t.Helper()
	if err := tb.store.PutFeed(context.Background(), feed); err != nil {
		t.Fatal(err)
	}
}

func detailedFeeds(t *testing.T, snapshot *requestsnapshot.Snapshot) []domain.Feed {
	t.Helper()
	got, err := snapshot.DetailedFeeds(context.Background(), func(_ context.Context, feeds []domain.Feed) error {
		for i := range feeds {
			feeds[i].CustomTitle += " (detailed)"
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestFeedDeleteOnOneInstanceIsSeenByAnother(t *testing.T) {
	tb := newTable(t)
	clock := newClock()
	self, other := requestsnapshot.NewCache(tb.store, clock.Now), requestsnapshot.NewCache(tb.store, clock.Now)
	saveFeeds(t, other.Snapshot("user"), feed("user", "news", "News"), feed("user", "wire", "Wire"))
	wantTitles(t, feeds(t, self.Snapshot("user")), map[string]string{"news": "News", "wire": "Wire"})

	if err := other.Snapshot("user").DeleteFeed(context.Background(), "news"); err != nil {
		t.Fatal(err)
	}

	wantTitles(t, feeds(t, self.Snapshot("user")), map[string]string{"wire": "Wire"})
}

func TestDetailedFeedEditOnOneInstanceIsSeenByAnother(t *testing.T) {
	tb := newTable(t)
	clock := newClock()
	self, other := requestsnapshot.NewCache(tb.store, clock.Now), requestsnapshot.NewCache(tb.store, clock.Now)
	saveFeeds(t, other.Snapshot("user"), feed("user", "news", "Old"))
	wantTitles(t, detailedFeeds(t, self.Snapshot("user")), map[string]string{"news": "Old (detailed)"})

	saveFeeds(t, other.Snapshot("user"), feed("user", "news", "New"))

	wantTitles(t, detailedFeeds(t, self.Snapshot("user")), map[string]string{"news": "New (detailed)"})
}

func TestDecoratingLeavesThePlainFeedsAlone(t *testing.T) {
	tb := newTable(t)
	clock := newClock()
	cache := requestsnapshot.NewCache(tb.store, clock.Now)
	saveFeeds(t, cache.Snapshot("user"), feed("user", "news", "News"))
	request := cache.Snapshot("user")
	wantTitles(t, feeds(t, request), map[string]string{"news": "News"})

	wantTitles(t, detailedFeeds(t, request), map[string]string{"news": "News (detailed)"})

	wantTitles(t, feeds(t, request), map[string]string{"news": "News"})
	wantTitles(t, feeds(t, cache.Snapshot("user")), map[string]string{"news": "News"})
}

func TestOwnFeedWriteReloadsTheCopy(t *testing.T) {
	for _, test := range []struct {
		name  string
		write func(*requestsnapshot.Snapshot) error
		want  map[string]string
	}{
		{"save", func(s *requestsnapshot.Snapshot) error {
			return s.SaveFeeds(context.Background(), feed("user", "wire", "Wire"))
		}, map[string]string{"news": "Worker", "wire": "Wire"}},
		{"delete", func(s *requestsnapshot.Snapshot) error {
			return s.DeleteFeed(context.Background(), "missing")
		}, map[string]string{"news": "Worker"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			tb := newTable(t)
			clock := newClock()
			cache := requestsnapshot.NewCache(tb.store, clock.Now)
			saveFeeds(t, cache.Snapshot("user"), feed("user", "news", "News"))
			wantTitles(t, feeds(t, cache.Snapshot("user")), map[string]string{"news": "News"})
			wantTitles(t, detailedFeeds(t, cache.Snapshot("user")), map[string]string{"news": "News (detailed)"})
			tb.putFeedWithoutBump(t, feed("user", "news", "Worker"))

			if err := test.write(cache.Snapshot("user")); err != nil {
				t.Fatal(err)
			}

			// The copy was reloaded rather than patched, so the unbumped write shows.
			wantTitles(t, feeds(t, cache.Snapshot("user")), test.want)
			detailed := map[string]string{}
			for id, title := range test.want {
				detailed[id] = title + " (detailed)"
			}
			wantTitles(t, detailedFeeds(t, cache.Snapshot("user")), detailed)
		})
	}
}

func TestRequestSeesItsOwnFeedWrites(t *testing.T) {
	tb := newTable(t)
	clock := newClock()
	request := requestsnapshot.NewCache(tb.store, clock.Now).Snapshot("user")
	wantTitles(t, feeds(t, request), map[string]string{})
	wantTitles(t, detailedFeeds(t, request), map[string]string{})

	saveFeeds(t, request, feed("user", "news", "News"), feed("user", "wire", "Wire"))
	wantTitles(t, feeds(t, request), map[string]string{"news": "News", "wire": "Wire"})
	wantTitles(t, detailedFeeds(t, request), map[string]string{"news": "News (detailed)", "wire": "Wire (detailed)"})

	if err := request.DeleteFeed(context.Background(), "news"); err != nil {
		t.Fatal(err)
	}
	wantTitles(t, feeds(t, request), map[string]string{"wire": "Wire"})
	wantTitles(t, detailedFeeds(t, request), map[string]string{"wire": "Wire (detailed)"})
}

func TestRequestSeesOneFeedVersion(t *testing.T) {
	tb := newTable(t)
	clock := newClock()
	self, other := requestsnapshot.NewCache(tb.store, clock.Now), requestsnapshot.NewCache(tb.store, clock.Now)
	saveFeeds(t, other.Snapshot("user"), feed("user", "news", "Old"))
	request := self.Snapshot("user")
	_, before, err := request.Feeds(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	saveFeeds(t, other.Snapshot("user"), feed("user", "news", "New"))

	got, after, err := request.Feeds(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("FEED_VERSION moved from %d to %d within one request", before, after)
	}
	wantTitles(t, got, map[string]string{"news": "Old"})
	// The detailed list loads later in the request, from the same version.
	wantTitles(t, detailedFeeds(t, request), map[string]string{"news": "Old (detailed)"})
}

func TestFeedsExpireAfterAMinute(t *testing.T) {
	tb := newTable(t)
	clock := newClock()
	cache := requestsnapshot.NewCache(tb.store, clock.Now)
	saveFeeds(t, cache.Snapshot("user"), feed("user", "news", "News"))
	wantTitles(t, feeds(t, cache.Snapshot("user")), map[string]string{"news": "News"})
	wantTitles(t, detailedFeeds(t, cache.Snapshot("user")), map[string]string{"news": "News (detailed)"})
	tb.putFeedWithoutBump(t, feed("user", "news", "Worker"))

	clock.Advance(time.Minute - time.Second)
	wantTitles(t, feeds(t, cache.Snapshot("user")), map[string]string{"news": "News"})
	wantTitles(t, detailedFeeds(t, cache.Snapshot("user")), map[string]string{"news": "News (detailed)"})

	clock.Advance(time.Second)
	wantTitles(t, feeds(t, cache.Snapshot("user")), map[string]string{"news": "Worker"})
	wantTitles(t, detailedFeeds(t, cache.Snapshot("user")), map[string]string{"news": "Worker (detailed)"})
}

func TestFeedCacheEvictsItsOldestUserBeyond128(t *testing.T) {
	tb := newTable(t)
	clock := newClock()
	cache := requestsnapshot.NewCache(tb.store, clock.Now)
	saveFeeds(t, cache.Snapshot("user"), feed("user", "news", "News"))
	wantTitles(t, feeds(t, cache.Snapshot("user")), map[string]string{"news": "News"})
	tb.putFeedWithoutBump(t, feed("user", "news", "Worker"))
	for i := range 127 {
		clock.Advance(time.Millisecond)
		feeds(t, cache.Snapshot(fmt.Sprint("other-", i)))
	}
	wantTitles(t, feeds(t, cache.Snapshot("user")), map[string]string{"news": "News"})

	clock.Advance(time.Millisecond)
	feeds(t, cache.Snapshot("one-too-many"))

	wantTitles(t, feeds(t, cache.Snapshot("user")), map[string]string{"news": "Worker"})
}

func TestFeedCacheOutcomes(t *testing.T) {
	tb := newTable(t)
	clock := newClock()
	self, other := requestsnapshot.NewCache(tb.store, clock.Now), requestsnapshot.NewCache(tb.store, clock.Now)
	outcome := func(load func(*testing.T, *requestsnapshot.Snapshot) []domain.Feed) string {
		t.Helper()
		request := self.Snapshot("user")
		load(t, request)
		return request.CacheOutcomes()["FeedCache"]
	}
	for _, load := range []struct {
		name string
		load func(*testing.T, *requestsnapshot.Snapshot) []domain.Feed
	}{{"plain", feeds}, {"detailed", detailedFeeds}} {
		if got := outcome(load.load); got != "miss" {
			t.Fatalf("first %s load = %q, want miss", load.name, got)
		}
		if got := outcome(load.load); got != "hit" {
			t.Fatalf("second %s load = %q, want hit", load.name, got)
		}
		saveFeeds(t, other.Snapshot("user"), feed("user", "news", load.name))
		if got := outcome(load.load); got != "stale" {
			t.Fatalf("%s load after another instance's Feed write = %q, want stale", load.name, got)
		}
		clock.Advance(time.Minute)
		if got := outcome(load.load); got != "miss" {
			t.Fatalf("%s load after the TTL = %q, want miss", load.name, got)
		}
	}
	if got := self.Snapshot("user").CacheOutcomes()["FeedCache"]; got != "" {
		t.Fatalf("request that never loaded Feeds recorded FeedCache = %q", got)
	}
}

func TestFeedCacheOutcomeIsTheRequestsFirst(t *testing.T) {
	tb := newTable(t)
	clock := newClock()
	cache := requestsnapshot.NewCache(tb.store, clock.Now)
	feeds(t, cache.Snapshot("user"))
	request := cache.Snapshot("user")

	// The plain list hits, then the detailed list misses.
	feeds(t, request)
	detailedFeeds(t, request)

	if got := request.CacheOutcomes()["FeedCache"]; got != "hit" {
		t.Fatalf("FeedCache = %q, want the first load's hit", got)
	}
}
