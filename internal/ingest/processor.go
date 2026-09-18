package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	redditconnector "github.com/nuntz/sema/internal/connector/reddit"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/embed"
	"github.com/nuntz/sema/internal/extract"
	"github.com/nuntz/sema/internal/httpx"
	"github.com/nuntz/sema/internal/media"
	"github.com/nuntz/sema/internal/observability"
	"github.com/nuntz/sema/internal/score"
	"github.com/nuntz/sema/internal/store"
	storycluster "github.com/nuntz/sema/internal/story"
	"github.com/nuntz/sema/internal/summarize"
	"github.com/nuntz/sema/internal/vectorstore"
)

type httpClient interface {
	Get(context.Context, string, http.Header) (httpx.Response, error)
}

// Limit concurrent image decodes and resizes to fit the Lambda memory budget.
const maxConcurrentRecords = 2

// Batches hold two records at concurrency two, so every item runs in a single
// wave. Image-heavy pages regularly need 30-60 seconds; leave the rest of the
// 120-second Lambda budget for vector writes/return.
const itemTimeout = 90 * time.Second
const batchTimeout = 110 * time.Second
const mediaFetchTimeout = 10 * time.Second

// Optional media stages get their own caps, and each is further limited to the
// time left on the item minus a reserve, so summarization, embedding, and the
// item write can still finish after slow publishers.
const leadFetchTimeout = 30 * time.Second
const embedThumbnailsTimeout = 20 * time.Second
const bodyImagesTimeout = 40 * time.Second
const completionReserve = 20 * time.Second

// optionalBudget returns how long an optional stage may run: its own cap, or
// less when the item deadline minus completionReserve is closer.
func optionalBudget(ctx context.Context, want time.Duration) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return want
	}
	remaining := time.Until(deadline) - completionReserve
	if remaining < want {
		want = remaining
	}
	return max(want, 0)
}

type vectorBatchStore interface {
	PutBatch(context.Context, []vectorstore.Record) error
	Query(context.Context, string, []float32, int, int64) ([]vectorstore.Match, error)
}

type Processor struct {
	Store             itemStore
	HTTP              httpClient
	Media             mediaProcessor
	Embedder          embed.Embedder
	ImageEmbedder     embed.ImageEmbedder
	Summarizer        summarize.Summarizer
	Models            ModelLoader
	Extraction        Extractor
	Scoring           Scorer
	ModelVersion      string
	ImageModelVersion string
	ScoringVersion    string
	Vectors           vectorBatchStore
	ImageVectors      vectorBatchStore
	StoryConfig       storycluster.Config
	Emit              func(map[string]float64, map[string]string)
}

type mediaProcessor interface {
	bodyImageFetcher
	FetchLead(context.Context, []string) (media.Lead, error)
	FetchVideoLead(context.Context, []string) (media.Lead, error)
	FetchEmbed(context.Context, string) (media.Image, error)
}

type processedVectors struct {
	messageID string
	text      vectorstore.Record
	image     *vectorstore.Record
}

type itemStore interface {
	Content(context.Context, string) ([]byte, string, error)
	ContentExists(context.Context, string) (bool, error)
	ContentURL(string) string
	Feed(context.Context, string, string) (domain.Feed, error)
	Item(context.Context, string, string) (domain.Item, error)
	ItemByIdentity(context.Context, string, string) (domain.Item, error)
	OverwriteItem(context.Context, domain.Item) error
	PutContent(context.Context, string, string, []byte) error
	PutItem(context.Context, domain.Item) (bool, error)
	PutItemFailure(context.Context, string, string, int64) error
	Signals(context.Context, string) ([]domain.Signal, error)
	ResolveItemIDsConsistent(context.Context, string, []string) ([]domain.Item, error)
	CreateCluster(context.Context, domain.Cluster) (bool, error)
	AddClusterMember(context.Context, string, string, string, int64) error
	SetItemCluster(context.Context, domain.Item, string) error
}

