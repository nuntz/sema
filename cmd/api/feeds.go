package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-lambda-go/events"
	"github.com/nuntz/sema/internal/connector/rss"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
	"github.com/nuntz/sema/internal/store"
)

const feedCacheTTL = time.Minute

var errInvalidFeedTag = errors.New("invalid feed tag")

type feedDiscoverer interface {
	Discover(context.Context, string) ([]rss.Candidate, error)
}

type cachedFeedList struct {
	loaded time.Time
	feeds  []domain.Feed
}

func (s *server) feedRoute(ctx context.Context, userID, method, suffix, body string) events.APIGatewayV2HTTPResponse {
	if strings.HasSuffix(suffix, "/retry") {
		if method != http.MethodPost {
			return response(http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return s.retryFeed(ctx, userID, strings.TrimSuffix(suffix, "/retry"))
	}
	if suffix == "" || strings.Contains(suffix, "/") {
		return response(http.StatusNotFound, map[string]string{"error": "feed not found"})
	}
	switch method {
	case http.MethodPatch:
		return s.patchFeed(ctx, userID, suffix, body)
	case http.MethodDelete:
		return s.deleteFeed(ctx, userID, suffix)
	default:
		return response(http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *server) discoverFeeds(ctx context.Context, body string) events.APIGatewayV2HTTPResponse {
	var input struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(body, &input); err != nil {
		return badRequest(err)
	}
	if s.discover == nil {
		return s.failure("discover feeds", errors.New("feed discovery is not configured"))
	}
	discoveryContext, cancel := context.WithTimeout(ctx, 14*time.Second)
	defer cancel()
	candidates, err := s.discover.Discover(discoveryContext, input.URL)
	if err != nil {
		return response(http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	}
	if candidates == nil {
		candidates = []rss.Candidate{}
	}
	return response(http.StatusOK, map[string]any{"candidates": candidates})
}

func (s *server) addFeed(ctx context.Context, userID, body string) events.APIGatewayV2HTTPResponse {
	var input struct {
		FeedURL     string   `json:"feed_url"`
		Tags        []string `json:"tags"`
		CustomTitle string   `json:"custom_title"`
	}
	if err := decodeJSON(body, &input); err != nil {
		return badRequest(err)
	}
	feedURL, err := normalizeFeedURL(input.FeedURL)
	if err != nil {
		return badRequest(err)
	}
	tags, err := normalizeTags(input.Tags)
	if err != nil {
		return badRequest(err)
	}
	customTitle, err := normalizeTitle(input.CustomTitle)
	if err != nil {
		return badRequest(err)
	}
	now := time.Now().UTC()
	feedID := domain.FeedID(feedURL)
	feed := domain.Feed{
		PK: domain.UserPK(userID), SK: domain.FeedSK(feedID), FeedID: feedID, URL: feedURL,
		CustomTitle: customTitle, Tags: tags, FetchIntervalH: 1,
		NextFetchAt: domain.Timestamp(now), LastStatus: "queued",
	}
	created := true
	if existing, getErr := s.store.Feed(ctx, userID, feedID); getErr == nil {
		created = false
		feed = existing
		merged, mergeErr := mergeFeedTags(feed.Tags, tags)
		if mergeErr != nil {
			return badRequest(mergeErr)
		}
		feed.Tags = merged
		if customTitle != "" {
			feed.CustomTitle = customTitle
		}
		if !feed.Muted {
			feed.NextFetchAt = domain.Timestamp(now)
			feed.LastStatus = "queued"
		}
	} else if !errors.Is(getErr, store.ErrNotFound) {
		return s.failure("find feed", getErr)
	}
	if err := s.store.PutFeed(ctx, feed); err != nil {
		return s.failure("store feed", err)
	}
	if !feed.Muted {
		if err := s.enqueueFeeds(ctx, []domain.FeedMessage{{User: userID, FeedID: feedID}}); err != nil {
			return s.failure("enqueue feed", err)
		}
	}
	s.invalidateFeeds(userID)
	status := http.StatusAccepted
	if !created {
		status = http.StatusOK
	}
	return response(status, map[string]any{"feed": publicFeed(s.store, feed), "created": created})
}

func (s *server) patchFeed(ctx context.Context, userID, feedID, body string) events.APIGatewayV2HTTPResponse {
	var input struct {
		CustomTitle    *string   `json:"custom_title"`
		Tags           *[]string `json:"tags"`
		Muted          *bool     `json:"muted"`
		AlwaysGenerate *bool     `json:"always_generate"`
		FetchInterval  *int      `json:"fetch_interval_h"`
	}
	if err := decodeJSON(body, &input); err != nil {
		return badRequest(err)
	}
	if input.CustomTitle == nil && input.Tags == nil && input.Muted == nil && input.AlwaysGenerate == nil && input.FetchInterval == nil {
		return badRequest(errors.New("at least one feed field is required"))
	}
	feed, err := s.store.Feed(ctx, userID, feedID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return response(http.StatusNotFound, map[string]string{"error": "feed not found"})
		}
		return s.failure("get feed", err)
	}
	wasMuted := feed.Muted
	if input.CustomTitle != nil {
		value, titleErr := normalizeTitle(*input.CustomTitle)
		if titleErr != nil {
			return badRequest(titleErr)
		}
		feed.CustomTitle = value
	}
	if input.Tags != nil {
		value, tagsErr := normalizeTags(*input.Tags)
		if tagsErr != nil {
			return badRequest(tagsErr)
		}
		feed.Tags = value
	}
	if input.FetchInterval != nil {
		if *input.FetchInterval != 1 && *input.FetchInterval != 6 && *input.FetchInterval != 24 {
			return badRequest(errors.New("fetch_interval_h must be 1, 6, or 24"))
		}
		feed.FetchIntervalH = *input.FetchInterval
	}
	if input.Muted != nil {
		feed.Muted = *input.Muted
	}
	if input.AlwaysGenerate != nil {
		feed.AlwaysGenerate = *input.AlwaysGenerate
	}
	unmuted := wasMuted && !feed.Muted
	if unmuted {
		feed.NextFetchAt = domain.Timestamp(time.Now().UTC())
		feed.LastStatus = "queued"
	}
	if err := s.store.PutFeed(ctx, feed); err != nil {
		return s.failure("update feed", err)
	}
	if unmuted {
		if err := s.enqueueFeeds(ctx, []domain.FeedMessage{{User: userID, FeedID: feedID}}); err != nil {
			return s.failure("enqueue unmuted feed", err)
		}
	}
	s.invalidateFeeds(userID)
	feed.Status = feedStatus(feed)
	return response(http.StatusOK, publicFeed(s.store, feed))
}

func (s *server) retryFeed(ctx context.Context, userID, feedID string) events.APIGatewayV2HTTPResponse {
	if feedID == "" {
		return badRequest(errors.New("feed id is required"))
	}
	feed, err := s.store.Feed(ctx, userID, feedID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return response(http.StatusNotFound, map[string]string{"error": "feed not found"})
		}
		return s.failure("get feed", err)
	}
	if feed.Muted {
		return badRequest(errors.New("unmute the feed before retrying"))
	}
	feed.ErrorCount = 0
	feed.LastStatus = "queued"
	feed.NextFetchAt = domain.Timestamp(time.Now().UTC())
	if err := s.store.PutFeed(ctx, feed); err != nil {
		return s.failure("reset feed", err)
	}
	if err := s.enqueueFeeds(ctx, []domain.FeedMessage{{User: userID, FeedID: feedID}}); err != nil {
		return s.failure("enqueue retry", err)
	}
	s.invalidateFeeds(userID)
	feed.Status = feedStatus(feed)
	return response(http.StatusAccepted, publicFeed(s.store, feed))
}

func (s *server) exportFeeds(ctx context.Context, userID string) events.APIGatewayV2HTTPResponse {
	feeds, err := s.store.Feeds(ctx, userID)
	if err != nil {
		return s.failure("list feeds for export", err)
	}
	sort.Slice(feeds, func(i, j int) bool { return feeds[i].URL < feeds[j].URL })
	subscriptions := make([]rss.Subscription, 0, len(feeds))
	for _, feed := range feeds {
		title := feed.CustomTitle
		if title == "" {
			title = feed.Title
		}
		subscriptions = append(subscriptions, rss.Subscription{
			Title: title, URL: feed.URL, Tags: feed.Tags, Muted: feed.Muted, IntervalH: domain.FeedIntervalHours(feed),
		})
	}
	encoded, err := rss.ExportOPML(subscriptions)
	if err != nil {
		return s.failure("export OPML", err)
	}
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK, Body: string(encoded), Headers: map[string]string{
			"content-type": "text/x-opml; charset=utf-8", "content-disposition": `attachment; filename="sema-feeds.opml"`, "cache-control": "no-store",
		},
	}
}

func (s *server) allowedFeedIDs(ctx context.Context, userID, rawTag string) (map[string]bool, error) {
	tag := strings.ToLower(strings.TrimSpace(rawTag))
	if utf8.RuneCountInString(tag) > 32 {
		return nil, fmt.Errorf("%w: tag must be at most 32 characters", errInvalidFeedTag)
	}
	feeds, err := s.cachedFeeds(ctx, userID)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(feeds))
	for _, feed := range feeds {
		if feed.Muted {
			continue
		}
		if tag == "__untagged" {
			if len(feed.Tags) == 0 {
				allowed[feed.FeedID] = true
			}
			continue
		}
		if tag == "" || hasTag(feed.Tags, tag) {
			allowed[feed.FeedID] = true
		}
	}
	return allowed, nil
}

func (s *server) applyFeedPresentation(ctx context.Context, userID string, items []domain.Item) error {
	feeds, err := s.cachedFeeds(ctx, userID)
	if err != nil {
		return err
	}
	titles := make(map[string]string, len(feeds))
	for _, feed := range feeds {
		if feed.CustomTitle != "" {
			titles[feed.FeedID] = feed.CustomTitle
		} else if feed.Title != "" {
			titles[feed.FeedID] = feed.Title
		}
	}
	for i := range items {
		if title := titles[items[i].FeedID]; title != "" {
			items[i].FeedTitle = title
		}
	}
	return nil
}

func (s *server) cachedFeeds(ctx context.Context, userID string) ([]domain.Feed, error) {
	now := time.Now()
	s.feedMu.Lock()
	entry, ok := s.feedCache[userID]
	s.feedMu.Unlock()
	if ok && now.Sub(entry.loaded) < feedCacheTTL {
		return entry.feeds, nil
	}
	feeds, err := s.store.Feeds(ctx, userID)
	if err != nil {
		return nil, err
	}
	s.feedMu.Lock()
	if s.feedCache == nil {
		s.feedCache = make(map[string]cachedFeedList)
	}
	s.feedCache[userID] = cachedFeedList{loaded: now, feeds: feeds}
	s.feedMu.Unlock()
	return feeds, nil
}

func (s *server) invalidateFeeds(userID string) {
	s.feedMu.Lock()
	delete(s.feedCache, userID)
	delete(s.feedDetailCache, userID)
	s.feedMu.Unlock()
}

func (s *server) cachedDetailedFeeds(ctx context.Context, userID string) ([]domain.Feed, error) {
	now := time.Now()
	s.feedMu.Lock()
	entry, ok := s.feedDetailCache[userID]
	s.feedMu.Unlock()
	if ok && now.Sub(entry.loaded) < feedCacheTTL {
		return append([]domain.Feed(nil), entry.feeds...), nil
	}
	feeds, err := s.store.Feeds(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := s.decorateFeeds(ctx, userID, feeds); err != nil {
		return nil, err
	}
	s.feedMu.Lock()
	if s.feedDetailCache == nil {
		s.feedDetailCache = make(map[string]cachedFeedList)
	}
	s.feedDetailCache[userID] = cachedFeedList{loaded: now, feeds: feeds}
	s.feedMu.Unlock()
	return append([]domain.Feed(nil), feeds...), nil
}

func normalizeFeedURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("feed_url must be an absolute HTTP URL")
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func normalizeTitle(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if utf8.RuneCountInString(value) > 200 {
		return "", errors.New("custom_title must be at most 200 characters")
	}
	return value, nil
}

func normalizeTags(raw []string) ([]string, error) {
	result := make([]string, 0, len(raw))
	seen := make(map[string]bool)
	for _, item := range raw {
		tag := strings.ToLower(strings.TrimSpace(item))
		if tag == "" {
			continue
		}
		if utf8.RuneCountInString(tag) > 32 {
			return nil, fmt.Errorf("tag %q must be at most 32 characters", tag)
		}
		if !seen[tag] {
			seen[tag] = true
			result = append(result, tag)
		}
	}
	if len(result) > 10 {
		return nil, errors.New("a feed may have at most 10 tags")
	}
	sort.Strings(result)
	return result, nil
}

func mergeFeedTags(first, second []string) ([]string, error) {
	return normalizeTags(append(append([]string{}, first...), second...))
}

func hasTag(tags []string, wanted string) bool {
	for _, tag := range tags {
		if strings.EqualFold(tag, wanted) {
			return true
		}
	}
	return false
}

func feedStatus(feed domain.Feed) string {
	if feed.Muted {
		return "muted"
	}
	if feed.ErrorCount >= 3 {
		return "broken"
	}
	if feed.ErrorCount > 0 {
		return "slowed"
	}
	return "ok"
}

func publicFeed(repository *store.Store, feed domain.Feed) domain.Feed {
	feed.FaviconKey = repository.ContentURL(feed.FaviconKey)
	if feed.FetchIntervalH == 0 {
		feed.FetchIntervalH = 1
	}
	if feed.Tags == nil {
		feed.Tags = []string{}
	}
	sort.Strings(feed.Tags)
	if feed.LastError == "" && feed.ErrorCount > 0 {
		feed.LastError = feed.LastStatus
	}
	feed.Status = feedStatus(feed)
	return feed
}

func (s *server) decorateFeeds(ctx context.Context, userID string, feeds []domain.Feed) error {
	items, err := s.store.LiveItems(ctx, userID)
	if err != nil {
		return err
	}
	decorateExtraction(feeds, items)
	model, modelErr := s.store.Model(ctx, userID)
	if modelErr != nil && !errors.Is(modelErr, score.ErrModelNotFound) {
		return modelErr
	}
	for i := range feeds {
		feeds[i] = publicFeed(s.store, feeds[i])
		if modelErr == nil {
			feeds[i].Prior = model.FeedPrior[feeds[i].FeedID]
			feeds[i].PriorSignals = model.FeedSignalCount[feeds[i].FeedID]
		}
	}
	return nil
}

func decorateExtraction(feeds []domain.Feed, items []domain.Item) {
	counts := make(map[string]int)
	qualities := make(map[string][]float64)
	successes := make(map[string]int)
	bodyAttempts := make(map[string]int)
	for _, item := range items {
		counts[item.FeedID]++
		bodyAttempts[item.FeedID]++
		qualities[item.FeedID] = append(qualities[item.FeedID], item.ExtractQuality)
		if item.HasBody {
			successes[item.FeedID]++
		}
	}
	for i := range feeds {
		feeds[i].ItemCount = counts[feeds[i].FeedID]
		feeds[i].ExtractionSample = bodyAttempts[feeds[i].FeedID]
		if feeds[i].ExtractionSample >= 10 {
			values := qualities[feeds[i].FeedID]
			sort.Float64s(values)
			rate := float64(successes[feeds[i].FeedID]) / float64(feeds[i].ExtractionSample)
			median := values[len(values)/2]
			if len(values)%2 == 0 {
				median = (values[len(values)/2-1] + values[len(values)/2]) / 2
			}
			feeds[i].ExtractionRate = &rate
			feeds[i].MedianQuality = &median
		}
	}
}
