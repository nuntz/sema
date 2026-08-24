package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/nuntz/sema/internal/connector"
	rssconnector "github.com/nuntz/sema/internal/connector/rss"
	youtubeconnector "github.com/nuntz/sema/internal/connector/youtube"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/httpx"
	"github.com/nuntz/sema/internal/media"
	"github.com/nuntz/sema/internal/observability"
	"github.com/nuntz/sema/internal/store"
)

const (
	defaultRateLimitDelay = 15 * time.Minute
	rateLimitJitterWindow = 5 * time.Minute
)

type handler struct {
	store      feedStore
	connectors map[string]connector.Connector
	shorts     shortsDetector
	media      *media.Processor
	queue      queueAPI
	itemsURL   string
}

type queueAPI interface {
	SendMessageBatch(context.Context, *sqs.SendMessageBatchInput, ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error)
}

type shortsDetector interface {
	IsShort(context.Context, string) bool
}

type feedStore interface {
	Feed(context.Context, string, string) (domain.Feed, error)
	PutFeed(context.Context, domain.Feed) error
	ItemExists(context.Context, string, string) (bool, error)
	PutContent(context.Context, string, string, []byte) error
}

func (h *handler) run(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
	failures := make(chan events.SQSBatchItemFailure, len(event.Records))
	var group sync.WaitGroup
	for _, record := range event.Records {
		group.Add(1)
		go func(record events.SQSMessage) {
			defer group.Done()
			if err := h.process(ctx, record.Body); err != nil {
				var message domain.FeedMessage
				_ = json.Unmarshal([]byte(record.Body), &message)
				slog.Error("feed failed", "message_id", record.MessageId, "user", message.User, "feed_id", message.FeedID, "error", err)
				failures <- events.SQSBatchItemFailure{ItemIdentifier: record.MessageId}
			}
		}(record)
	}
	group.Wait()
	close(failures)
	response := events.SQSEventResponse{}
	for failure := range failures {
		response.BatchItemFailures = append(response.BatchItemFailures, failure)
	}
	return response, nil
}

func (h *handler) process(ctx context.Context, body string) error {
	var message domain.FeedMessage
	if err := json.Unmarshal([]byte(body), &message); err != nil {
		return fmt.Errorf("decode message: %w", err)
	}
	feed, err := h.store.Feed(ctx, message.User, message.FeedID)
	if err != nil {
		return err
	}
	if feed.Muted {
		slog.InfoContext(ctx, "skipping muted feed", "user", message.User, "feed_id", message.FeedID)
		return nil
	}
	implementation, ok := h.connectors[domain.FeedConnector(feed)]
	if !ok {
		return fmt.Errorf("unknown feed connector %q", domain.FeedConnector(feed))
	}
	started := time.Now().UTC()
	result, err := implementation.Fetch(ctx, feed)
	if err != nil {
		feed.ErrorCount++
		feed.LastFetchAt = domain.Timestamp(started)
		feed.LastStatus = truncate(err.Error(), 240)
		feed.LastError = truncate(err.Error(), 200)
		next, rateLimited := nextFetchAfterError(feed, started, err)
		feed.NextFetchAt = domain.Timestamp(next)
		if storeErr := h.persistFeed(ctx, message.User, feed); storeErr != nil {
			return fmt.Errorf("fetch: %v; update failure: %w", err, storeErr)
		}
		slog.WarnContext(ctx, "feed fetch failed", "user", message.User, "feed_id", message.FeedID, "error", err, "next_fetch_at", feed.NextFetchAt)
		metrics := map[string]float64{"FeedsFailed": 1}
		if rateLimited {
			metrics["FeedsRateLimited"] = 1
		}
		observability.Emit(metrics, nil)
		return nil
	}
	if result.NotModified {
		feed.LastFetchAt = domain.Timestamp(started)
		feed.LastStatus = "304"
		feed.ErrorCount = 0
		feed.LastError = ""
		feed.NextFetchAt = domain.Timestamp(domain.NextFeedFetch(feedScheduleKey(feed), started, domain.FeedIntervalHours(feed)))
		observability.Emit(map[string]float64{"FeedsNotModified": 1}, nil)
		return h.persistFeed(ctx, message.User, feed)
	}

	messages := make([]domain.ItemMessage, 0, len(result.Entries))
	for _, entry := range result.Entries {
		if entry.Published.Before(started.Add(-domain.Retention)) {
			continue
		}
		itemID := domain.ItemID(feed.FeedID, entry.GUID, entry.URL)
		exists, err := h.store.ItemExists(ctx, message.User, itemID)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if feed.HideShorts && entry.VideoID != "" && h.shorts != nil {
			entry.IsShort = h.shorts.IsShort(ctx, entry.VideoID)
			if entry.IsShort {
				continue
			}
		}
		messages = append(messages, domain.ItemMessage{
			User: message.User, FeedID: message.FeedID, ItemID: itemID, URL: entry.URL, Title: entry.Title,
			SummaryRaw: truncateBytes(entry.SummaryRaw, 20<<10), ContentRaw: truncateBytes(entry.ContentRaw, 200<<10),
			Author: entry.Author, PublishedTS: domain.Timestamp(entry.Published), DisplayDate: entry.DisplayDate, EnclosureURLs: entry.Enclosures,
			MediaType: videoMediaType(entry.VideoID), VideoID: entry.VideoID, IsShort: entry.IsShort,
		})
	}
	if err := h.enqueue(ctx, messages); err != nil {
		return err
	}
	if result.Title != "" {
		feed.Title = result.Title
	}
	if result.SiteURL != "" {
		feed.SiteURL = result.SiteURL
	}
	if result.ETag != "" {
		feed.ETag = result.ETag
	}
	if result.Modified != "" {
		feed.LastModified = result.Modified
	}
	if feed.FaviconKey == "" && h.media != nil {
		var icon media.Image
		var iconErr error
		if feed.AvatarURL != "" {
			icon, iconErr = h.media.Avatar(ctx, feed.AvatarURL)
		} else if feed.SiteURL != "" {
			icon, iconErr = h.media.Favicon(ctx, feed.SiteURL)
		} else {
			iconErr = errors.New("feed has no icon source")
		}
		if iconErr == nil {
			key := store.FaviconKey(feed.FeedID)
			if putErr := h.store.PutContent(ctx, key, icon.ContentType, icon.Bytes); putErr == nil {
				feed.FaviconKey = key
			}
		}
	}
	feed.LastFetchAt = domain.Timestamp(started)
	feed.LastStatus = "200"
	feed.ErrorCount = 0
	feed.LastError = ""
	feed.NextFetchAt = domain.Timestamp(domain.NextFeedFetch(feedScheduleKey(feed), started, domain.FeedIntervalHours(feed)))
	slog.Info("feed fetched", "user", message.User, "feed_id", message.FeedID, "items_enqueued", len(messages))
	observability.Emit(map[string]float64{"FeedsFetched": 1, "ItemsEnqueued": float64(len(messages))}, nil)
	return h.persistFeed(ctx, message.User, feed)
}

