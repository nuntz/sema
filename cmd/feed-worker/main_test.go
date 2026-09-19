package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nuntz/sema/internal/feedstatus"
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
	wantDelay := 2 * time.Minute
	if lastErr != nil || nextErr != nil || next.Sub(last) != wantDelay || feed.ErrorCount != 3 || feed.RefusedSince == "" || feed.LastStatus != "feed returned HTTP 429" {
		t.Fatalf("rate-limited feed = %#v, last error %v, next error %v, want delay %v", feed, lastErr, nextErr, wantDelay)
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

func TestTerminalFailureMarkerSuppressesReenqueue(t *testing.T) {
	entry := domain.Entry{GUID: "post", URL: "https://example.com/post", Published: time.Now().UTC()}
	itemID := domain.ItemID("feed", entry.GUID, entry.URL)
	store := &fakeFeedStore{
		feed: domain.Feed{PK: "U#user", SK: "F#feed", FeedID: "feed"}, existing: map[string]bool{itemID: true},
	}
	queue := &fakeItemsQueue{}
	handler := &handler{
		store: store, connectors: map[string]connector.Connector{domain.ConnectorRSS: resultConnector{result: domain.FetchResult{Entries: []domain.Entry{entry}}}},
		queue: queue, itemsURL: "items",
	}
	if err := handler.process(context.Background(), `{"user":"user","feed_id":"feed"}`); err != nil {
		t.Fatal(err)
	}
	if len(queue.messages) != 0 {
		t.Fatalf("terminal item was re-enqueued: %#v", queue.messages)
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

func TestRefusalsPersistAndRecover(t *testing.T) {
	for _, test := range []struct {
		connector       string
		status, cadence int
		since           string
		wantStatus      string
	}{
		{domain.ConnectorYouTube, 404, 1, "", "ok"}, {domain.ConnectorYouTube, 500, 24, "", "ok"},
		{domain.ConnectorRSS, 429, 3, domain.Timestamp(time.Now().Add(-2 * time.Hour)), "ok"},
		{domain.ConnectorYouTube, 404, 24, domain.Timestamp(time.Now().Add(-25 * time.Hour)), "broken"},
	} {
		t.Run(fmt.Sprintf("%s-%d-%s", test.connector, test.status, test.wantStatus), func(t *testing.T) {
			repository := &fakeFeedStore{feed: domain.Feed{PK: "U#user", SK: "F#feed", FeedID: "feed", URL: "https://example.com/feed", Connector: test.connector, FetchIntervalH: test.cadence, RefusedSince: test.since}}
			h := &handler{store: repository, connectors: map[string]connector.Connector{test.connector: failingConnector{err: &connector.HTTPStatusError{StatusCode: test.status}}}}
			if err := h.process(context.Background(), `{"user":"user","feed_id":"feed"}`); err != nil {
				t.Fatal(err)
			}
			got := repository.putFeeds[0]
			if got.ErrorCount != 0 || got.RefusedSince == "" || (test.since != "" && got.RefusedSince != test.since) || feedstatus.Status(got) != test.wantStatus {
				t.Fatalf("refused feed=%+v", got)
			}
			last, _ := time.Parse(time.RFC3339Nano, got.LastFetchAt)
			next, _ := time.Parse(time.RFC3339Nano, got.NextFetchAt)
			if next.Sub(last) != time.Duration(min(test.cadence, 6))*time.Hour+domain.StableOffset(feedstatus.ScheduleKey(got), 5*time.Minute) {
				t.Fatalf("retry=%s", next.Sub(last))
			}
			repository.feed = got
			if err := h.process(context.Background(), `{"user":"user","feed_id":"feed"}`); err != nil {
				t.Fatal(err)
			}
			if repository.putFeeds[1].RefusedSince != got.RefusedSince {
				t.Fatal("second refusal replaced first timestamp")
			}
			h.connectors[test.connector] = resultConnector{result: domain.FetchResult{NotModified: true}}
			if err := h.process(context.Background(), `{"user":"user","feed_id":"feed"}`); err != nil {
				t.Fatal(err)
			}
			if recovered := repository.putFeeds[2]; recovered.RefusedSince != "" || feedstatus.Status(recovered) != "ok" {
				t.Fatalf("304=%+v", recovered)
			}
		})
	}
}

func TestSuccessfulFetchLearnsCadenceFromEnqueuedItems(t *testing.T) {
	now := time.Now().UTC()
	for _, test := range []struct {
		name      string
		pin       int
		connector string
		age       time.Duration
		want      int
	}{
		{"auto", 0, domain.ConnectorRSS, 8 * 24 * time.Hour, 3}, {"young", 0, domain.ConnectorRSS, time.Hour, 1},
		{"pinned", 24, domain.ConnectorRSS, 8 * 24 * time.Hour, 24}, {"reddit", 6, domain.ConnectorReddit, 8 * 24 * time.Hour, 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			feed := domain.Feed{PK: "U#user", SK: "F#feed", FeedID: "feed", URL: "https://example.com/feed", Connector: test.connector, FetchIntervalH: test.pin, HistoryStartedAt: domain.Timestamp(now.Add(-test.age)), PublishHistory: map[string]int{now.Format("2006-01-02"): 9}, EffectiveCadenceH: 24}
			repository := &fakeFeedStore{feed: feed, existing: map[string]bool{domain.ItemID("feed", "old", "https://example.com/old"): true}}
			queue := &fakeItemsQueue{}
			h := &handler{store: repository, queue: queue, connectors: map[string]connector.Connector{test.connector: resultConnector{result: domain.FetchResult{Entries: []domain.Entry{{GUID: "new", URL: "https://example.com/new", Published: now, LinkItem: true}, {GUID: "old", URL: "https://example.com/old", Published: now}}}}}}
			if err := h.process(context.Background(), `{"user":"user","feed_id":"feed"}`); err != nil {
				t.Fatal(err)
			}
			got := repository.putFeeds[0]
			if got.PublishHistory[now.Format("2006-01-02")] != 10 || domain.FeedIntervalHours(got) != test.want || len(queue.messages) != 1 || !queue.messages[0].LinkItem {
				t.Fatalf("feed=%+v messages=%+v", got, queue.messages)
			}
			last, _ := time.Parse(time.RFC3339Nano, got.LastFetchAt)
			if got.NextFetchAt != domain.Timestamp(domain.NextFeedFetch(feedstatus.ScheduleKey(got), last, test.want)) {
				t.Fatalf("next fetch=%s", got.NextFetchAt)
			}
		})
	}
}
