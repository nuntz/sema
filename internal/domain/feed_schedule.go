package domain

import (
	"hash/fnv"
	"time"
)

const (
	feedFetchPeriod = time.Hour
	minFeedDelay    = 30 * time.Minute
)

// NextFeedFetch assigns each feed a stable phase within the hour. Requiring the
// next occurrence to be at least 30 minutes away prevents an early scheduler
// invocation from selecting the phase that the current fetch is satisfying.
func NextFeedFetch(key string, after time.Time) time.Time {
	after = after.UTC()
	next := after.Truncate(feedFetchPeriod).Add(StableOffset(key, feedFetchPeriod))
	for next.Before(after.Add(minFeedDelay)) {
		next = next.Add(feedFetchPeriod)
	}
	return next
}

// StableOffset returns a deterministic offset in [0, window) for key.
func StableOffset(key string, window time.Duration) time.Duration {
	if window <= 0 {
		return 0
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(key))
	return time.Duration(hash.Sum64() % uint64(window))
}
