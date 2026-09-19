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
	refusalJitterWindow = 5 * time.Minute
)

func ScheduleKey(feed domain.Feed) string {
	return feed.PK + "#" + feed.FeedID
}

func nextFetchAfterError(feed domain.Feed, started time.Time, err error) (time.Time, bool) {
	var statusErr *connector.HTTPStatusError
	refused := errors.As(err, &statusErr) && (statusErr.StatusCode == http.StatusTooManyRequests || domain.FeedConnector(feed) == domain.ConnectorYouTube && (statusErr.StatusCode == 404 || statusErr.StatusCode == 500))
	if !refused {
		delay := time.Duration(1<<min(feed.ErrorCount, 5)) * time.Hour
		return started.Add(min(delay, 24*time.Hour)), false
	}
	if retryAt, ok := parseRetryAfter(statusErr.Header.Get("Retry-After"), started); ok {
		return retryAt, true
	}
	delay := time.Duration(min(domain.FeedIntervalHours(feed), 6)) * time.Hour
	return started.Add(delay).Add(domain.StableOffset(ScheduleKey(feed), refusalJitterWindow)), true
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
		feed.LastStatus = truncate(err.Error(), 240)
		next, refused := nextFetchAfterError(feed, now, err)
		if refused {
			if feed.RefusedSince == "" {
				feed.RefusedSince = domain.Timestamp(now)
			}
		} else {
			feed.RefusedSince = ""
			feed.ErrorCount++
			feed.LastError = truncate(err.Error(), 200)
			next, _ = nextFetchAfterError(feed, now, err)
		}
		feed.NextFetchAt = domain.Timestamp(next)
		return feed, refused
	}
	feed.RefusedSince = ""
	feed.ErrorCount = 0
	feed.LastError = ""
	feed.LastStatus = "200"
	if result.NotModified {
		feed.LastStatus = "304"
	}
	domain.UpdateCadence(&feed, now, 0)
	feed.NextFetchAt = domain.Timestamp(domain.NextFeedFetch(ScheduleKey(feed), now, domain.FeedIntervalHours(feed)))
	return feed, false
}
func Retry(feed domain.Feed, now time.Time) domain.Feed {
	if feed.Muted {
		return feed
	}
	feed.RefusedSince = ""
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
	if since, err := time.Parse(time.RFC3339Nano, feed.RefusedSince); err == nil && time.Since(since) > 24*time.Hour {
		return "broken"
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
