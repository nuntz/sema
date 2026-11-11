package requestsnapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

// storyTTL bounds how long a rendered Story list can serve. Its versions
// cover every user write a Story shows, so the TTL only bounds what moves no
// version: Clusters, Item content and the ranking model's priors.
const storyTTL = 4 * time.Minute

// storyKey is one user's Story filters, as the caller set them.
type storyKey struct {
	userID  string
	filters string
}

// storyVersions labels a Story list with the version of each kind of state it
// shows: Feed presentation, Read state, and Boost, Bury and Keep.
type storyVersions struct {
	feed, read, feedback int64
}

// storyRender is one rendered Story list within a request.
type storyRender struct {
	filters  string
	versions storyVersions
}

// Stories returns the user's Stories for filters, which must encode as JSON.
// render builds them from read, the Snapshot's Read markers, which it must
// not change. The list is cached under the filters plus the request's
// FEED_VERSION, READ_VERSION and FEEDBACK_VERSION, each read strongly
// consistent before rendering, so a Read, Feed edit or feedback write on any
// instance renders it again, and so does the request's own write.
//
// render may read the Snapshot's Read markers and Feeds, but must not write
// through it or ask it for Stories. It must read Boost, Bury and Keep state
// strongly consistent, or it could cache values older than FEEDBACK_VERSION.
// It must return the same type for every call on a Cache, and what it shows
// that no version moves can be up to four minutes old. The list is shared: callers must not change it.
func Stories[T any](ctx context.Context, s *Snapshot, filters any, render func(ctx context.Context, read map[string]bool) (T, error)) (T, error) {
	var zero T
	list, err := s.stories(ctx, filters, func(ctx context.Context, read map[string]bool) (any, error) {
		return render(ctx, read)
	})
	if err != nil {
		return zero, err
	}
	typed, ok := list.(T)
	if !ok {
		return zero, fmt.Errorf("cached Stories are %T, not %T", list, zero)
	}
	return typed, nil
}

func (s *Snapshot) stories(ctx context.Context, filters any, render func(context.Context, map[string]bool) (any, error)) (any, error) {
	encoded, err := json.Marshal(filters)
	if err != nil {
		return nil, err
	}
	s.storyMu.Lock()
	defer s.storyMu.Unlock()
	versions, read, err := s.storyVersions(ctx)
	if err != nil {
		return nil, err
	}
	rendered := storyRender{filters: string(encoded), versions: versions}
	if list, ok := s.renders[rendered]; ok {
		return list, nil
	}
	cache := s.cache
	key := storyKey{userID: s.userID, filters: string(encoded)}
	list, err := cache.stories.load(key, versions, cache.now(), s.recorder("StoryCache"), func() (any, error) {
		return render(ctx, read)
	})
	if err != nil {
		return nil, err
	}
	if s.renders == nil {
		s.renders = map[storyRender]any{}
	}
	s.renders[rendered] = list
	return list, nil
}

// storyVersions reads the request's three versions, each before what it
// labels, and returns the Read markers labelled with its READ_VERSION. The
// caller holds storyMu.
func (s *Snapshot) storyVersions(ctx context.Context) (storyVersions, map[string]bool, error) {
	s.feedMu.Lock()
	err := s.feedVersion(ctx)
	var feed int64
	if err == nil {
		feed = s.feed.version
	}
	s.feedMu.Unlock()
	if err != nil {
		return storyVersions{}, nil, err
	}
	feedback, err := s.feedbackVersion(ctx)
	if err != nil {
		return storyVersions{}, nil, err
	}
	read, readVersion, err := s.ReadMarkers(ctx)
	if err != nil {
		return storyVersions{}, nil, err
	}
	return storyVersions{feed: feed, read: readVersion, feedback: feedback}, read, nil
}

// SetKeep Keeps itemID, or Unkeeps it when kept is false, and returns the
// Kept Item's archive key and the user's Kept count.
func (s *Snapshot) SetKeep(ctx context.Context, itemID string, kept bool) (archiveSK string, keptCount int, err error) {
	err = s.writeFeedback(func() error {
		archiveSK, keptCount, err = s.cache.store.SetHeart(ctx, s.userID, itemID, kept)
		return err
	})
	return archiveSK, keptCount, err
}

// SetSignal Boosts item (value 1), Buries it (-1) or makes it neutral (0).
func (s *Snapshot) SetSignal(ctx context.Context, item domain.Item, value int) error {
	return s.writeFeedback(func() error {
		return s.cache.store.SetSignal(ctx, s.userID, item, value)
	})
}

// writeFeedback runs a write that may move FEEDBACK_VERSION, even when it
// fails partway, so the request reads the version again on next use and its
// Stories render under the new one. A write error drops the user's Stories
// too, in case the write committed but its bump failed.
func (s *Snapshot) writeFeedback(write func() error) error {
	s.feedbackMu.Lock()
	err := write()
	s.feedback = nil
	s.feedbackMu.Unlock()
	return s.dropStoriesOnError(err)
}

// dropStoriesOnError forgets the instance's and the request's Stories for the
// user when err is set, and returns err. A write that failed may have
// committed without moving its version, and Stories show every kind of
// write. The caller holds no other kind's lock, since Stories take them
// while holding storyMu.
func (s *Snapshot) dropStoriesOnError(err error) error {
	if err == nil {
		return nil
	}
	s.storyMu.Lock()
	defer s.storyMu.Unlock()
	s.renders = nil
	s.cache.stories.dropMatching(func(key storyKey) bool { return key.userID == s.userID })
	return err
}

// feedbackVersion reads FEEDBACK_VERSION once per request, strongly
// consistent, until the request's own feedback write.
func (s *Snapshot) feedbackVersion(ctx context.Context) (int64, error) {
	s.feedbackMu.Lock()
	defer s.feedbackMu.Unlock()
	if s.feedback == nil {
		version, err := s.cache.store.FeedbackVersion(ctx, s.userID)
		if err != nil {
			return 0, err
		}
		s.feedback = &version
	}
	return *s.feedback, nil
}