func (h *Processor) Run(ctx context.Context, event Batch) (BatchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, batchTimeout)
	defer cancel()
	failures := make(chan Failure, len(event.Records))
	processed := make(chan processedVectors, len(event.Records))
	semaphore := make(chan struct{}, maxConcurrentRecords)
	var group sync.WaitGroup
	for _, record := range event.Records {
		semaphore <- struct{}{}
		group.Add(1)
		go func(record Message) {
			defer group.Done()
			defer func() { <-semaphore }()
			vectors, err := h.processWithinDeadline(ctx, record.Body, itemTimeout)
			if err != nil {
				var message domain.ItemMessage
				_ = json.Unmarshal([]byte(record.Body), &message)
				slog.Error("item failed", "message_id", record.ID, "user", message.User, "feed_id", message.FeedID, "item_id", message.ItemID, "error", err)
				failures <- Failure{ID: record.ID}
			} else if vectors != nil {
				vectors.messageID = record.ID
				processed <- *vectors
			}
		}(record)
	}
	group.Wait()
	close(failures)
	close(processed)
	response := BatchResult{}
	for failure := range failures {
		response.Failures = append(response.Failures, failure)
	}
	vectorRecords := make([]vectorstore.Record, 0, len(processed))
	imageRecords := make([]vectorstore.Record, 0, len(processed))
	indexMessageIDs := []string{}
	indexingFailed := false
	for vectors := range processed {
		indexMessageIDs = append(indexMessageIDs, vectors.messageID)
		vectorRecords = append(vectorRecords, vectors.text)
		if vectors.image != nil {
			imageRecords = append(imageRecords, *vectors.image)
		}
	}
	if h.Vectors != nil && len(vectorRecords) > 0 {
		metric := "VectorPutSucceeded"
		if err := h.Vectors.PutBatch(ctx, vectorRecords); err != nil {
			slog.WarnContext(ctx, "vector batch write failed", "records", len(vectorRecords), "error", err)
			metric = "VectorPutFailed"
			indexingFailed = true
		}
		h.emitMetrics(map[string]float64{metric: float64(len(vectorRecords))}, nil)
	}
	if h.ImageVectors != nil && len(imageRecords) > 0 {
		metric := "ImageVectorPutSucceeded"
		if err := h.ImageVectors.PutBatch(ctx, imageRecords); err != nil {
			slog.WarnContext(ctx, "image vector batch write failed", "records", len(imageRecords), "error", err)
			metric = "ImageVectorPutFailed"
			indexingFailed = true
		}
		h.emitMetrics(map[string]float64{metric: float64(len(imageRecords))}, nil)
	}
	if indexingFailed {
		for _, id := range indexMessageIDs {
			response.Failures = append(response.Failures, Failure{ID: id})
		}
	}
	return response, nil
}

func (h *Processor) processWithinDeadline(ctx context.Context, body string, timeout time.Duration) (*processedVectors, error) {
	itemCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	vectors, err := h.process(itemCtx, body)
	// Best-effort extraction/media paths may swallow cancellation. Never report
	// such an item as successful, even if a dependency returned a nil error.
	if itemCtx.Err() != nil {
		if errors.Is(itemCtx.Err(), context.DeadlineExceeded) {
			var message domain.ItemMessage
			_ = json.Unmarshal([]byte(body), &message)
			h.emitMetrics(map[string]float64{"ItemDeadlineExceeded": 1}, map[string]string{
				"feed_id": message.FeedID, "item_id": message.ItemID, "FeedID": message.FeedID,
			})
		}
		return nil, itemCtx.Err()
	}
	return vectors, err
}

