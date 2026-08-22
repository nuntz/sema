package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/embed"
	bedrockembed "github.com/nuntz/sema/internal/embed/bedrock"
	"github.com/nuntz/sema/internal/extract"
	"github.com/nuntz/sema/internal/httpx"
	"github.com/nuntz/sema/internal/media"
	"github.com/nuntz/sema/internal/observability"
	"github.com/nuntz/sema/internal/score"
	"github.com/nuntz/sema/internal/store"
)

type handler struct {
	store    *store.Store
	http     *httpx.Client
	media    *media.Processor
	embedder embed.Embedder
	signals  *score.Cache
}

func (h *handler) run(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
	failures := make(chan events.SQSBatchItemFailure, len(event.Records))
	var group sync.WaitGroup
	for _, record := range event.Records {
		group.Add(1)
		go func(record events.SQSMessage) {
			defer group.Done()
			if err := h.process(ctx, record.Body); err != nil {
				var message domain.ItemMessage
				_ = json.Unmarshal([]byte(record.Body), &message)
				slog.Error("item failed", "message_id", record.MessageId, "user", message.User, "feed_id", message.FeedID, "item_id", message.ItemID, "error", err)
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
	started := time.Now().UTC()
	var message domain.ItemMessage
	if err := json.Unmarshal([]byte(body), &message); err != nil {
		return fmt.Errorf("decode message: %w", err)
	}
	published, err := time.Parse(time.RFC3339Nano, message.PublishedTS)
	if err != nil {
		return fmt.Errorf("published_ts: %w", err)
	}
	if published.Before(started.Add(-domain.Retention)) {
		return nil
	}
	feed, err := h.store.Feed(ctx, message.User, message.FeedID)
	if err != nil {
		return err
	}
	pageURL, _ := url.Parse(message.URL)
	var article extract.Result
	var pageHTML []byte
	if message.URL != "" {
		if response, fetchErr := h.http.Get(ctx, message.URL, nil); fetchErr == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
			pageHTML = response.Body
			if response.FinalURL != nil {
				pageURL = response.FinalURL
			}
		}
	}
	if extract.Substantial(message.ContentRaw) {
		article, err = extract.FeedContent(message.ContentRaw, pageURL)
	} else if len(pageHTML) > 0 {
		article, err = extract.Article(pageHTML, pageURL)
	}
	if err != nil || article.HTML == "" {
		article = extract.Result{}
	}

	summary := extract.Summary(message.SummaryRaw, article.FirstParagraph)
	candidates := media.Candidates(message.EnclosureURLs, pageHTML, []byte(article.HTML), article.LeadImage, pageURL)
	mediaKey, mediaW, mediaH := "", 0, 0
	if lead, mediaErr := h.media.FetchLead(ctx, candidates); mediaErr == nil {
		mediaKey = store.MediaKey(message.User, message.ItemID, lead.Extension)
		if err := h.store.PutContent(ctx, mediaKey, lead.ContentType, lead.Bytes); err != nil {
			return fmt.Errorf("store media: %w", err)
		}
		mediaW, mediaH = lead.Width, lead.Height
		if cleaned, removed := extract.RemoveLeadingImage(article.HTML, lead.SourceURL); removed {
			article.HTML = cleaned
		}
	}
	bodyKey, hasBody := "", false
	if article.HTML != "" {
		bodyKey = store.BodyKey(message.User, message.ItemID)
		if err := h.store.PutContent(ctx, bodyKey, "text/html; charset=utf-8", []byte(article.HTML)); err != nil {
			return fmt.Errorf("store body: %w", err)
		}
		hasBody = true
	}
	embedInput := capRunes(strings.TrimSpace(message.Title+"\n"+summary+"\n"+article.FirstParagraph), 2048)
	embedStarted := time.Now()
	vector, err := h.embedder.Embed(ctx, embedInput)
	if err != nil {
		return err
	}
	signals, err := h.signals.Get(ctx, message.User)
	if err != nil {
		return err
	}
	value := score.Calculate(vector, signals, mediaKey != "", published, started)
	item := domain.Item{
		PK: domain.UserPK(message.User), SK: domain.ItemSK(published, message.ItemID), FeedPK: "F#" + message.FeedID,
		ItemID: message.ItemID, FeedID: message.FeedID, FeedTitle: feed.Title, FaviconKey: feed.FaviconKey,
		URL: message.URL, Title: message.Title, Summary: summary, Author: message.Author,
		PublishedTS: domain.Timestamp(published), FetchedTS: domain.Timestamp(started),
		MediaKey: mediaKey, MediaW: mediaW, MediaH: mediaH, BodyKey: bodyKey, HasBody: hasBody,
		Score: value, Size: score.Size(value), Vector: score.EncodeVector(vector), TTL: published.Add(domain.Retention).Unix(),
	}
	written, err := h.store.PutItem(ctx, item)
	if err != nil {
		return err
	}
	slog.Info("item processed", "user", message.User, "feed_id", message.FeedID, "item_id", message.ItemID, "written", written, "has_body", hasBody, "has_media", mediaKey != "", "duration_ms", time.Since(started).Milliseconds())
	metrics := map[string]float64{"ItemWorkerDurationMs": float64(time.Since(started).Milliseconds()), "BedrockLatencyMs": float64(time.Since(embedStarted).Milliseconds())}
	if written {
		metrics["ItemsWritten"] = 1
	} else {
		metrics["ItemsDeduped"] = 1
	}
	if hasBody {
		metrics["ExtractionSucceeded"] = 1
	} else {
		metrics["ExtractionFailed"] = 1
	}
	if mediaKey != "" {
		metrics["MediaSucceeded"] = 1
	} else {
		metrics["MediaFailed"] = 1
	}
	observability.Emit(metrics, nil)
	return nil
}

func capRunes(value string, count int) string {
	if utf8.RuneCountInString(value) <= count {
		return value
	}
	return string([]rune(value)[:count])
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	repository, config, err := store.FromEnv(context.Background())
	if err != nil {
		panic(err)
	}
	articleHTTP := httpx.New(15*time.Second, 5<<20)
	processor := media.New(httpx.New(15*time.Second, 10<<20))
	embedder := bedrockembed.New(bedrockruntime.NewFromConfig(config))
	h := &handler{store: repository, http: articleHTTP, media: processor, embedder: embedder}
	h.signals = score.NewCache(repository, 5*time.Minute)
	lambda.Start(h.run)
}
