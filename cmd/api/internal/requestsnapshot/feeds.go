package requestsnapshot

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

// feedTTL bounds how long a Feed list copy can serve Feed writes that did not
// move FEED_VERSION: the feed worker's and backfills' (ADR 0006).
const feedTTL = time.Minute

// feedState is a Snapshot's FEED_VERSION and the Feed lists labelled with it.
// The lists are never changed once loaded: writes drop the whole state.
type feedState struct {
	version        int64
	plain          []domain.Feed
	plainLoaded    bool
	detailed       []domain.Feed
	detailedLoaded bool
}

// Feeds returns the user's Feeds and the FEED_VERSION they are labelled with.
// They load on first use and stay fixed for the request, apart from its own
// Feed writes. The list is shared: callers must not change it.
func (s *Snapshot) Feeds(ctx context.Context) ([]domain.Feed, int64, error) {
	s.feedMu.Lock()
	defer s.feedMu.Unlock()
	feeds, err := s.plainFeeds(ctx)
	if err != nil {
		return nil, 0, err
	}
	return feeds, s.feed.version, nil
}

// DetailedFeeds returns the user's Feeds after decorate, which gets its own
// copy of the plain list to change in place. The decorated list is cached
// like the plain one, so decorate must be the same for every call on a
// Cache, and what it adds that no Feed write moves (such as ranking priors)
// can be up to a TTL old. The list is shared: callers must not change it.
func (s *Snapshot) DetailedFeeds(ctx context.Context, decorate func(context.Context, []domain.Feed) error) ([]domain.Feed, error) {
	s.feedMu.Lock()
	defer s.feedMu.Unlock()
	if err := s.feedVersion(ctx); err != nil {
		return nil, err
	}
	if s.feed.detailedLoaded {
		return s.feed.detailed, nil
	}
	cache := s.cache
	now := cache.now()
	detailed, outcome := cache.detailedFeeds.lookup(s.userID, s.feed.version, now)
	s.record("FeedCache", outcome)
	if outcome != outcomeHit {
		plain, err := s.plainFeeds(ctx)
		if err != nil {
			cache.detailedFeeds.drop(s.userID)
			return nil, err
		}
		detailed = cloneFeeds(plain)
		if err := decorate(ctx, detailed); err != nil {
			cache.detailedFeeds.drop(s.userID)
			return nil, err
		}
		cache.detailedFeeds.fill(s.userID, s.feed.version, detailed, now)
	}
	s.feed.detailed, s.feed.detailedLoaded = detailed, true
	return detailed, nil
}

// feedVersion reads FEED_VERSION once per request, strongly consistent and
// before any Feeds, so a copy is never labelled newer than its contents. The
// caller holds feedMu.
func (s *Snapshot) feedVersion(ctx context.Context) error {
	if s.feed != nil {
		return nil
	}
	version, err := s.cache.store.FeedVersion(ctx, s.userID)
	if err != nil {
		return err
	}
	s.feed = &feedState{version: version}
	return nil
}

// plainFeeds loads the plain list once per request. The caller holds feedMu.
func (s *Snapshot) plainFeeds(ctx context.Context) ([]domain.Feed, error) {
	if err := s.feedVersion(ctx); err != nil {
		return nil, err
	}
	if s.feed.plainLoaded {
		return s.feed.plain, nil
	}
	cache := s.cache
	now := cache.now()
	feeds, outcome := cache.feeds.lookup(s.userID, s.feed.version, now)
	s.record("FeedCache", outcome)
	if outcome != outcomeHit {
		var err error
		feeds, err = cache.store.Feeds(ctx, s.userID)
		if err != nil {
			cache.feeds.drop(s.userID)
			return nil, err
		}
		cache.feeds.fill(s.userID, s.feed.version, feeds, now)
	}
	s.feed.plain, s.feed.plainLoaded = feeds, true
	return feeds, nil
}

// SaveFeeds writes feeds as the user's own edit: add, edit, retry or import.
// Feed writes are rare and a reload is one Query, so rather than patching,
// it drops the instance's Feed copies and the request reloads both its
// FEED_VERSION and its Feeds on next use. It drops them after an error too,
// because the write may have committed partway.
func (s *Snapshot) SaveFeeds(ctx context.Context, feeds ...domain.Feed) error {
	s.feedMu.Lock()
	defer s.feedMu.Unlock()
	_, err := s.cache.store.SaveFeeds(ctx, s.userID, feeds)
	s.dropFeeds()
	return err
}

// DeleteFeed deletes one of the user's Feeds, dropping Feed copies as
// SaveFeeds does.
func (s *Snapshot) DeleteFeed(ctx context.Context, feedID string) error {
	s.feedMu.Lock()
	defer s.feedMu.Unlock()
	_, err := s.cache.store.DeleteFeed(ctx, s.userID, feedID)
	s.dropFeeds()
	return err
}

// dropFeeds forgets the instance's and the request's Feeds. The caller holds
// feedMu.
func (s *Snapshot) dropFeeds() {
	s.cache.feeds.drop(s.userID)
	s.cache.detailedFeeds.drop(s.userID)
	s.feed = nil
}

// cloneFeeds copies feeds deeply enough that changing the copy, its Tags or
// its publish history leaves the original alone.
func cloneFeeds(feeds []domain.Feed) []domain.Feed {
	cloned := slices.Clone(feeds)
	for i := range cloned {
		cloned[i].Tags = slices.Clone(cloned[i].Tags)
		cloned[i].PublishHistory = maps.Clone(cloned[i].PublishHistory)
	}
	return cloned
}