func (h *Processor) process(ctx context.Context, body string) (*processedVectors, error) {
	started := time.Now().UTC()
	var message domain.ItemMessage
	if err := json.Unmarshal([]byte(body), &message); err != nil {
		return nil, fmt.Errorf("decode message: %w", err)
	}
	published, err := time.Parse(time.RFC3339Nano, message.PublishedTS)
	if err != nil {
		return nil, fmt.Errorf("published_ts: %w", err)
	}
	if domain.LiveWindowTTL(published) <= started.Unix() {
		return nil, nil
	}
	// Redelivery repairs indexing directly from durable vectors, without fetching
	// content or paying for another embedding.
	if !message.Reprocess {
		stored, loadErr := h.Store.ItemByIdentity(ctx, message.User, message.ItemID)
		if loadErr == nil {
			return recordsForItem(stored), nil
		}
		if !errors.Is(loadErr, store.ErrNotFound) {
			return nil, loadErr
		}
	}
	var existing domain.Item
	if message.Reprocess {
		existing, err = h.Store.Item(ctx, message.User, message.ItemID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
	}
	work, err := Plan(message, existing, started, h.ModelVersion, h.ImageModelVersion)
	if err != nil {
		return nil, err
	}
	message = work.Message
	published = work.Published

	if message.Title == "" {
		if err := h.Store.PutItemFailure(ctx, message.User, message.ItemID, domain.LiveWindowTTL(published)); err != nil {
			return nil, fmt.Errorf("record permanent item failure: %w", err)
		}
		slog.WarnContext(ctx, "item permanently rejected", "user", message.User, "feed_id", message.FeedID, "item_id", message.ItemID, "reason", "missing title")
		return nil, nil
	}
	isVideo := work.Video
	feed, err := h.Store.Feed(ctx, message.User, message.FeedID)
	if err != nil {
		if !message.Reprocess && errors.Is(err, store.ErrNotFound) {
			slog.InfoContext(ctx, "feed removed before item processed", "user", message.User, "feed_id", message.FeedID, "item_id", message.ItemID)
			return nil, nil
		}
		if !message.Reprocess || !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		feed = domain.Feed{FeedID: existing.FeedID, Title: existing.FeedTitle, FaviconKey: existing.FaviconKey, Connector: existing.Connector}
	}
	feedTitle := feed.Title
	if feed.CustomTitle != "" {
		feedTitle = feed.CustomTitle
	}
	processAssets := work.Assets
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
			exists, existsErr := h.Store.ContentExists(ctx, objectKey)
			if existsErr != nil {
				return nil, existsErr
			}
			if !exists {
				processAssets = true
				break
			}
		}
	}
	contentURL, fetchURL := itemContentURLs(message)
	pageURL, _ := url.Parse(contentURL)
	var article extract.Result
	var pageHTML []byte
	if processAssets {
		if !isVideo && fetchURL != "" {
			if response, fetchErr := h.HTTP.Get(ctx, fetchURL, nil); fetchErr == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
				pageHTML = response.Body
				if response.FinalURL != nil {
					pageURL = response.FinalURL
				}
			}
		}
		if !isVideo {
			extractor := h.Extraction
			if extractor == nil {
				extractor = ArticleExtractor{}
			}
			article, err = extractor.Extract(ExtractionInput{RawContent: message.ContentRaw, ItemURL: contentURL, SiteURL: feed.SiteURL, PageURL: pageURL, PageHTML: pageHTML})
			if err != nil || article.HTML == "" {
				article = extract.Result{}
			}
		}
	}
	if !isVideo && message.ForceSummary && article.HTML == "" && existing.BodyKey != "" {
		if storedBody, _, contentErr := h.Store.Content(ctx, existing.BodyKey); contentErr == nil {
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
		summaryArticle := article
		if isVideo {
			fallbackRaw = message.ContentRaw
			descriptionText := strings.TrimSpace(extract.PlainText(message.ContentRaw))
			if descriptionText != "" {
				summaryArticle = extract.Result{Text: descriptionText, FirstParagraph: descriptionText, Quality: 1}
			}
		}
		force := feed.AlwaysGenerate
		if message.ForceSummary {
			fallbackRaw = existing.Summary
			force = forceSummaryGeneration(feed.AlwaysGenerate, existing.SummarySource)
		}
		summary, summarySource, summaryMetrics = h.chooseSummary(ctx, message.Title, fallbackRaw, summaryArticle, force)
	}
	mediaKey, mediaW, mediaH := existing.MediaKey, existing.MediaW, existing.MediaH
	mediaVariants := existing.MediaVariants
	var freshImageJPEG []byte
	embedMediaSucceeded, embedMediaFailed := 0, 0
	bodyImageSucceeded, bodyImageFailed := 0, 0
	if processAssets {
		feedURL, feedErr := url.Parse(feed.SiteURL)
		if feedErr != nil || feedURL.Host == "" {
			feedURL = pageURL
		}
		feedHTML := []byte(message.ContentRaw + "\n" + message.SummaryRaw)
		enclosures, recoveryErr := h.mediaEnclosures(ctx, message, feed)
		if recoveryErr != nil {
			slog.Warn("Reddit gallery media recovery failed", "user", message.User, "feed_id", message.FeedID, "item_id", message.ItemID, "error", recoveryErr)
		}
		candidates := media.Candidates(enclosures, pageHTML, []byte(article.HTML), feedHTML, article.LeadImage, pageURL, feedURL)
		if isVideo {
			candidates = append([]string{"https://i.ytimg.com/vi/" + url.PathEscape(message.VideoID) + "/maxresdefault.jpg"}, candidates...)
		}
		mediaKey, mediaW, mediaH, mediaVariants = "", 0, 0, nil
		var lead media.Lead
		var mediaErr error
		// Selection and the variant uploads share one budget so a slow upload
		// cannot eat into the completion reserve either.
		leadCtx, leadCancel := context.WithTimeout(ctx, optionalBudget(ctx, leadFetchTimeout))
		if isVideo {
			lead, mediaErr = h.Media.FetchVideoLead(leadCtx, candidates)
		} else {
			lead, mediaErr = h.Media.FetchLead(leadCtx, candidates)
		}
		if mediaErr == nil {
			mediaKey = store.MediaKey(message.User, message.ItemID, lead.Extension)
			mediaVariants, mediaErr = storeLead(leadCtx, h.Store, mediaKey, lead)
			if mediaErr != nil && (leadCtx.Err() == nil || ctx.Err() != nil) {
				leadCancel()
				return nil, fmt.Errorf("store media: %w", mediaErr)
			}
			if mediaErr != nil {
				// The lead budget ran out mid-upload: continue without a lead image.
				mediaKey, mediaVariants = "", nil
				mediaErr = fmt.Errorf("store media: %w", mediaErr)
			}
		}
		leadCancel()
		if mediaErr == nil {
			mediaW, mediaH = lead.Width, lead.Height
			freshImageJPEG = selectEncodedImage(lead.Variants)
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
		if !isVideo {
			embedCtx, embedCancel := context.WithTimeout(ctx, optionalBudget(ctx, embedThumbnailsTimeout))
			article.HTML, embedFailures = extract.ResolveMediaCards(article.HTML, func(card extract.MediaCard) (string, error) {
				if err := embedCtx.Err(); err != nil {
					embedMediaFailed++
					return "", err
				}
				thumbnailURL, thumbnailErr := h.embedThumbnailURL(embedCtx, card)
				if thumbnailErr != nil {
					embedMediaFailed++
					return "", thumbnailErr
				}
				thumbnail, mediaErr := h.Media.FetchEmbed(embedCtx, thumbnailURL)
				if mediaErr != nil {
					embedMediaFailed++
					return "", mediaErr
				}
				objectKey := store.EmbedMediaKey(message.User, message.ItemID, card.Index)
				if err := h.Store.PutContent(embedCtx, objectKey, thumbnail.ContentType, thumbnail.Bytes); err != nil {
					embedMediaFailed++
					return "", fmt.Errorf("store embed thumbnail: %w", err)
				}
				embedMediaSucceeded++
				return h.Store.ContentURL(objectKey), nil
			})
			embedCancel()
		}
		for _, embedErr := range embedFailures {
			slog.Warn("embed thumbnail failed", "user", message.User, "feed_id", message.FeedID, "item_id", message.ItemID, "error", embedErr)
		}
		var bodyImageFailures []error
		if !isVideo {
			article.HTML, bodyImageSucceeded, bodyImageFailed, bodyImageFailures = cacheBodyImages(ctx, h.Media, h.Store, message.User, message.ItemID, article.HTML)
		}
		for _, bodyImageErr := range bodyImageFailures {
			slog.Warn("body image failed", "user", message.User, "feed_id", message.FeedID, "item_id", message.ItemID, "error", bodyImageErr)
		}
	}
	bodyKey, hasBody := existing.BodyKey, existing.HasBody
	extractQuality := existing.ExtractQuality
	if processAssets {
		bodyKey, hasBody = "", false
		extractQuality = 0
		if !isVideo {
			extractQuality = article.Quality
		}
		if !isVideo && article.HTML != "" {
			bodyKey = store.BodyKey(message.User, message.ItemID)
			if err := h.Store.PutContent(ctx, bodyKey, "text/html; charset=utf-8", []byte(article.HTML)); err != nil {
				return nil, fmt.Errorf("store body: %w", err)
			}
			hasBody = HasBody(isVideo, article.HTML, extractQuality)
		}
	}
	imageVector, imageModelVersion := []byte(nil), ""
	if !isVideo {
		imageVector, imageModelVersion = existing.ImageVector, existing.ImageModelVersion
	}
	if processAssets && mediaKey == "" {
		imageVector, imageModelVersion = nil, ""
	}
	imageEmbedSucceeded, imageEmbedFailed := 0.0, 0.0
	imageEmbedReused := 0.0
	imageEmbedLatency := float64(0)
	imageEmbedAttempted := false
	if !isVideo && mediaKey != "" && h.ImageEmbedder != nil && h.ImageVectors != nil {
		reuseExisting := work.ReuseImage
		if reuseExisting {
			imageEmbedReused = 1
		} else {
			jpeg := freshImageJPEG
			if len(jpeg) == 0 {
				variantKey := selectedImageVariantKey(mediaVariants, mediaKey)
				stored, _, contentErr := h.Store.Content(ctx, variantKey)
				if contentErr != nil {
					imageEmbedFailed = 1
					slog.WarnContext(ctx, "image embedding failed", "user", message.User, "feed_id", message.FeedID, "item_id", message.ItemID, "error", contentErr)
				} else {
					jpeg = stored
				}
			}
			if len(jpeg) > 0 {
				imageStarted := time.Now()
				imageEmbedAttempted = true
				embedded, imageErr := h.ImageEmbedder.EmbedImage(ctx, jpeg)
				imageEmbedLatency = float64(time.Since(imageStarted).Milliseconds())
				if imageErr != nil {
					imageEmbedFailed = 1
					slog.WarnContext(ctx, "image embedding failed", "user", message.User, "feed_id", message.FeedID, "item_id", message.ItemID, "error", imageErr)
				} else {
					imageVector = score.EncodeVector(score.Normalize(embedded))
					imageModelVersion = h.ImageModelVersion
					imageEmbedSucceeded = 1
				}
			}
		}
	}
	embedTitle := message.Title
	if embedTitle == "" {
		embedTitle = existing.Title
	}
	embedInput := TextInput(embedTitle, summary, article.FirstParagraph)
	embedStarted := time.Now()
	vector := score.DecodeVector(existing.Vector)
	textEmbedded := false
	if work.Text {
		vector, err = h.Embedder.Embed(ctx, embedInput)
		if err != nil {
			return nil, err
		}
		vector = score.Normalize(vector)
		textEmbedded = true
	}
	scorer := h.Scoring
	if scorer == nil {
		scorer = Ranking{Signals: h.Store, Models: h.Models, Version: h.ScoringVersion, TextVersion: h.ModelVersion, ImageVersion: h.ImageModelVersion}
	}
	ranked, err := scorer.Score(ctx, ScoringInput{
		User: message.User, FeedID: message.FeedID, FeedTitle: feedTitle,
		Vector: vector, ImageVector: imageVector, ImageVersion: imageModelVersion,
		HasMedia: mediaKey != "", Published: published, Now: started,
	})
	if err != nil {
		return nil, err
	}
	value, why, model := ranked.Value, ranked.Why, ranked.Model

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
	item := Build(work, existing, domain.Item{
		FeedTitle: feedTitle, Connector: domain.FeedConnector(feed), FaviconKey: feed.FaviconKey,
		Title: embedTitle, Summary: summary, SummarySource: summarySource, Description: videoDescription(isVideo, message.ContentRaw, existing.Description), Author: author, DisplayDate: displayDate,
		MediaKey: mediaKey, MediaVariants: mediaVariants, MediaW: mediaW, MediaH: mediaH, MediaType: message.MediaType, VideoID: message.VideoID, IsShort: message.IsShort, BodyKey: bodyKey, HasBody: hasBody, ExtractQuality: extractQuality,
		Score: value, Vector: score.EncodeVector(vector), ModelVersion: h.ModelVersion,
		ImageVector: imageVector, ImageModelVersion: imageModelVersion, Why: why,
	}, started, h.ScoringVersion, model)
	storyMetrics := map[string]float64{}
	if message.Reprocess && existing.StoryID != "" {
		item.StoryID = existing.StoryID
	} else if h.Vectors != nil {
		if assignmentMetrics, storyErr := h.assignCluster(ctx, message.User, vector, &item); storyErr != nil {
			item.StoryID = ""
			storyMetrics["StoryAssignmentFailed"] = 1
			slog.WarnContext(ctx, "story assignment failed", "user", message.User, "feed_id", message.FeedID, "item_id", message.ItemID, "error", storyErr)
		} else {
			storyMetrics = assignmentMetrics
		}
	}
	written := false
	if message.Reprocess {
		if err := h.Store.OverwriteItem(ctx, item); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, nil
			}
			return nil, err
		}
		written = true
	} else {
		written, err = h.Store.PutItem(ctx, item)
		if err != nil {
			return nil, err
		}
	}
	slog.Info("item processed", "user", message.User, "feed_id", message.FeedID, "item_id", message.ItemID, "written", written, "has_body", hasBody, "extract_quality", extractQuality, "summary_source", summarySource, "has_media", mediaKey != "", "duration_ms", time.Since(started).Milliseconds())
	metrics := map[string]float64{"ItemWorkerDurationMs": float64(time.Since(started).Milliseconds())}
	if textEmbedded {
		metrics["BedrockLatencyMs"] = float64(time.Since(embedStarted).Milliseconds())
	}
	if imageEmbedSucceeded > 0 {
		metrics["ImageEmbedSucceeded"] = imageEmbedSucceeded
	}
	if imageEmbedFailed > 0 {
		metrics["ImageEmbedFailed"] = imageEmbedFailed
	}
	if imageEmbedReused > 0 {
		metrics["ImageEmbedReused"] = imageEmbedReused
	}
	if imageEmbedAttempted {
		metrics["ImageEmbedLatencyMs"] = imageEmbedLatency
	}
	for name, metric := range storyMetrics {
		metrics[name] = metric
	}
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
	if bodyImageSucceeded > 0 {
		metrics["BodyImageSucceeded"] = float64(bodyImageSucceeded)
	}
	if bodyImageFailed > 0 {
		metrics["BodyImageFailed"] = float64(bodyImageFailed)
	}
	var vectorRecords *processedVectors
	if written {
		metrics["ItemsWritten"] = 1
		kind := vectorstore.KindLive
		if item.ArchiveSK != "" {
			kind = vectorstore.KindArchive
		}
		record := vectorstore.FromItem(item, kind)
		vectorRecords = &processedVectors{text: record}
		if imageRecord, ok := vectorstore.ImageRecordFromItem(item, kind); ok {
			vectorRecords.image = &imageRecord
		}
	} else {
		metrics["ItemsDeduped"] = 1
		stored, loadErr := h.Store.ItemByIdentity(ctx, message.User, message.ItemID)
		if loadErr != nil && !errors.Is(loadErr, store.ErrNotFound) {
			return nil, loadErr
		}
		if loadErr == nil {
			vectorRecords = recordsForItem(stored)
		}
	}
	emitItemMetrics(metrics, message.FeedID, hasBody, mediaKey != "", h.emitMetrics)
	return vectorRecords, nil
}

