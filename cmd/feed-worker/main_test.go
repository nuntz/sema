package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/nuntz/sema/internal/connector"
	"github.com/nuntz/sema/internal/domain"
)

type fakeFeedStore struct {
	feed     domain.Feed
	putErr   error
	putFeeds []domain.Feed
}

func (f *fakeFeedStore) Feed(context.Context, string, string) (domain.Feed, error) {
	return f.feed, nil
}

func (f *fakeFeedStore) PutFeed(_ context.Context, feed domain.Feed) error {
	f.putFeeds = append(f.putFeeds, feed)
	return f.putErr
}

func (*fakeFeedStore) ItemExists(context.Context, string, string, time.Time) (bool, error) {
	return false, nil
}

func (*fakeFeedStore) PutContent(context.Context, string, string, []byte) error {
	return nil
}

type failingConnector struct {
	err error
}

func (f failingConnector) Fetch(context.Context, domain.Feed) (domain.FetchResult, error) {
	return domain.FetchResult{}, f.err
}

func TestFetchFailureIsConsumedAfterPersistingBackoff(t *testing.T) {
	store := &fakeFeedStore{feed: domain.Feed{PK: "U#user", SK: "F#feed", FeedID: "feed", URL: "https://example.com/feed", ErrorCount: 1}}
	handler := &handler{store: store, connector: failingConnector{err: errors.New("upstream timeout")}}
	response, err := handler.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "message", Body: `{"user":"user","feed_id":"feed"}`}}})
	if err != nil || len(response.BatchItemFailures) != 0 {
		t.Fatalf("run = %#v, %v", response, err)
	}
	if len(store.putFeeds) != 1 {
		t.Fatalf("feed writes = %d", len(store.putFeeds))
	}
	feed := store.putFeeds[0]
	last, lastErr := time.Parse(time.RFC3339Nano, feed.LastFetchAt)
	next, nextErr := time.Parse(time.RFC3339Nano, feed.NextFetchAt)
	if lastErr != nil || nextErr != nil || next.Sub(last) != 4*time.Hour || feed.ErrorCount != 2 || feed.LastStatus != "upstream timeout" {
		t.Fatalf("failed feed = %#v, last error %v, next error %v", feed, lastErr, nextErr)
	}
}

func TestFetchFailureRetriesWhenBackoffWriteFails(t *testing.T) {
	store := &fakeFeedStore{
		feed:   domain.Feed{PK: "U#user", SK: "F#feed", FeedID: "feed", URL: "https://example.com/feed"},
		putErr: errors.New("dynamo unavailable"),
	}
	handler := &handler{store: store, connector: failingConnector{err: errors.New("bad feed")}}
	if err := handler.process(context.Background(), `{"user":"user","feed_id":"feed"}`); err == nil {
		t.Fatal("process succeeded despite failed backoff write")
	}
}

func TestRateLimitIsPersistedWithoutExponentialBackoff(t *testing.T) {
	headers := make(http.Header)
	headers.Set("Retry-After", "120")
	statusErr := &connector.HTTPStatusError{StatusCode: http.StatusTooManyRequests, Header: headers}
	store := &fakeFeedStore{feed: domain.Feed{
		PK: "U#user", SK: "F#feed", FeedID: "feed", URL: "https://example.com/feed", ErrorCount: 3,
	}}
	handler := &handler{store: store, connector: failingConnector{err: statusErr}}

	if err := handler.process(context.Background(), `{"user":"user","feed_id":"feed"}`); err != nil {
		t.Fatal(err)
	}
	if len(store.putFeeds) != 1 {
		t.Fatalf("feed writes = %d", len(store.putFeeds))
	}
	feed := store.putFeeds[0]
	last, lastErr := time.Parse(time.RFC3339Nano, feed.LastFetchAt)
	next, nextErr := time.Parse(time.RFC3339Nano, feed.NextFetchAt)
	wantDelay := 2*time.Minute + domain.StableOffset(feedScheduleKey(feed), rateLimitJitterWindow)
	if lastErr != nil || nextErr != nil || next.Sub(last) != wantDelay || feed.ErrorCount != 4 || feed.LastStatus != "feed returned HTTP 429" {
		t.Fatalf("rate-limited feed = %#v, last error %v, next error %v, want delay %v", feed, lastErr, nextErr, wantDelay)
	}
}

func TestRateLimitHonorsRetryAfterWithPositiveJitter(t *testing.T) {
	started := time.Date(2026, 8, 23, 14, 20, 0, 0, time.UTC)
	feed := domain.Feed{PK: "U#user", FeedID: "feed", ErrorCount: 1}
	headers := make(http.Header)
	headers.Set("Retry-After", "120")

	next, rateLimited := nextFetchAfterError(feed, started, &connector.HTTPStatusError{StatusCode: http.StatusTooManyRequests, Header: headers})
	want := started.Add(2*time.Minute + domain.StableOffset(feedScheduleKey(feed), rateLimitJitterWindow))
	if !rateLimited || next != want || next.Before(started.Add(2*time.Minute)) {
		t.Fatalf("next = %s, rate limited = %v, want %s", next, rateLimited, want)
	}
}

func TestRateLimitAcceptsHTTPDateAndFallsBackWhenInvalid(t *testing.T) {
	started := time.Date(2026, 8, 23, 14, 20, 0, 0, time.UTC)
	feed := domain.Feed{PK: "U#user", FeedID: "feed", ErrorCount: 4}
	jitter := domain.StableOffset(feedScheduleKey(feed), rateLimitJitterWindow)

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
