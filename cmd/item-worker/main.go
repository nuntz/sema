package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/embed"
	bedrockembed "github.com/nuntz/sema/internal/embed/bedrock"
	"github.com/nuntz/sema/internal/extract"
	"github.com/nuntz/sema/internal/httpx"
	"github.com/nuntz/sema/internal/media"
	"github.com/nuntz/sema/internal/observability"
	"github.com/nuntz/sema/internal/score"
	"github.com/nuntz/sema/internal/store"
	"github.com/nuntz/sema/internal/summarize"
	bedrocksummary "github.com/nuntz/sema/internal/summarize/bedrock"
	"github.com/nuntz/sema/internal/vectorstore"
)

type httpClient interface {
	Get(context.Context, string, http.Header) (httpx.Response, error)
}

type handler struct {
	store          *store.Store
	http           httpClient
	media          *media.Processor
	embedder       embed.Embedder
	summarizer     summarize.Summarizer
	models         *score.Cache
	modelVersion   string
	scoringVersion string
	vectors        vectorstore.Store
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
	feedTitle := feed.Title
	if feed.CustomTitle != "" {
		feedTitle = feed.CustomTitle
	}
	processAssets := !message.Reprocess || message.ForceExtract
	if message.Reprocess && !message.ForceExtract {
		keys := []string{}
		if existing.BodyKey != "" {
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
	if message.ForceSummary && article.HTML == "" && existing.BodyKey != "" {
		if storedBody, _, contentErr := h.store.Content(ctx, existing.BodyKey); contentErr == nil {
			article = extract.Result{
				HTML: string(storedBody), Text: extract.PlainText(string(storedBody)), FirstParagraph: extract.FirstParagraph(string(storedBody)), Quality: existing.ExtractQuality,
			}
		} else {
			slog.Warn("body unavailable for forced summary", "user", message.User, "item_id", message.ItemID, "error", contentErr)
		}
	}

	summary, summarySource := existing.Summary, existing.SummarySource
	if summarySource == "" && summary != "" {
		summarySource = domain.SummarySourceFeed
	}
	summaryMetrics := map[string]float64{}
	if !message.Reprocess || message.ForceSummary {
		fallbackRaw := message.SummaryRaw
		force := feed.AlwaysGenerate
		if message.ForceSummary {
			fallbackRaw = existing.Summary
			force = forceSummaryGeneration(feed.AlwaysGenerate, existing.SummarySource)
		}
		summary, summarySource, summaryMetrics = h.chooseSummary(ctx, message.Title, fallbackRaw, article, force)
	}
	mediaKey, mediaW, mediaH := existing.MediaKey, existing.MediaW, existing.MediaH
	embedMediaSucceeded, embedMediaFailed := 0, 0
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
		var embedFailures []error
		article.HTML, embedFailures = extract.ResolveMediaCards(article.HTML, func(card extract.MediaCard) (string, error) {
			thumbnailURL, thumbnailErr := h.embedThumbnailURL(ctx, card)
			if thumbnailErr != nil {
				embedMediaFailed++
				return "", thumbnailErr
			}
			thumbnail, mediaErr := h.media.FetchEmbed(ctx, thumbnailURL)
			if mediaErr != nil {
				embedMediaFailed++
				return "", mediaErr
			}
			objectKey := store.EmbedMediaKey(message.User, message.ItemID, card.Index)
			if err := h.store.PutContent(ctx, objectKey, thumbnail.ContentType, thumbnail.Bytes); err != nil {
				embedMediaFailed++
				return "", fmt.Errorf("store embed thumbnail: %w", err)
			}
			embedMediaSucceeded++
			return h.store.ContentURL(objectKey), nil
		})
		for _, embedErr := range embedFailures {
			slog.Warn("embed thumbnail failed", "user", message.User, "feed_id", message.FeedID, "item_id", message.ItemID, "error", embedErr)
		}
	}
	bodyKey, hasBody := existing.BodyKey, existing.HasBody
	extractQuality := existing.ExtractQuality
	if processAssets {
		bodyKey, hasBody = "", false
		extractQuality = article.Quality
		if article.HTML != "" {
			bodyKey = store.BodyKey(message.User, message.ItemID)
			if err := h.store.PutContent(ctx, bodyKey, "text/html; charset=utf-8", []byte(article.HTML)); err != nil {
				return fmt.Errorf("store body: %w", err)
			}
			hasBody = extractQuality >= 0.3
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
	model := domain.Model{}
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
		loadedModel, loadErr := h.models.Get(ctx, message.User)
		if loadErr != nil {
			return loadErr
		}
		model = loadedModel
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
			why = score.Why(result, vector, feedTitle, liked)
		}
	}
	author := strings.TrimSpace(message.Author)
	if author == "" {
		author = strings.TrimSpace(article.Author)
	}
	if author == "" {
		author = existing.Author
	}
	displayDate := message.DisplayDate
	if displayDate == "" && article.DisplayDate != "" {
		displayDate = article.DisplayDate
	}
	if displayDate == "" {
		displayDate = existing.DisplayDate
	}
	item := domain.Item{
		PK: domain.UserPK(message.User), SK: domain.ItemSK(published, message.ItemID), FeedPK: "F#" + message.FeedID,
		ItemID: message.ItemID, FeedID: message.FeedID, FeedTitle: feedTitle, FaviconKey: feed.FaviconKey,
		URL: message.URL, Title: embedTitle, Summary: summary, SummarySource: summarySource, Author: author, DisplayDate: displayDate,
		SearchText:  domain.DeriveSearchText(embedTitle, summary),
		PublishedTS: domain.Timestamp(published), FetchedTS: domain.Timestamp(started),
		MediaKey: mediaKey, MediaW: mediaW, MediaH: mediaH, BodyKey: bodyKey, HasBody: hasBody, ExtractQuality: extractQuality,
		Score: value, Size: ingestSize(value, h.scoringVersion, model), Vector: score.EncodeVector(vector), ModelVersion: h.modelVersion, Why: why, TTL: published.Add(domain.Retention).Unix(),
	}
	written := false
	if message.Reprocess {
		item.PK, item.SK, item.FeedPK = existing.PK, existing.SK, existing.FeedPK
		item.URL = existing.URL
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
	slog.Info("item processed", "user", message.User, "feed_id", message.FeedID, "item_id", message.ItemID, "written", written, "has_body", hasBody, "extract_quality", extractQuality, "summary_source", summarySource, "has_media", mediaKey != "", "duration_ms", time.Since(started).Milliseconds())
	metrics := map[string]float64{"ItemWorkerDurationMs": float64(time.Since(started).Milliseconds()), "BedrockLatencyMs": float64(time.Since(embedStarted).Milliseconds())}
	for name, metric := range summaryMetrics {
		metrics[name] = metric
	}
	metrics["ExtractionQuality"] = extractQuality
	if embedMediaSucceeded > 0 {
		metrics["EmbedMediaSucceeded"] = float64(embedMediaSucceeded)
	}
	if embedMediaFailed > 0 {
		metrics["EmbedMediaFailed"] = float64(embedMediaFailed)
	}
	if written {
		metrics["ItemsWritten"] = 1
		kind := vectorstore.KindLive
		if item.ArchiveSK != "" {
			kind = vectorstore.KindArchive
		}
		if h.vectors != nil {
			if err := h.vectors.Put(ctx, vectorstore.FromItem(item, kind)); err != nil {
				slog.WarnContext(ctx, "vector write failed", "user", message.User, "item_id", item.ItemID, "error", err)
				metrics["VectorPutFailed"] = 1
			} else {
				metrics["VectorPutSucceeded"] = 1
			}
		}
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

func forceSummaryGeneration(alwaysGenerate bool, existingSource string) bool {
	return alwaysGenerate || existingSource == domain.SummarySourceGenerated || existingSource == domain.SummarySourceBody
}

func (h *handler) embedThumbnailURL(ctx context.Context, card extract.MediaCard) (string, error) {
	if card.ThumbnailURL != "" {
		return card.ThumbnailURL, nil
	}
	if card.Provider != "Vimeo" || card.URL == "" {
		return "", fmt.Errorf("%s embed has no thumbnail", card.Provider)
	}
	oembedURL := "https://vimeo.com/api/oembed.json?url=" + url.QueryEscape(card.URL)
	response, err := h.http.Get(ctx, oembedURL, nil)
	if err != nil {
		return "", fmt.Errorf("fetch Vimeo oEmbed: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("fetch Vimeo oEmbed: HTTP status %d", response.StatusCode)
	}
	var metadata struct {
		ThumbnailURL string `json:"thumbnail_url"`
	}
	if err := json.Unmarshal(response.Body, &metadata); err != nil {
		return "", fmt.Errorf("decode Vimeo oEmbed: %w", err)
	}
	if strings.TrimSpace(metadata.ThumbnailURL) == "" {
		return "", fmt.Errorf("Vimeo oEmbed returned no thumbnail")
	}
	return metadata.ThumbnailURL, nil
}

func (h *handler) chooseSummary(ctx context.Context, title, summaryRaw string, article extract.Result, force bool) (string, string, map[string]float64) {
	metrics := map[string]float64{}
	feedSummary := extract.Summary(summaryRaw, "")
	if !summarize.IsJunk(summaryRaw, title, force) {
		return feedSummary, domain.SummarySourceFeed, metrics
	}
	bodyFallback := extract.Summary("", article.FirstParagraph)
	if bodyFallback == "" {
		bodyFallback = extract.Summary("", article.Text)
	}
	if article.Quality < 0.3 || strings.TrimSpace(article.Text) == "" {
		metrics["SummaryFallbackNoBody"] = 1
		if feedSummary != "" {
			return feedSummary, domain.SummarySourceFeed, metrics
		}
		return bodyFallback, domain.SummarySourceBody, metrics
	}
	started := time.Now()
	if h.summarizer != nil {
		if generated, err := h.summarizer.Summarize(ctx, title, article.Text); err == nil {
			metrics["SummariesGenerated"] = 1
			metrics["SummaryLatencyMs"] = float64(time.Since(started).Milliseconds())
			return generated, domain.SummarySourceGenerated, metrics
		} else {
			slog.WarnContext(ctx, "summary generation failed", "error", err)
		}
	}
	metrics["SummaryFallbackBody"] = 1
	metrics["SummaryFallbackError"] = 1
	return bodyFallback, domain.SummarySourceBody, metrics
}

func ingestSize(value float64, scoringVersion string, model domain.Model) string {
	if scoringVersion == "1" {
		return score.Size(value, domain.Model{})
	}
	return score.Size(value, model)
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
	summaryModel := strings.TrimSpace(os.Getenv("SUMMARIZE_MODEL"))
	if summaryModel == "" {
		summaryModel = bedrocksummary.ModelID
	}
	runtime := bedrockruntime.NewFromConfig(config)
	vectorBucket, vectorIndex := strings.TrimSpace(os.Getenv("VECTOR_BUCKET")), strings.TrimSpace(os.Getenv("VECTOR_INDEX"))
	if vectorBucket == "" || vectorIndex == "" {
		panic("VECTOR_BUCKET and VECTOR_INDEX are required")
	}
	embedder := bedrockembed.NewWithModel(runtime, modelVersion)
	summarizer := summarize.New(bedrocksummary.NewWithModel(runtime, summaryModel))
	h := &handler{
		store: repository, http: articleHTTP, media: processor, embedder: embedder, summarizer: summarizer,
		modelVersion: modelVersion, scoringVersion: scoringVersion,
		vectors: vectorstore.NewS3(s3vectors.NewFromConfig(config), vectorBucket, vectorIndex),
	}
	h.models = score.NewCache(repository, 5*time.Minute, modelVersion)
	lambda.Start(h.run)
}