func (h *Processor) emitMetrics(metrics map[string]float64, dimensions map[string]string) {
	if h.Emit != nil {
		h.Emit(metrics, dimensions)
		return
	}
	observability.Emit(metrics, dimensions)
}

func (h *Processor) assignCluster(ctx context.Context, userID string, vector []float32, item *domain.Item) (map[string]float64, error) {
	metrics := map[string]float64{}
	matches, err := h.Vectors.Query(ctx, userID, vector, 20, time.Now().Unix())
	if err != nil {
		return metrics, err
	}
	ids := make([]string, 0, len(matches))
	similarities := make(map[string]int, len(matches))
	for _, match := range matches {
		if match.Key == item.ItemID {
			continue
		}
		ids = append(ids, match.Key)
		similarities[match.Key] = match.Similarity
	}
	resolved, err := h.Store.ResolveItemIDsConsistent(ctx, userID, ids)
	if err != nil {
		return metrics, err
	}
	now := time.Now().Unix()
	candidates := make([]storycluster.Candidate, 0, len(resolved))
	for _, candidate := range resolved {
		if !domain.Live(candidate, now) {
			continue
		}
		similarity, ok := similarities[candidate.ItemID]
		if !ok || !storycluster.Qualifies(candidate, *item, similarity, h.StoryConfig) {
			continue
		}
		candidates = append(candidates, storycluster.Candidate{Item: candidate, Similarity: similarity})
	}
	metrics["StoryCandidates"] = float64(len(candidates))
	if len(candidates) == 0 {
		return metrics, nil
	}
	if clusterID, found := storycluster.Choose(candidates); found {
		if err := h.Store.AddClusterMember(ctx, userID, clusterID, item.ItemID, item.TTL); err != nil {
			return metrics, err
		}
		item.StoryID = clusterID
		metrics["StoryJoined"] = 1
		return metrics, nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Similarity != candidates[j].Similarity {
			return candidates[i].Similarity > candidates[j].Similarity
		}
		return candidates[i].Item.ItemID < candidates[j].Item.ItemID
	})
	founder := candidates[0].Item
	clusterID := founder.ItemID
	created := domain.Timestamp(time.Now())
	row := domain.Cluster{
		PK: domain.UserPK(userID), SK: domain.ClusterSK(clusterID), StoryID: clusterID,
		MemberIDs: []string{clusterID, item.ItemID}, CreatedAt: created, UpdatedAt: created, TTL: max(founder.TTL, item.TTL),
	}
	createdCluster, err := h.Store.CreateCluster(ctx, row)
	if err != nil {
		return metrics, err
	}
	if !createdCluster {
		if err := h.Store.AddClusterMember(ctx, userID, clusterID, item.ItemID, item.TTL); err != nil {
			return metrics, err
		}
	}
	if err := h.Store.SetItemCluster(ctx, founder, clusterID); err != nil {
		return metrics, err
	}
	item.StoryID = clusterID
	if createdCluster {
		metrics["StoryCreated"] = 1
	} else {
		metrics["StoryJoined"] = 1
	}
	return metrics, nil
}