// persistFeed keeps user-managed fields from a concurrent drawer edit. It also
// prevents a fetch that was already in flight from recreating a removed feed.
func (h *handler) persistFeed(ctx context.Context, userID string, fetched domain.Feed) error {
	current, err := h.store.Feed(ctx, userID, fetched.FeedID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	fetched.CustomTitle = current.CustomTitle
	fetched.Tags = current.Tags
	fetched.Muted = current.Muted
	fetched.HideShorts = current.HideShorts
	fetched.AlwaysGenerate = current.AlwaysGenerate
	fetched.Connector = current.Connector
	fetched.AvatarURL = current.AvatarURL
	if domain.FeedIntervalHours(current) != domain.FeedIntervalHours(fetched) && fetched.ErrorCount == 0 {
		if lastFetch, parseErr := time.Parse(time.RFC3339Nano, fetched.LastFetchAt); parseErr == nil {
			fetched.NextFetchAt = domain.Timestamp(domain.NextFeedFetch(feedScheduleKey(fetched), lastFetch, domain.FeedIntervalHours(current)))
		}
	}
	fetched.FetchIntervalH = current.FetchIntervalH
	return h.store.PutFeed(ctx, fetched)
}

func videoMediaType(videoID string) string {
	if videoID != "" {
		return "video"
	}
	return ""
}

func feedScheduleKey(feed domain.Feed) string {
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
	next = next.Add(domain.StableOffset(feedScheduleKey(feed), rateLimitJitterWindow))
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

func (h *handler) enqueue(ctx context.Context, messages []domain.ItemMessage) error {
	for offset := 0; offset < len(messages); offset += 10 {
		end := min(offset+10, len(messages))
		entries := make([]types.SendMessageBatchRequestEntry, 0, end-offset)
		for i, message := range messages[offset:end] {
			body, err := json.Marshal(message)
			if err != nil {
				return err
			}
			if len(body) > 250<<10 {
				shrinkBy := len(body) - (249 << 10)
				message.ContentRaw = truncateBytes(message.ContentRaw, max(0, len(message.ContentRaw)-shrinkBy))
				body, err = json.Marshal(message)
				if err != nil || len(body) > 250<<10 {
					return fmt.Errorf("item %s exceeds the SQS message limit", message.ItemID)
				}
			}
			entries = append(entries, types.SendMessageBatchRequestEntry{Id: aws.String(fmt.Sprintf("item-%d", offset+i)), MessageBody: aws.String(string(body))})
		}
		response, err := h.queue.SendMessageBatch(ctx, &sqs.SendMessageBatchInput{QueueUrl: aws.String(h.itemsURL), Entries: entries})
		if err != nil {
			return err
		}
		if len(response.Failed) > 0 {
			return fmt.Errorf("failed to enqueue %d items: %s", len(response.Failed), aws.ToString(response.Failed[0].Message))
		}
	}
	return nil
}

func truncate(value string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes])
}

func truncateBytes(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	repository, config, err := store.FromEnv(context.Background())
	if err != nil {
		panic(err)
	}
	itemsURL := os.Getenv("ITEMS_QUEUE_URL")
	if itemsURL == "" {
		panic("ITEMS_QUEUE_URL is required")
	}
	feedHTTP := httpx.New(15*time.Second, 5<<20)
	mediaHTTP := httpx.New(15*time.Second, 10<<20)
	h := &handler{
		store: repository,
		connectors: map[string]connector.Connector{
			domain.ConnectorRSS: rssconnector.New(feedHTTP), domain.ConnectorYouTube: youtubeconnector.New(feedHTTP),
		},
		shorts: youtubeconnector.NewShortsDetector(feedHTTP), media: media.New(mediaHTTP),
		queue: sqs.NewFromConfig(config), itemsURL: itemsURL,
	}
	lambda.Start(h.run)
}
