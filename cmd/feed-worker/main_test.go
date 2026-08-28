package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/nuntz/sema/internal/connector"
	"github.com/nuntz/sema/internal/domain"
)

type fakeFeedStore struct {
	mu       sync.Mutex
	feed     domain.Feed
	feeds    map[string]domain.Feed
	putErr   error
	putFeeds []domain.Feed
	existing map[string]bool
}

func (f *fakeFeedStore) Feed(_ context.Context, _, feedID string) (domain.Feed, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.feeds != nil {
		return f.feeds[feedID], nil
	}
	return f.feed, nil
}

func (f *fakeFeedStore) PutFeed(_ context.Context, feed domain.Feed) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.putFeeds = append(f.putFeeds, feed)
	if f.feeds != nil {
		f.feeds[feed.FeedID] = feed
	}
	return f.putErr
}

func (f *fakeFeedStore) ItemExists(_ context.Context, _, itemID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.existing[itemID], nil
}

func (*fakeFeedStore) PutContent(context.Context, string, string, []byte) error {
	return nil
}

type failingConnector struct {
	err error
}

type resultConnector struct{ result domain.FetchResult }

type connectorFunc func(context.Context, domain.Feed) (domain.FetchResult, error)

func (f connectorFunc) Fetch(ctx context.Context, feed domain.Feed) (domain.FetchResult, error) {
	return f(ctx, feed)
}

func (c resultConnector) Fetch(context.Context, domain.Feed) (domain.FetchResult, error) {
	return c.result, nil
}

type countingConnector struct{ calls atomic.Int32 }

func (c *countingConnector) Fetch(context.Context, domain.Feed) (domain.FetchResult, error) {
	c.calls.Add(1)
	return domain.FetchResult{}, nil
}

type fakeItemsQueue struct{ messages []domain.ItemMessage }

func (q *fakeItemsQueue) SendMessageBatch(_ context.Context, input *sqs.SendMessageBatchInput, _ ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error) {
	for _, entry := range input.Entries {
		var message domain.ItemMessage
		if err := json.Unmarshal([]byte(*entry.MessageBody), &message); err != nil {
			return nil, err
		}
		q.messages = append(q.messages, message)
	}
	return &sqs.SendMessageBatchOutput{}, nil
}

type fakeShortsDetector struct {
	short bool
	calls int
}

func (d *fakeShortsDetector) IsShort(context.Context, string) bool {
	d.calls++
	return d.short
}

func (f failingConnector) Fetch(context.Context, domain.Feed) (domain.FetchResult, error) {
	return domain.FetchResult{}, f.err
}

func TestFetchFailureIsConsumedAfterPersistingBackoff(t *testing.T) {
	store := &fakeFeedStore{feed: domain.Feed{PK: "U#user", SK: "F#feed", FeedID: "feed", URL: "https://example.com/feed", ErrorCount: 1}}
	handler := &handler{store: store, connectors: map[string]connector.Connector{domain.ConnectorRSS: failingConnector{err: errors.New("upstream timeout")}}}
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
	handler := &handler{store: store, connectors: map[string]connector.Connector{domain.ConnectorRSS: failingConnector{err: errors.New("bad feed")}}}
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
	handler := &handler{store: store, connectors: map[string]connector.Connector{domain.ConnectorRSS: failingConnector{err: statusErr}}}

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

func TestErrorBackoffCapsAtTwentyFourHours(t *testing.T) {
	started := time.Date(2026, 8, 23, 14, 20, 0, 0, time.UTC)
	feed := domain.Feed{ErrorCount: 12, FetchIntervalH: 24}
	next, rateLimited := nextFetchAfterError(feed, started, errors.New("offline"))
	if rateLimited || next != started.Add(24*time.Hour) {
		t.Fatalf("next = %s, rate limited = %v", next, rateLimited)
	}
}

func TestMutedFeedIsConsumedWithoutFetching(t *testing.T) {
	store := &fakeFeedStore{feed: domain.Feed{PK: "U#user", FeedID: "feed", Muted: true}}
	handler := &handler{store: store, connectors: map[string]connector.Connector{domain.ConnectorRSS: failingConnector{err: errors.New("must not fetch")}}}
	if err := handler.process(context.Background(), `{"user":"user","feed_id":"feed"}`); err != nil {
		t.Fatal(err)
	}
	if len(store.putFeeds) != 0 {
		t.Fatalf("muted feed writes = %d", len(store.putFeeds))
	}
}

func TestUnknownConnectorFailsTheQueueRecord(t *testing.T) {
	store := &fakeFeedStore{feed: domain.Feed{PK: "U#user", FeedID: "feed", Connector: "missing"}}
	handler := &handler{store: store, connectors: map[string]connector.Connector{domain.ConnectorRSS: failingConnector{err: errors.New("unused")}}}
	response, err := handler.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "message", Body: `{"user":"user","feed_id":"feed"}`}}})
	if err != nil || len(response.BatchItemFailures) != 1 || response.BatchItemFailures[0].ItemIdentifier != "message" || len(store.putFeeds) != 0 {
		t.Fatalf("response = %#v, writes = %d, err = %v", response, len(store.putFeeds), err)
	}
}

