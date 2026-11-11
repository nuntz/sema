package requestsnapshot_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/nuntz/sema/cmd/api/internal/requestsnapshot"
	"github.com/nuntz/sema/internal/domain"
)

func (s *scriptedStore) FeedVersion(context.Context, string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.feedVersion, nil
}

func (s *scriptedStore) Feeds(context.Context, string) ([]domain.Feed, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.feedsErr != nil {
		return nil, s.feedsErr
	}
	return slices.Clone(s.feeds), nil
}

func (s *scriptedStore) SaveFeeds(context.Context, string, []domain.Feed) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return 0, s.feedWriteErr
}

func (s *scriptedStore) DeleteFeed(context.Context, string, string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return 0, s.feedWriteErr
}

// scriptFeeds sets the Feeds storage holds now.
func (s *scriptedStore) scriptFeeds(version int64, feeds ...domain.Feed) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.feedVersion, s.feeds = version, feeds
}

func scriptedFeed(feedID, title string) domain.Feed {
	return domain.Feed{FeedID: feedID, CustomTitle: title}
}

func TestFeedWriteThatFailsDropsTheCopy(t *testing.T) {
	for _, write := range []struct {
		name  string
		write func(*requestsnapshot.Snapshot) error
	}{
		{"save", func(s *requestsnapshot.Snapshot) error {
			return s.SaveFeeds(context.Background(), scriptedFeed("news", "New"))
		}},
		{"delete", func(s *requestsnapshot.Snapshot) error { return s.DeleteFeed(context.Background(), "news") }},
	} {
		for _, test := range []struct {
			name    string
			version int64 // FEED_VERSION after the failed write
		}{
			// Some writes committed and bumped before one failed.
			{"partway", 2},
			// The write committed but the bump failed.
			{"failed bump", 1},
		} {
			t.Run(write.name+" "+test.name, func(t *testing.T) {
				db := &scriptedStore{}
				db.scriptFeeds(1, scriptedFeed("news", "Old"))
				cache := requestsnapshot.NewCache(db, newClock().Now)
				wantTitles(t, feeds(t, cache.Snapshot("user")), map[string]string{"news": "Old"})
				wantTitles(t, detailedFeeds(t, cache.Snapshot("user")), map[string]string{"news": "Old (detailed)"})
				request := cache.Snapshot("user")
				wantTitles(t, feeds(t, request), map[string]string{"news": "Old"})
				db.feedWriteErr = errors.New("write failed")
				db.scriptFeeds(test.version, scriptedFeed("news", "New"))

				if err := write.write(request); err == nil {
					t.Fatal("Feed write hid the error")
				}

				wantTitles(t, feeds(t, request), map[string]string{"news": "New"})
				wantTitles(t, feeds(t, cache.Snapshot("user")), map[string]string{"news": "New"})
				wantTitles(t, detailedFeeds(t, cache.Snapshot("user")), map[string]string{"news": "New (detailed)"})
			})
		}
	}
}

func TestFeedLoadErrorDropsTheCopy(t *testing.T) {
	for _, load := range []struct {
		name string
		load func(*testing.T, *requestsnapshot.Snapshot) []domain.Feed
		want string
	}{{"plain", feeds, "Unbumped"}, {"detailed", detailedFeeds, "Unbumped (detailed)"}} {
		t.Run(load.name, func(t *testing.T) {
			db := &scriptedStore{}
			db.scriptFeeds(1, scriptedFeed("news", "Old"))
			cache := requestsnapshot.NewCache(db, newClock().Now)
			load.load(t, cache.Snapshot("user"))
			db.scriptFeeds(2, scriptedFeed("news", "New"))
			db.feedsErr = errors.New("load failed")
			if _, err := cache.Snapshot("user").DetailedFeeds(context.Background(), func(context.Context, []domain.Feed) error { return nil }); err == nil {
				t.Fatal("DetailedFeeds hid the load error")
			}

			// Back at the cached version, so only a dropped copy reloads.
			db.feedsErr = nil
			db.scriptFeeds(1, scriptedFeed("news", "Unbumped"))

			wantTitles(t, load.load(t, cache.Snapshot("user")), map[string]string{"news": load.want})
		})
	}
}

func TestDecorationErrorDropsTheDetailedCopy(t *testing.T) {
	db := &scriptedStore{}
	db.scriptFeeds(1, scriptedFeed("news", "Old"))
	cache := requestsnapshot.NewCache(db, newClock().Now)
	detailedFeeds(t, cache.Snapshot("user"))
	db.scriptFeeds(2, scriptedFeed("news", "New"))
	if _, err := cache.Snapshot("user").DetailedFeeds(context.Background(), func(context.Context, []domain.Feed) error {
		return errors.New("decoration failed")
	}); err == nil {
		t.Fatal("DetailedFeeds hid the decoration error")
	}

	db.scriptFeeds(1, scriptedFeed("news", "Unbumped"))

	wantTitles(t, detailedFeeds(t, cache.Snapshot("user")), map[string]string{"news": "Unbumped (detailed)"})
}

func TestSnapshotLoadsFeedsOnceUnderConcurrency(t *testing.T) {
	db := &countingStore{}
	request := requestsnapshot.NewCache(db, newClock().Now).Snapshot("user")
	var wg sync.WaitGroup
	got := make([][]domain.Feed, 16)
	for i := range got {
		wg.Go(func() {
			var feeds []domain.Feed
			var err error
			if i%2 == 0 {
				feeds, _, err = request.Feeds(context.Background())
			} else {
				feeds, err = request.DetailedFeeds(context.Background(), func(context.Context, []domain.Feed) error { return nil })
			}
			if err != nil {
				t.Error(err)
			}
			got[i] = feeds
		})
	}
	wg.Wait()
	for _, feeds := range got {
		wantTitles(t, feeds, map[string]string{"load-1": ""})
	}
}

// Feeds names the load that returned them, as LoadReadMarkers does.
func (s *countingStore) Feeds(context.Context, string) ([]domain.Feed, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	return []domain.Feed{{FeedID: fmt.Sprint("load-", s.loads)}}, nil
}
