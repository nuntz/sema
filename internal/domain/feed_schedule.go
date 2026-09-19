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
	hours := feed.FetchIntervalH
	if hours == 0 {
		hours = feed.EffectiveCadenceH
	}
	switch hours {
	case 3, 6, 24:
		return hours
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

// UpdateCadence keeps the seven UTC-day buckets and learns only after a full week.
func UpdateCadence(feed *Feed, now time.Time, enqueued int) {
	history := make(map[string]int, 7)
	cutoff := now.UTC().AddDate(0, 0, -6).Format("2006-01-02")
	today := now.UTC().Format("2006-01-02")
	for day, count := range feed.PublishHistory {
		if day >= cutoff && day <= today {
			history[day] = count
		}
	}
	if enqueued > 0 {
		history[today] += enqueued
	}
	feed.PublishHistory = history
	feed.EffectiveCadenceH = AutoCadence(*feed, now)
}

func AutoCadence(feed Feed, now time.Time) int {
	if feed.FetchIntervalH != 0 {
		return FeedIntervalHours(feed)
	}
	if FeedConnector(feed) == ConnectorReddit {
		return FeedIntervalHours(feed)
	}
	start, err := time.Parse(time.RFC3339Nano, feed.HistoryStartedAt)
	if err != nil || now.Sub(start) < 7*24*time.Hour {
		return 1
	}
	total := 0
	cutoff := now.UTC().AddDate(0, 0, -6).Format("2006-01-02")
	today := now.UTC().Format("2006-01-02")
	for day, count := range feed.PublishHistory {
		if day >= cutoff && day <= today {
			total += count
		}
	}
	switch {
	case total >= 28:
		return 1
	case total >= 10:
		return 3
	case total >= 3:
		return 6
	default:
		return 24
	}
}
