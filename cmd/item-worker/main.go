package main

import (
	"context"
	"encoding/json"
	"errors"
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
	store          *store.Store
	http           *httpx.Client
	media          *media.Processor
	embedder       embed.Embedder
	models         *score.Cache
	modelVersion   string
	scoringVersion string
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
	var existing domain.Item
	if message.Reprocess {
		existing, err = h.store.Item(ctx, message.User, message.ItemID)
		if err != nil {
			return err
		}
		if message.Title == "" {
			message.Title = existing.Title
		}
	}
	feed, err := h.store.Feed(ctx, message.User, message.FeedID)
	if err != nil {
		if !message.Reprocess || !errors.Is(err, store.ErrNotFound) {
			return err
		}
		feed = domain.Feed{FeedID: existing.FeedID, Title: existing.FeedTitle, FaviconKey: existing.FaviconKey}
	}
	processAssets := !message.Reprocess || message.ForceExtract
	if message.Reprocess && !message.ForceExtract {
		keys := []string{}
		if existing.HasBody && existing.BodyKey != "" {
			keys = append(keys, existing.BodyKey)
		}
		if existing.MediaKey != "" {
			keys = append(keys, existing.MediaKey)
		}
		processAssets = len(keys) == 0
		for _, objectKey := range keys {
			exists, existsErr := h.store.ContentExists(ctx, objectKey)
			if existsErr != nil {
				return existsErr
			}
			if !exists {
				processAssets = true
				break
			}
		}
	}
	pageURL, _ := url.Parse(message.URL)
	var article extract.Result
	var pageHTML []byte
	if processAssets {
		if message.URL != "" {
			if response, fetchErr := h.http.Get(ctx, message.URL, nil); fetchErr == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
				pageHTML = response.Body
				if response.FinalURL != nil {
					pageURL = response.FinalURL
				}
			}
		}
		article, err = articleContent(message.ContentRaw, message.URL, feed.SiteURL, pageURL, pageHTML)
		if err != nil || article.HTML == "" {
			article = extract.Result{}
		}
	}

	summary := existing.Summary
	if processAssets {
		if refreshed := extract.Summary(message.SummaryRaw, article.FirstParagraph); refreshed != "" {
			summary = refreshed
		}
	}
	mediaKey, mediaW, mediaH := existing.MediaKey, existing.MediaW, existing.MediaH
	if processAssets {
		feedURL, feedErr := url.Parse(feed.SiteURL)
		if feedErr != nil || feedURL.Host == "" {
			feedURL = pageURL
		}
		feedHTML := []byte(message.ContentRaw + "\n" + message.SummaryRaw)
		candidates := media.Candidates(message.EnclosureURLs, pageHTML, []byte(article.HTML), feedHTML, article.LeadImage, pageURL, feedURL)
		mediaKey, mediaW, mediaH = "", 0, 0
		if lead, mediaErr := h.media.FetchLead(ctx, candidates); mediaErr == nil {
			mediaKey = store.MediaKey(message.User, message.ItemID, lead.Extension)
			if err := h.store.PutContent(ctx, mediaKey, lead.ContentType, lead.Bytes); err != nil {
				return fmt.Errorf("store media: %w", err)
			}
			mediaW, mediaH = lead.Width, lead.Height
			if cleaned, removed := extract.RemoveLeadImage(article.HTML, lead.SourceURL); removed {
				article.HTML = cleaned
			}
		} else {
			attributes := []any{"user", message.User, "feed_id", message.FeedID, "item_id", message.ItemID, "error", mediaErr}
			var leadErr *media.LeadError
			if errors.As(mediaErr, &leadErr) {
				attributes = append(attributes, "url", leadErr.URL, "content_type", leadErr.ContentType)
			}
			slog.Warn("media failed", attributes...)
		}
	}
	bodyKey, hasBody := existing.BodyKey, existing.HasBody
	if processAssets {
		bodyKey, hasBody = "", false
		if article.HTML != "" {
			bodyKey = store.BodyKey(message.User, message.ItemID)
			if err := h.store.PutContent(ctx, bodyKey, "text/html; charset=utf-8", []byte(article.HTML)); err != nil {
				return fmt.Errorf("store body: %w", err)
			}
			hasBody = true
		}
	}
	embedTitle := message.Title
	if embedTitle == "" {
		embedTitle = existing.Title
	}
	embedInput := capRunes(strings.TrimSpace(embedTitle+"\n"+summary+"\n"+article.FirstParagraph), 2048)
	embedStarted := time.Now()
	vector, err := h.embedder.Embed(ctx, embedInput)
	if err != nil {
		return err
	}
	vector = score.Normalize(vector)
	value, why := 0.0, (*domain.Why)(nil)
	if h.scoringVersion == "1" {
		rows, loadErr := h.store.Signals(ctx, message.User)
		if loadErr != nil {
			return loadErr
		}
		legacy := make([]score.Signal, 0, len(rows))
		for _, row := range rows {
			legacy = append(legacy, score.Signal{Value: row.Value, Vector: score.DecodeVector(row.Vector)})
		}
		value = score.LegacyCalculate(vector, legacy, mediaKey != "", published, started)
	} else {
		model, loadErr := h.models.Get(ctx, message.User)
		if loadErr != nil {
			return loadErr
		}
		result := score.Calculate(vector, model, message.FeedID, mediaKey != "", started.Sub(published).Hours())
		value = result.Score
		if result.Base > 0.6 {
			rows, signalErr := h.store.Signals(ctx, message.User)
			if signalErr != nil {
				return signalErr
			}
			liked := make([]score.Candidate, 0, len(rows))
			for _, row := range rows {
				if row.Value > 0 && score.CompatibleVersion(row.ModelVersion, h.modelVersion) {
					liked = append(liked, score.Candidate{Title: row.Title, Vector: score.DecodeVector(row.Vector)})
				}
			}
			why = score.Why(result, vector, feed.Title, liked)
		}
	}
	item := domain.Item{
		PK: domain.UserPK(message.User), SK: domain.ItemSK(published, message.ItemID), FeedPK: "F#" + message.FeedID,
		ItemID: message.ItemID, FeedID: message.FeedID, FeedTitle: feed.Title, FaviconKey: feed.FaviconKey,
		URL: message.URL, Title: embedTitle, Summary: summary, Author: message.Author,
		PublishedTS: domain.Timestamp(published), FetchedTS: domain.Timestamp(started),
		MediaKey: mediaKey, MediaW: mediaW, MediaH: mediaH, BodyKey: bodyKey, HasBody: hasBody,
		Score: value, Size: score.Size(value), Vector: score.EncodeVector(vector), ModelVersion: h.modelVersion, Why: why, TTL: published.Add(domain.Retention).Unix(),
	}
	written := false
	if message.Reprocess {
		item.PK, item.SK, item.FeedPK = existing.PK, existing.SK, existing.FeedPK
		item.URL, item.Author = existing.URL, existing.Author
		item.FetchedTS, item.TTL = existing.FetchedTS, existing.TTL
		item.ArchiveSK, item.HeartedTS = existing.ArchiveSK, existing.HeartedTS
		if err := h.store.OverwriteItem(ctx, item); err != nil {
			return err
		}
		written = true
	} else {
		written, err = h.store.PutItem(ctx, item)
		if err != nil {
			return err
		}
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

func articleContent(rawContent, itemURL, siteURL string, pageURL *url.URL, pageHTML []byte) (extract.Result, error) {
	if extract.Substantial(rawContent) {
		return extract.FeedContent(rawContent, pageURL)
	}
	if extract.IsLinkblogEntry(itemURL, siteURL, rawContent) {
		feedURL, err := url.Parse(siteURL)
		if err != nil {
			return extract.Result{}, fmt.Errorf("parse feed site URL: %w", err)
		}
		return extract.FeedContent(rawContent, feedURL)
	}
	if len(pageHTML) > 0 {
		article, err := extract.Article(pageHTML, pageURL)
		if err == nil && article.HTML != "" {
			return article, nil
		}
		if strings.TrimSpace(extract.PlainText(rawContent)) != "" {
			return extract.FeedContent(rawContent, pageURL)
		}
		return article, err
	}
	if strings.TrimSpace(extract.PlainText(rawContent)) != "" {
		return extract.FeedContent(rawContent, pageURL)
	}
	return extract.Result{}, nil
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
	modelVersion := strings.TrimSpace(os.Getenv("MODEL_VERSION"))
	if modelVersion == "" {
		modelVersion = "amazon.titan-embed-text-v2:0"
	}
	scoringVersion := strings.TrimSpace(os.Getenv("SCORING_VERSION"))
	if scoringVersion == "" {
		scoringVersion = score.VersionV2
	}
	embedder := bedrockembed.NewWithModel(bedrockruntime.NewFromConfig(config), modelVersion)
	h := &handler{store: repository, http: articleHTTP, media: processor, embedder: embedder, modelVersion: modelVersion, scoringVersion: scoringVersion}
	h.models = score.NewCache(repository, 5*time.Minute, modelVersion)
	lambda.Start(h.run)
}