func emitItemMetrics(metrics map[string]float64, feedID string, hasBody, hasMedia bool, emit func(map[string]float64, map[string]string)) {
	failures := map[string]float64{}
	if bodyImageFailed := metrics["BodyImageFailed"]; bodyImageFailed > 0 {
		failures["BodyImageFailed"] = bodyImageFailed
		delete(metrics, "BodyImageFailed")
	}
	if hasBody {
		metrics["ExtractionSucceeded"] = 1
	} else {
		failures["ExtractionFailed"] = 1
	}
	if hasMedia {
		metrics["MediaSucceeded"] = 1
	} else {
		failures["MediaFailed"] = 1
	}
	emit(metrics, nil)
	if len(failures) > 0 {
		emit(failures, map[string]string{"FeedID": feedID})
	}
}

type contentWriter interface {
	PutContent(context.Context, string, string, []byte) error
}

type bodyImageFetcher interface {
	FetchBodyImage(context.Context, string) (media.Image, error)
}

type bodyImageWriter interface {
	contentWriter
	ContentURL(string) string
}

func cacheBodyImages(ctx context.Context, fetcher bodyImageFetcher, writer bodyImageWriter, userID, itemID, raw string) (string, int, int, []error) {
	bodyImageCtx, cancel := context.WithTimeout(ctx, optionalBudget(ctx, bodyImagesTimeout))
	defer cancel()
	succeeded, failed := 0, 0
	rewritten, failures := extract.ResolveBodyImages(raw, func(image extract.BodyImage) (string, error) {
		// Once the budget is gone, skip the fetch instead of opening a doomed request.
		if err := bodyImageCtx.Err(); err != nil {
			failed++
			return "", err
		}
		fetchCtx, fetchCancel := context.WithTimeout(bodyImageCtx, mediaFetchTimeout)
		defer fetchCancel()
		fetched, err := fetcher.FetchBodyImage(fetchCtx, image.URL)
		if err != nil {
			failed++
			return "", err
		}
		key := store.BodyImageKey(userID, itemID, image.Index)
		if err := writer.PutContent(bodyImageCtx, key, fetched.ContentType, fetched.Bytes); err != nil {
			failed++
			return "", fmt.Errorf("store body image: %w", err)
		}
		succeeded++
		return writer.ContentURL(key), nil
	})
	return rewritten, succeeded, failed, failures
}