func TestShortsFilteringOnlyProbesOptedInYouTubeFeeds(t *testing.T) {
	entry := domain.Entry{
		GUID: "yt:video:short", URL: "https://www.youtube.com/watch?v=short", Title: "Video", VideoID: "short",
		Published: time.Now().UTC(),
	}
	for _, test := range []struct {
		name         string
		hide, short  bool
		wantCalls    int
		wantMessages int
	}{{"opted out never probes", false, true, 0, 1}, {"opted in filters Short", true, true, 1, 0}, {"opted in keeps regular", true, false, 1, 1}} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeFeedStore{feed: domain.Feed{PK: "U#user", SK: "F#feed", FeedID: "feed", Connector: domain.ConnectorYouTube, HideShorts: test.hide}}
			queue := &fakeItemsQueue{}
			detector := &fakeShortsDetector{short: test.short}
			handler := &handler{
				store: store, connectors: map[string]connector.Connector{domain.ConnectorYouTube: resultConnector{result: domain.FetchResult{Entries: []domain.Entry{entry}}}},
				shorts: detector, queue: queue, itemsURL: "items",
			}
			if err := handler.process(context.Background(), `{"user":"user","feed_id":"feed"}`); err != nil {
				t.Fatal(err)
			}
			if detector.calls != test.wantCalls || len(queue.messages) != test.wantMessages {
				t.Fatalf("calls = %d messages = %#v", detector.calls, queue.messages)
			}
			if len(queue.messages) == 1 && (queue.messages[0].MediaType != "video" || queue.messages[0].VideoID != "short") {
				t.Fatalf("video message = %#v", queue.messages[0])
			}
		})
	}
}

func TestRegistryDispatchesRSSRedditAndYouTubeInOneBatch(t *testing.T) {
	store := &fakeFeedStore{feeds: map[string]domain.Feed{
		"rss":    {PK: "U#user", SK: "F#rss", FeedID: "rss", Connector: domain.ConnectorRSS},
		"reddit": {PK: "U#user", SK: "F#reddit", FeedID: "reddit", Connector: domain.ConnectorReddit},
		"yt":     {PK: "U#user", SK: "F#yt", FeedID: "yt", Connector: domain.ConnectorYouTube},
	}}
	rssConnector, redditConnector, youtubeConnector := &countingConnector{}, &countingConnector{}, &countingConnector{}
	handler := &handler{store: store, connectors: map[string]connector.Connector{
		domain.ConnectorRSS: rssConnector, domain.ConnectorReddit: redditConnector, domain.ConnectorYouTube: youtubeConnector,
	}}
	response, err := handler.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{
		{MessageId: "rss", Body: `{"user":"user","feed_id":"rss"}`},
		{MessageId: "reddit", Body: `{"user":"user","feed_id":"reddit"}`},
		{MessageId: "yt", Body: `{"user":"user","feed_id":"yt"}`},
	}})
	if err != nil || len(response.BatchItemFailures) != 0 || rssConnector.calls.Load() != 1 || redditConnector.calls.Load() != 1 || youtubeConnector.calls.Load() != 1 {
		t.Fatalf("response = %#v, rss calls = %d, reddit calls = %d, youtube calls = %d, err = %v", response, rssConnector.calls.Load(), redditConnector.calls.Load(), youtubeConnector.calls.Load(), err)
	}
}

func TestRedditDestinationsAndPostTypeReachItemQueue(t *testing.T) {
	entry := domain.Entry{
		GUID: "t3_one", URL: "https://reddit.com/r/example/comments/one/title/", ExternalURL: "https://example.com/story", PostType: "link",
		Title: "Story", SummaryRaw: "Clean excerpt", Published: time.Now().UTC(),
	}
	store := &fakeFeedStore{feed: domain.Feed{PK: "U#user", SK: "F#feed", FeedID: "feed", Connector: domain.ConnectorReddit}}
	queue := &fakeItemsQueue{}
	handler := &handler{store: store, connectors: map[string]connector.Connector{domain.ConnectorReddit: resultConnector{result: domain.FetchResult{Entries: []domain.Entry{entry}}}}, queue: queue, itemsURL: "items"}
	if err := handler.process(context.Background(), `{"user":"user","feed_id":"feed"}`); err != nil {
		t.Fatal(err)
	}
	if len(queue.messages) != 1 || queue.messages[0].ExternalURL != entry.ExternalURL || queue.messages[0].PostType != "link" || queue.messages[0].SummaryRaw != "Clean excerpt" {
		t.Fatalf("messages = %#v", queue.messages)
	}
}

