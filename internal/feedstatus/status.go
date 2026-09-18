// Package feedstatus owns Feed Status transitions and fetch scheduling.
package feedstatus

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nuntz/sema/internal/connector"
	"github.com/nuntz/sema/internal/domain"
)

const (
	defaultRateLimitDelay = 15 * time.Minute
	rateLimitJitterWindow = 5 * time.Minute
)

func ScheduleKey(feed domain.Feed) string {
	return feed.PK + "#" + feed.FeedID
}

func nextFetchAfterError(feed domain.Feed, started time.Time, err error) (time.Time, bool) {
	maximum := time.Duration(max(24, domain.FeedIntervalHours(feed))) * time.Hour
	var statusErr *connector.HTTPStatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusTooManyRequests {
		delay := time.Duration(1<<min(feed.ErrorCount, 5)) * time.Hour
		return started.Add(min(delay, maximum)), false
	}

	next := started.Add(defaultRateLimitDelay)
	if retryAt, ok := parseRetryAfter(statusErr.Header.Get("Retry-After"), started); ok {
		next = retryAt
	}
	next = next.Add(domain.StableOffset(ScheduleKey(feed), rateLimitJitterWindow))
	if capAt := started.Add(maximum); next.After(capAt) {
		next = capAt
	}
	return next, true
}

func parseRetryAfter(value string, now time.Time) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		return now.Add(time.Duration(seconds) * time.Second), true
	}
	if retryAt, err := http.ParseTime(value); err == nil && !retryAt.Before(now) {
		return retryAt.UTC(), true
	}
	return time.Time{}, false
}

func AfterFetch(feed domain.Feed, result domain.FetchResult, err error, now time.Time) (domain.Feed, bool) {
	feed.LastFetchAt = domain.Timestamp(now)
	if err != nil {
		feed.ErrorCount++
		feed.LastStatus = truncate(err.Error(), 240)
		feed.LastError = truncate(err.Error(), 200)
		next, limited := nextFetchAfterError(feed, now, err)
		feed.NextFetchAt = domain.Timestamp(next)
		return feed, limited
	}
	feed.ErrorCount = 0
	feed.LastError = ""
	feed.LastStatus = "200"
	if result.NotModified {
		feed.LastStatus = "304"
	}
	feed.NextFetchAt = domain.Timestamp(domain.NextFeedFetch(ScheduleKey(feed), now, domain.FeedIntervalHours(feed)))
	return feed, false
}
func Retry(feed domain.Feed, now time.Time) domain.Feed {
	if feed.Muted {
		return feed
	}
	feed.ErrorCount = 0
	feed.LastError = ""
	feed.LastStatus = "queued"
	feed.NextFetchAt = domain.Timestamp(now)
	return feed
}
func Mute(feed domain.Feed) domain.Feed { feed.Muted = true; return feed }
func Unmute(feed domain.Feed, now time.Time) domain.Feed {
	feed.Muted = false
	feed.LastStatus = "queued"
	feed.NextFetchAt = domain.Timestamp(now)
	return feed
}
func Status(feed domain.Feed) string {
	if feed.Muted {
		return "muted"
	}
	if feed.ErrorCount >= 3 {
		return "broken"
	}
	if feed.ErrorCount > 0 {
		return "slowed"
	}
	return "ok"
}
func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