func storeLead(ctx context.Context, writer contentWriter, mediaKey string, lead media.Lead) ([]domain.MediaVariant, error) {
	variants := make([]domain.MediaVariant, 0, len(lead.Variants))
	for index, image := range lead.Variants {
		key := mediaKey
		if index != len(lead.Variants)-1 {
			key = store.MediaVariantKey(mediaKey, image.Width)
		}
		if err := writer.PutContent(ctx, key, image.ContentType, image.Bytes); err != nil {
			return nil, err
		}
		variants = append(variants, domain.MediaVariant{Key: key, Width: image.Width, Height: image.Height})
	}
	return variants, nil
}

func selectedImageVariantKey(variants []domain.MediaVariant, fallback string) string {
	if len(variants) == 0 {
		return fallback
	}
	selected := variants[0]
	withinLimit := false
	for _, variant := range variants {
		if variant.Width <= 768 && (!withinLimit || variant.Width > selected.Width) {
			selected = variant
			withinLimit = true
		} else if !withinLimit && variant.Width < selected.Width {
			selected = variant
		}
	}
	if selected.Key == "" {
		return fallback
	}
	return selected.Key
}

func selectEncodedImage(variants []media.Image) []byte {
	if len(variants) == 0 {
		return nil
	}
	selected := variants[0]
	withinLimit := false
	for _, variant := range variants {
		if variant.Width <= 768 && (!withinLimit || variant.Width > selected.Width) {
			selected = variant
			withinLimit = true
		} else if !withinLimit && variant.Width < selected.Width {
			selected = variant
		}
	}
	return selected.Bytes
}