func TestRedditFullnameDeduplicatesAcrossScheduledRuns(t *testing.T) {
	entry := domain.Entry{
		GUID: "t3_same", URL: "https://www.reddit.com/r/example/comments/same/title/",
		Title: "Same post", Published: time.Now().UTC(),
	}
	store := &fakeFeedStore{
		feed:     domain.Feed{PK: "U#user", SK: "F#feed", FeedID: "feed", Connector: domain.ConnectorReddit},
		existing: map[string]bool{},
	}
	queue := &fakeItemsQueue{}
	handler := &handler{
		store: store,
		connectors: map[string]connector.Connector{
			domain.ConnectorReddit: resultConnector{result: domain.FetchResult{Entries: []domain.Entry{entry}}},
		},
		queue: queue, itemsURL: "items",
	}
	message := `{"user":"user","feed_id":"feed"}`
	if err := handler.process(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(queue.messages) != 1 {
		t.Fatalf("first run messages = %#v", queue.messages)
	}
	store.existing[domain.ItemID("feed", "t3_same", entry.URL)] = true
	if err := handler.process(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(queue.messages) != 1 {
		t.Fatalf("duplicate was queued: %#v", queue.messages)
	}
}

func TestRedditPersistentForbiddenDoesNotAffectHealthySibling(t *testing.T) {
	store := &fakeFeedStore{feeds: map[string]domain.Feed{
		"blocked": {PK: "U#user", SK: "F#blocked", FeedID: "blocked", Connector: domain.ConnectorReddit, URL: "https://www.reddit.com/r/blocked/.rss"},
		"healthy": {PK: "U#user", SK: "F#healthy", FeedID: "healthy", Connector: domain.ConnectorReddit, URL: "https://www.reddit.com/r/healthy/.rss"},
	}}
	var healthyCalls atomic.Int32
	implementation := connectorFunc(func(_ context.Context, feed domain.Feed) (domain.FetchResult, error) {
		if feed.FeedID == "blocked" {
			return domain.FetchResult{}, &connector.HTTPStatusError{StatusCode: http.StatusForbidden}
		}
		healthyCalls.Add(1)
		return domain.FetchResult{}, nil
	})
	handler := &handler{store: store, connectors: map[string]connector.Connector{domain.ConnectorReddit: implementation}}
	event := events.SQSEvent{Records: []events.SQSMessage{
		{MessageId: "blocked", Body: `{"user":"user","feed_id":"blocked"}`},
		{MessageId: "healthy", Body: `{"user":"user","feed_id":"healthy"}`},
	}}
	for run := 0; run < 3; run++ {
		response, err := handler.run(context.Background(), event)
		if err != nil || len(response.BatchItemFailures) != 0 {
			t.Fatalf("run %d = %#v, %v", run+1, response, err)
		}
	}
	blocked := store.feeds["blocked"]
	healthy := store.feeds["healthy"]
	last, lastErr := time.Parse(time.RFC3339Nano, blocked.LastFetchAt)
	next, nextErr := time.Parse(time.RFC3339Nano, blocked.NextFetchAt)
	if blocked.ErrorCount != 3 || blocked.LastStatus != "feed returned HTTP 403" || lastErr != nil || nextErr != nil || next.Sub(last) != 8*time.Hour {
		t.Fatalf("blocked feed = %#v, last error %v, next error %v", blocked, lastErr, nextErr)
	}
	if healthyCalls.Load() != 3 || healthy.ErrorCount != 0 || healthy.LastStatus != "200" {
		t.Fatalf("healthy feed = %#v, calls = %d", healthy, healthyCalls.Load())
	}
}

func TestPersistFeedDropsStaleRedditSortResult(t *testing.T) {
	current := domain.Feed{
		PK: "U#user", SK: "F#reddit", FeedID: "reddit", Connector: domain.ConnectorReddit,
		URL: "https://www.reddit.com/r/castles/new.rss", FetchIntervalH: 1,
	}
	store := &fakeFeedStore{feed: current}
	handler := &handler{store: store}
	fetched := current
	fetched.URL = "https://www.reddit.com/r/castles/top.rss?t=day"
	fetched.FetchIntervalH = 24
	fetched.LastStatus = "200"
	if err := handler.persistFeed(context.Background(), "user", fetched); err != nil {
		t.Fatal(err)
	}
	if len(store.putFeeds) != 0 {
		t.Fatalf("stale result wrote %#v", store.putFeeds)
	}
}
