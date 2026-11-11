package requestsnapshot_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"testing"

	"github.com/nuntz/sema/cmd/api/internal/requestsnapshot"
	"github.com/nuntz/sema/internal/domain"
)

func (s *scriptedStore) FeedbackVersion(context.Context, string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.feedbackVersion, nil
}

func (s *scriptedStore) SetHeart(context.Context, string, string, bool) (string, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return "", 0, s.feedbackWriteErr
}

func (s *scriptedStore) SetSignal(context.Context, string, domain.Item, int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.feedbackWriteErr
}

func TestWriteThatFailsRendersTheStoriesAgain(t *testing.T) {
	failed := errors.New("write failed")
	for _, write := range []struct {
		name  string
		fail  func(db *scriptedStore, bumped bool)
		write func(*requestsnapshot.Snapshot) error
	}{
		{"read", func(db *scriptedStore, bumped bool) {
			db.setReadErr = failed
			if bumped {
				db.script(2)
			}
		}, func(s *requestsnapshot.Snapshot) error { return s.SetRead(context.Background(), []string{"a"}, true) }},
		{"feed save", func(db *scriptedStore, bumped bool) {
			db.feedWriteErr = failed
			if bumped {
				db.scriptFeeds(2)
			}
		}, func(s *requestsnapshot.Snapshot) error {
			return s.SaveFeeds(context.Background(), scriptedFeed("news", "New"))
		}},
		{"feed delete", func(db *scriptedStore, bumped bool) {
			db.feedWriteErr = failed
			if bumped {
				db.scriptFeeds(2)
			}
		}, func(s *requestsnapshot.Snapshot) error { return s.DeleteFeed(context.Background(), "news") }},
		{"keep", func(db *scriptedStore, bumped bool) {
			db.feedbackWriteErr = failed
			if bumped {
				db.feedbackVersion = 2
			}
		}, func(s *requestsnapshot.Snapshot) error {
			_, _, err := s.SetKeep(context.Background(), "a", true)
			return err
		}},
		{"signal", func(db *scriptedStore, bumped bool) {
			db.feedbackWriteErr = failed
			if bumped {
				db.feedbackVersion = 2
			}
		}, func(s *requestsnapshot.Snapshot) error {
			return s.SetSignal(context.Background(), domain.Item{ItemID: "a"}, 1)
		}},
	} {
		for _, test := range []struct {
			name   string
			bumped bool
		}{
			// The write committed partway and moved the version.
			{"partway", true},
			// The write committed but the bump failed, so no version moved.
			{"failed bump", false},
		} {
			t.Run(write.name+" "+test.name, func(t *testing.T) {
				db := &scriptedStore{feedbackVersion: 1}
				db.script(1)
				db.scriptFeeds(1)
				cache := requestsnapshot.NewCache(db, newClock().Now)
				r := &renderer{}
				r.stories(t, cache.Snapshot("user"), storyFilters{})
				request := cache.Snapshot("user")
				wantRender(t, r.stories(t, request, storyFilters{}), 1)
				write.fail(db, test.bumped)

				if err := write.write(request); !errors.Is(err, failed) {
					t.Fatalf("%s = %v, want the write error", write.name, err)
				}

				if got := r.stories(t, request, storyFilters{}); got.render == 1 {
					t.Fatal("the request kept Stories rendered before its failed write")
				}
				if got := r.stories(t, cache.Snapshot("user"), storyFilters{}); got.render == 1 {
					t.Fatal("the instance kept Stories rendered before a failed write")
				}
			})
		}
	}
}

func TestStoryRenderErrorDropsTheCopy(t *testing.T) {
	db := &scriptedStore{feedbackVersion: 1}
	cache := requestsnapshot.NewCache(db, newClock().Now)
	r := &renderer{}
	wantRender(t, r.stories(t, cache.Snapshot("user"), storyFilters{}), 1)
	db.feedbackVersion = 2
	if _, err := requestsnapshot.Stories(context.Background(), cache.Snapshot("user"), storyFilters{}, func(context.Context, map[string]bool) (rendered, error) {
		return rendered{}, errors.New("render failed")
	}); err == nil {
		t.Fatal("Stories hid the render error")
	}

	// Back at the cached version, so only a dropped copy renders again.
	db.feedbackVersion = 1

	wantRender(t, r.stories(t, cache.Snapshot("user"), storyFilters{}), 2)
}

func TestSnapshotRendersStoriesOnceUnderConcurrency(t *testing.T) {
	db := &countingStore{}
	request := requestsnapshot.NewCache(db, newClock().Now).Snapshot("user")
	r := &renderer{}
	// Rendering reads the request's own Feeds and Read markers, as the API's
	// render does for Feed presentation.
	render := func(ctx context.Context, read map[string]bool) (rendered, error) {
		if _, _, err := request.Feeds(ctx); err != nil {
			return rendered{}, err
		}
		if _, _, err := request.ReadMarkers(ctx); err != nil {
			return rendered{}, err
		}
		return r.render(ctx, read)
	}
	var wg sync.WaitGroup
	got := make([]*rendered, 16)
	for i := range got {
		wg.Go(func() {
			var err error
			switch i % 3 {
			case 0:
				var stories rendered
				stories, err = requestsnapshot.Stories(context.Background(), request, storyFilters{}, render)
				got[i] = &stories
			case 1:
				_, _, err = request.Feeds(context.Background())
			default:
				_, _, err = request.ReadMarkers(context.Background())
			}
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	read := slices.Sorted(maps.Keys(readMarkers(t, request)))
	for _, stories := range got {
		if stories != nil {
			wantRender(t, *stories, 1, read...)
		}
	}
}