func videoDescription(video bool, raw, existing string) string {
	if !video {
		return ""
	}
	if value := strings.TrimSpace(raw); value != "" {
		return value
	}
	return existing
}

func itemContentURLs(message domain.ItemMessage) (contentURL, fetchURL string) {
	contentURL, fetchURL = message.URL, message.URL
	if message.PostType == "" {
		return contentURL, fetchURL
	}
	// Reddit thread pages are not scraped. Link posts extract the external
	// article; text and media posts use the cleaned Atom body and enclosures.
	if message.PostType == "link" && message.ExternalURL != "" {
		return message.ExternalURL, message.ExternalURL
	}
	return contentURL, ""
}

func (h *Processor) mediaEnclosures(ctx context.Context, message domain.ItemMessage, feed domain.Feed) ([]domain.Enclosure, error) {
	if len(message.EnclosureURLs) > 0 || domain.FeedConnector(feed) != domain.ConnectorReddit || message.PostType != "gallery" {
		return message.EnclosureURLs, nil
	}
	if h.HTTP == nil {
		return nil, fmt.Errorf("HTTP client is unavailable")
	}
	entry, err := redditconnector.New(h.HTTP).FetchPost(ctx, message.URL)
	if err != nil {
		return nil, err
	}
	if entry.PostType != "gallery" {
		return nil, fmt.Errorf("recovered Reddit post type is %q", entry.PostType)
	}
	return entry.Enclosures, nil
}

