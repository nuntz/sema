package feedstatus

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/connector"
	"github.com/nuntz/sema/internal/domain"
)

func TestRateLimitHonorsRetryAfterWithPositiveJitter(t *testing.T) {
	started := time.Date(2026, 8, 23, 14, 20, 0, 0, time.UTC)
	feed := domain.Feed{PK: "U#user", FeedID: "feed", ErrorCount: 1}
	headers := make(http.Header)
	headers.Set("Retry-After", "120")

	next, rateLimited := nextFetchAfterError(feed, started, &connector.HTTPStatusError{StatusCode: http.StatusTooManyRequests, Header: headers})
	want := started.Add(2*time.Minute + domain.StableOffset(ScheduleKey(feed), rateLimitJitterWindow))
	if !rateLimited || next != want || next.Before(started.Add(2*time.Minute)) {
		t.Fatalf("next = %s, rate limited = %v, want %s", next, rateLimited, want)
	}
}

func TestRateLimitAcceptsHTTPDateAndFallsBackWhenInvalid(t *testing.T) {
	started := time.Date(2026, 8, 23, 14, 20, 0, 0, time.UTC)
	feed := domain.Feed{PK: "U#user", FeedID: "feed", ErrorCount: 4}
	jitter := domain.StableOffset(ScheduleKey(feed), rateLimitJitterWindow)

	headers := make(http.Header)
	headers.Set("Retry-After", started.Add(7*time.Minute).Format(http.TimeFormat))
	next, rateLimited := nextFetchAfterError(feed, started, &connector.HTTPStatusError{StatusCode: http.StatusTooManyRequests, Header: headers})
	if !rateLimited || next != started.Add(7*time.Minute).Add(jitter) {
		t.Fatalf("HTTP-date next = %s, rate limited = %v", next, rateLimited)
	}

	headers.Set("Retry-After", "not-a-date")
	next, rateLimited = nextFetchAfterError(feed, started, &connector.HTTPStatusError{StatusCode: http.StatusTooManyRequests, Header: headers})
	if !rateLimited || next != started.Add(defaultRateLimitDelay).Add(jitter) {
		t.Fatalf("fallback next = %s, rate limited = %v", next, rateLimited)
	}
}

func TestNonRateLimitKeepsExponentialBackoff(t *testing.T) {
	started := time.Date(2026, 8, 23, 14, 20, 0, 0, time.UTC)
	feed := domain.Feed{ErrorCount: 3}

	next, rateLimited := nextFetchAfterError(feed, started, &connector.HTTPStatusError{StatusCode: http.StatusServiceUnavailable})
	if rateLimited || next != started.Add(8*time.Hour) {
		t.Fatalf("next = %s, rate limited = %v", next, rateLimited)
	}
}

func TestErrorBackoffCapsAtTwentyFourHours(t *testing.T) {
	started := time.Date(2026, 8, 23, 14, 20, 0, 0, time.UTC)
	feed := domain.Feed{ErrorCount: 12, FetchIntervalH: 24}
	next, rateLimited := nextFetchAfterError(feed, started, errors.New("offline"))
	if rateLimited || next != started.Add(24*time.Hour) {
		t.Fatalf("next = %s, rate limited = %v", next, rateLimited)
	}
}

func TestFeedStatusTransitions(t *testing.T) {
	now := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	feed := domain.Feed{FeedID: "feed"}
	for _, want := range []string{"slowed", "slowed", "broken"} {
		feed, _ = AfterFetch(feed, domain.FetchResult{}, errors.New("offline"), now)
		if Status(feed) != want {
			t.Fatalf("status=%s want=%s", Status(feed), want)
		}
	}
	feed = Mute(feed)
	if Status(feed) != "muted" || Retry(feed, now).LastStatus != feed.LastStatus {
		t.Fatal("muted retry changed feed")
	}
	feed = Unmute(feed, now)
	if feed.Muted || feed.NextFetchAt != domain.Timestamp(now) {
		t.Fatal("unmute did not queue")
	}
	feed = Retry(feed, now)
	if Status(feed) != "ok" || feed.LastError != "" {
		t.Fatal("retry did not reset errors")
	}
	for _, modified := range []bool{true, false} {
		got, _ := AfterFetch(domain.Feed{ErrorCount: 4, LastError: "offline"}, domain.FetchResult{NotModified: modified}, nil, now)
		if Status(got) != "ok" || got.LastError != "" || got.NextFetchAt == "" {
			t.Fatal("success did not reset errors and schedule fetch")
		}
	}
}

func TestFetchErrorTrimsWhitespace(t *testing.T) {
	feed, _ := AfterFetch(domain.Feed{}, domain.FetchResult{}, errors.New("  unavailable\n"), time.Now())
	if feed.LastStatus != "unavailable" || feed.LastError != "unavailable" {
		t.Fatalf("error text = %q / %q", feed.LastStatus, feed.LastError)
	}
}
