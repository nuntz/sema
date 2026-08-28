package domain

import (
	"hash/fnv"
	"time"
)

const (
	defaultFeedFetchPeriod = time.Hour
	minFeedDelay           = 30 * time.Minute
)

// NextFeedFetch assigns each feed a stable phase within the hour. Requiring the
// next occurrence to be at least 30 minutes away prevents an early scheduler
// invocation from selecting the phase that the current fetch is satisfying.
func NextFeedFetch(key string, after time.Time, intervalHours ...int) time.Time {
	after = after.UTC()
	period := defaultFeedFetchPeriod
	if len(intervalHours) > 0 {
		switch intervalHours[0] {
		case 3, 6, 24:
			period = time.Duration(intervalHours[0]) * time.Hour
		}
	}
	next := after.Truncate(period).Add(StableOffset(key, period))
	for next.Before(after.Add(minFeedDelay)) {
		next = next.Add(period)
	}
	return next
}

func FeedIntervalHours(feed Feed) int {
	switch feed.FetchIntervalH {
	case 3, 6, 24:
		return feed.FetchIntervalH
	default:
		return 1
	}
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