func forceSummaryGeneration(alwaysGenerate bool, existingSource string) bool {
	return alwaysGenerate || existingSource == domain.SummarySourceGenerated || existingSource == domain.SummarySourceBody
}

func (h *Processor) embedThumbnailURL(ctx context.Context, card extract.MediaCard) (string, error) {
	if card.ThumbnailURL != "" {
		return card.ThumbnailURL, nil
	}
	if card.Provider != "Vimeo" || card.URL == "" {
		return "", fmt.Errorf("%s embed has no thumbnail", card.Provider)
	}
	oembedURL := "https://vimeo.com/api/oembed.json?url=" + url.QueryEscape(card.URL)
	response, err := h.HTTP.Get(ctx, oembedURL, nil)
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

func (h *Processor) chooseSummary(ctx context.Context, title, summaryRaw string, article extract.Result, force bool) (string, string, map[string]float64) {
	metrics := map[string]float64{}
	feedSummary := extract.Summary(summaryRaw, "")
	if !summarize.IsJunk(summaryRaw, title, force) {
		return feedSummary, domain.SummarySourceFeed, metrics
	}
	bodyFallback := extract.Summary("", article.FirstParagraph)
	if bodyFallback == "" {
		bodyFallback = extract.Summary("", article.Text)
	}
	if strings.TrimSpace(article.Text) == "" {
		metrics["SummaryFallbackNoBody"] = 1
		if feedSummary != "" {
			return feedSummary, domain.SummarySourceFeed, metrics
		}
		return bodyFallback, domain.SummarySourceBody, metrics
	}
	if article.Quality < 0.3 {
		metrics["SummaryFallbackLowQuality"] = 1
		if feedSummary != "" {
			return feedSummary, domain.SummarySourceFeed, metrics
		}
		return bodyFallback, domain.SummarySourceBody, metrics
	}
	started := time.Now()
	if h.Summarizer != nil {
		if generated, err := h.Summarizer.Summarize(ctx, title, article.Text); err == nil {
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

func recordsForItem(item domain.Item) *processedVectors {
	if len(item.Vector) == 0 {
		return nil
	}
	kind := vectorstore.KindLive
	if item.ArchiveSK != "" {
		kind = vectorstore.KindArchive
	}
	records := &processedVectors{text: vectorstore.FromItem(item, kind)}
	if image, ok := vectorstore.ImageRecordFromItem(item, kind); ok {
		records.image = &image
	}
	return records
}

// Message and BatchResult keep queue transport out of the ingest pipeline.
type Message struct {
	ID   string
	Body string
}
type Batch struct{ Records []Message }
type Failure struct{ ID string }
type BatchResult struct{ Failures []Failure }
