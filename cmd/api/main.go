package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/nuntz/sema/internal/auth"
	"github.com/nuntz/sema/internal/connector/reddit"
	"github.com/nuntz/sema/internal/connector/rss"
	"github.com/nuntz/sema/internal/connector/youtube"
	"github.com/nuntz/sema/internal/discovery"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/embed"
	bedrockembed "github.com/nuntz/sema/internal/embed/bedrock"
	"github.com/nuntz/sema/internal/embed/titanimage"
	"github.com/nuntz/sema/internal/httpx"
	"github.com/nuntz/sema/internal/media"
	"github.com/nuntz/sema/internal/observability"
	"github.com/nuntz/sema/internal/score"
	"github.com/nuntz/sema/internal/store"
	storycluster "github.com/nuntz/sema/internal/story"
	"github.com/nuntz/sema/internal/vectorstore"
)

// DefaultImageSearchFloor must match defaultImageSearchFloor in infra/main.go.
const DefaultImageSearchFloor = 25

type queueAPI interface {
	SendMessageBatch(context.Context, *sqs.SendMessageBatchInput, ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error)
}

type server struct {
	store            *store.Store
	sessions         *auth.Sessions
	verifyGoogle     func(context.Context, string) (auth.Claims, error)
	queue            queueAPI
	feedsURL         string
	itemsURL         string
	signer           *auth.CookieSigner
	rescore          func(context.Context, string) error
	discover         feedDiscoverer
	media            *media.Processor
	feedMu           sync.Mutex
	feedCache        map[string]cachedFeedList
	feedDetailCache  map[string]cachedFeedList
	embedder         embed.Embedder
	vectors          vectorstore.Store
	imageEmbedder    embed.ImageEmbedder
	imageVectors     vectorstore.Store
	imageSearchFloor int
	emit             func(map[string]float64, map[string]string)
	storyConfig      storycluster.Config
}

type fusedMatch struct {
	Key         string
	Similarity  int
	MatchSource string
	Score       float64
	bestRank    int
}

func fuseMatches(textMatches, imageMatches []vectorstore.Match, imageFloor, limit int) []fusedMatch {
	if limit <= 0 {
		return []fusedMatch{}
	}
	byKey := make(map[string]*fusedMatch, len(textMatches)+len(imageMatches))
	add := func(matches []vectorstore.Match, source string, floor int) {
		seen := make(map[string]bool, len(matches))
		for index, match := range matches {
			if seen[match.Key] || match.Similarity < floor {
				continue
			}
			seen[match.Key] = true
			rank := index + 1
			fused := byKey[match.Key]
			if fused == nil {
				fused = &fusedMatch{Key: match.Key, MatchSource: source, bestRank: rank}
				byKey[match.Key] = fused
			} else if fused.MatchSource != source {
				fused.MatchSource = "both"
			}
			fused.Score += 1 / float64(60+rank)
			fused.Similarity = max(fused.Similarity, match.Similarity)
			fused.bestRank = min(fused.bestRank, rank)
		}
	}
	add(textMatches, "text", 0)
	add(imageMatches, "image", imageFloor)
	result := make([]fusedMatch, 0, len(byKey))
	for _, match := range byKey {
		result = append(result, *match)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		if result[i].bestRank != result[j].bestRank {
			return result[i].bestRank < result[j].bestRank
		}
		return result[i].Key < result[j].Key
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

type unsupportedFeed struct {
	Title  string `json:"title,omitempty"`
	URL    string `json:"url"`
	Reason string `json:"reason"`
}

type importFeedsResult struct {
	Imported    int               `json:"imported"`
	Unsupported []unsupportedFeed `json:"unsupported"`
}

func (s *server) handle(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	started := time.Now()
	result, err := s.handleRequest(ctx, request)
	status := result.StatusCode
	if err != nil || status == 0 {
		status = http.StatusInternalServerError
	}
	metrics := map[string]float64{"APIRequests": 1, "APIRequestDurationMs": float64(time.Since(started).Milliseconds())}
	if status >= http.StatusInternalServerError {
		metrics["APIServerErrors"] = 1
	}
	emit := s.emit
	if emit == nil {
		emit = observability.Emit
	}
	emit(metrics, map[string]string{
		"Route":  apiRouteTemplate(request.RequestContext.HTTP.Method, apiPath(request.RawPath)),
		"Status": strconv.Itoa(status),
	})
	return result, err
}

func (s *server) handleRequest(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	path := apiPath(request.RawPath)
	method := request.RequestContext.HTTP.Method
	if crossSiteMutation(method, request.Headers) {
		return response(http.StatusForbidden, map[string]string{"error": "forbidden"}), nil
	}
	if method == http.MethodPost && path == "/session" {
		return s.postSession(ctx, request.Body), nil
	}

	claims, renewedCookie, err := auth.FromRequest(ctx, request, s.sessions)
	if err != nil {
		return response(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}), nil
	}
	if method == http.MethodDelete && path == "/session" {
		if err := s.sessions.DeleteRequest(ctx, request); err != nil {
			return s.failure("delete session", err), nil
		}
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusNoContent, Headers: map[string]string{"cache-control": "no-store"}, Cookies: []string{auth.ClearSessionCookie()},
		}, nil
	}
	var result events.APIGatewayV2HTTPResponse
	switch {
	case method == http.MethodGet && path == "/me":
		result = s.getMe(ctx, claims.Subject)
	case method == http.MethodPatch && path == "/me":
		result = s.patchMe(ctx, claims.Subject, request.Body)
	case method == http.MethodGet && path == "/items":
		result = s.getItems(ctx, claims.Subject, request.QueryStringParameters)
	case method == http.MethodGet && path == "/stories":
		result = s.getStories(ctx, claims.Subject, request.QueryStringParameters)
	case method == http.MethodGet && path == "/search":
		result = s.getSearch(ctx, claims.Subject, request.QueryStringParameters)
	case method == http.MethodPost && path == "/items/read-batch":
		result = s.readBatch(ctx, claims.Subject, request.Body)
	case method == http.MethodPost && path == "/ranking/recompute":
		result = s.recomputeRanking(ctx, claims.Subject)
	case method == http.MethodGet && path == "/archive":
		result = s.getArchive(ctx, claims.Subject, request.QueryStringParameters)
	case method == http.MethodGet && strings.HasPrefix(path, "/archive/"):
		result = s.getArchiveItem(ctx, claims.Subject, strings.TrimPrefix(path, "/archive/"))
	case method == http.MethodGet && strings.HasPrefix(path, "/items/") && strings.HasSuffix(path, "/similar"):
		itemID := strings.TrimSuffix(strings.TrimPrefix(path, "/items/"), "/similar")
		result = s.getSimilar(ctx, claims.Subject, itemID, request.QueryStringParameters)
	case strings.HasPrefix(path, "/items/"):
		result = s.itemRoute(ctx, claims.Subject, method, strings.TrimPrefix(path, "/items/"), request.Body)
	case method == http.MethodGet && path == "/feeds":
		result = s.getFeeds(ctx, claims.Subject)
	case method == http.MethodGet && path == "/feeds/counts":
		result = s.getFeedItemCounts(ctx, claims.Subject)
	case method == http.MethodGet && path == "/feeds/export.opml":
		result = s.exportFeeds(ctx, claims.Subject)
	case method == http.MethodPost && path == "/feeds/discover":
		result = s.discoverFeeds(ctx, request.Body)
	case method == http.MethodPost && path == "/feeds":
		result = s.addFeed(ctx, claims.Subject, request.Body)
	case method == http.MethodPost && path == "/feeds/import":
		result = s.importFeeds(ctx, claims.Subject, request)
	case strings.HasPrefix(path, "/feeds/"):
		result = s.feedRoute(ctx, claims.Subject, method, strings.TrimPrefix(path, "/feeds/"), request.Body)
	default:
		result = response(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	if cookies, cookieErr := s.signer.Cookies(claims.Subject, time.Now().UTC()); cookieErr != nil {
		return s.failure("sign content cookies", cookieErr), nil
	} else {
		result.Cookies = cookies
	}
	if renewedCookie != "" {
		result.Cookies = append(result.Cookies, renewedCookie)
	}
	return result, nil
}

func apiPath(raw string) string {
	path := strings.TrimRight(raw, "/")
	path = strings.TrimPrefix(path, "/api")
	if path == "" {
		return "/"
	}
	return path
}

func apiRouteTemplate(method, path string) string {
	method = boundedMethod(method)
	for _, static := range []string{
		"/", "/session", "/me", "/items", "/stories", "/search", "/items/read-batch", "/ranking/recompute",
		"/archive", "/feeds", "/feeds/counts", "/feeds/export.opml", "/feeds/discover", "/feeds/import",
	} {
		if path == static {
			return method + " " + static
		}
	}
	if parts := routeParts(path, "/archive/"); len(parts) == 1 {
		return method + " /archive/{item_id}"
	}
	if parts := routeParts(path, "/items/"); len(parts) == 1 {
		return method + " /items/{item_id}"
	} else if len(parts) == 2 {
		switch parts[1] {
		case "similar", "retry", "heart", "signal", "read", "events":
			return method + " /items/{item_id}/" + parts[1]
		default:
			return method + " /items/{item_id}/{action}"
		}
	}
	if parts := routeParts(path, "/feeds/"); len(parts) == 1 {
		return method + " /feeds/{feed_id}"
	} else if len(parts) == 2 && parts[1] == "retry" {
		return method + " /feeds/{feed_id}/retry"
	}
	return "$unmatched"
}

func routeParts(path, prefix string) []string {
	if !strings.HasPrefix(path, prefix) {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	for _, part := range parts {
		if part == "" {
			return nil
		}
	}
	return parts
}

func boundedMethod(method string) string {
	switch method {
	case http.MethodDelete, http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut:
		return method
	default:
		return "OTHER"
	}
}

func (s *server) postSession(ctx context.Context, body string) events.APIGatewayV2HTTPResponse {
	var input struct {
		Credential string `json:"credential"`
	}
	if err := decodeJSON(body, &input); err != nil || strings.TrimSpace(input.Credential) == "" || s.verifyGoogle == nil {
		return response(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	claims, err := s.verifyGoogle(ctx, input.Credential)
	if err != nil {
		return response(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
	if err := s.store.EnsureUser(ctx, claims.Subject, claims.Email); err != nil {
		return s.failure("ensure user", err)
	}
	result := s.getMe(ctx, claims.Subject)
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		return result
	}
	contentCookies, err := s.signer.Cookies(claims.Subject, time.Now().UTC())
	if err != nil {
		return s.failure("sign content cookies", err)
	}
	sessionCookie, err := s.sessions.Create(ctx, claims)
	if err != nil {
		return s.failure("create session", err)
	}
	result.Cookies = append(contentCookies, sessionCookie)
	return result
}

func crossSiteMutation(method string, headers map[string]string) bool {
	if method == http.MethodGet || method == http.MethodHead {
		return false
	}
	site := ""
	for name, value := range headers {
		if strings.EqualFold(name, "sec-fetch-site") {
			site = strings.ToLower(strings.TrimSpace(value))
			break
		}
	}
	return site != "" && site != "same-origin" && site != "none"
}

type searchGroup struct {
	Window  []domain.Item `json:"window"`
	Archive []domain.Item `json:"archive"`
}

func (s *server) getSearch(ctx context.Context, userID string, query map[string]string) events.APIGatewayV2HTTPResponse {
	value := strings.TrimSpace(query["q"])
	terms := strings.Fields(strings.ToLower(value))
	if len([]rune(value)) < 2 || len(terms) == 0 {
		return badRequest(errors.New("q must contain at least 2 characters"))
	}
	limit, _ := strconv.Atoi(query["limit"])
	if limit < 1 || limit > 30 {
		limit = 30
	}
	window, err := s.store.SearchItems(ctx, userID, "I#", terms, limit)
	if err != nil {
		return s.failure("search live items", err)
	}
	archive, err := s.store.SearchItems(ctx, userID, "A#", terms, limit)
	if err != nil {
		return s.failure("search archive", err)
	}
	for _, items := range [][]domain.Item{window, archive} {
		if err := s.applyFeedPresentation(ctx, userID, items); err != nil {
			return s.failure("apply search feed presentation", err)
		}
		if err := s.prepareItems(ctx, userID, items); err != nil {
			return s.failure("prepare search results", err)
		}
	}
	exact := make(map[string]bool, len(window)+len(archive))
	for _, items := range [][]domain.Item{window, archive} {
		for _, item := range items {
			exact[item.ItemID] = true
		}
	}
	related, semanticAvailable := s.semanticResults(ctx, userID, value, limit, exact)
	return response(http.StatusOK, map[string]any{
		"matches": searchGroup{Window: window, Archive: archive},
		"related": related, "semantic_available": semanticAvailable,
	})
}

func (s *server) semanticResults(ctx context.Context, userID, query string, limit int, exclude map[string]bool) (searchGroup, bool) {
	related := searchGroup{Window: []domain.Item{}, Archive: []domain.Item{}}
	textConfigured := s.embedder != nil && s.vectors != nil
	imageConfigured := s.imageEmbedder != nil && s.imageVectors != nil
	if !textConfigured && !imageConfigured {
		return related, false
	}
	type queryResult struct {
		matches []vectorstore.Match
		err     error
	}
	queryStore := func(embed func(context.Context, string) ([]float32, error), vectors vectorstore.Store) queryResult {
		vector, err := embed(ctx, query)
		if err != nil {
			return queryResult{err: err}
		}
		matches, err := vectors.Query(ctx, score.Normalize(vector), limit, time.Now().Unix())
		return queryResult{matches: matches, err: err}
	}
	textResults, imageResults := queryResult{}, queryResult{}
	textDone, imageDone := make(chan queryResult, 1), make(chan queryResult, 1)
	if textConfigured {
		go func() { textDone <- queryStore(s.embedder.Embed, s.vectors) }()
	}
	if imageConfigured {
		go func() { imageDone <- queryStore(s.imageEmbedder.EmbedText, s.imageVectors) }()
	}
	if textConfigured {
		textResults = <-textDone
		if textResults.err != nil {
			slog.WarnContext(ctx, "semantic search text query failed", "error", textResults.err)
		}
	}
	if imageConfigured {
		imageResults = <-imageDone
		if imageResults.err != nil {
			slog.WarnContext(ctx, "semantic search image query failed", "error", imageResults.err)
		} else {
			similarities := make([]int, len(imageResults.matches))
			top := 0
			for index, match := range imageResults.matches {
				similarities[index] = match.Similarity
				top = max(top, match.Similarity)
			}
			slog.DebugContext(ctx, "image semantic search similarities", "similarities", similarities)
			emit := s.emit
			if emit == nil {
				emit = observability.Emit
			}
			emit(map[string]float64{"ImageSearchMatches": float64(len(imageResults.matches)), "ImageSearchTopSimilarity": float64(top)}, nil)
		}
	}
	available := (textConfigured && textResults.err == nil) || (imageConfigured && imageResults.err == nil)
	if !available {
		return related, false
	}
	filterExcluded := func(matches []vectorstore.Match) []vectorstore.Match {
		filtered := make([]vectorstore.Match, 0, len(matches))
		for _, match := range matches {
			if !exclude[match.Key] {
				filtered = append(filtered, match)
			}
		}
		return filtered
	}
	fused := fuseMatches(filterExcluded(textResults.matches), filterExcluded(imageResults.matches), s.imageSearchFloor, limit)
	ids := make([]string, len(fused))
	for index := range fused {
		ids[index] = fused[index].Key
	}
	items, err := s.store.ResolveItemIDs(ctx, userID, ids)
	if err != nil {
		return related, false
	}
	byID := make(map[string]fusedMatch, len(fused))
	for _, match := range fused {
		byID[match.Key] = match
	}
	for index := range items {
		match := byID[items[index].ItemID]
		similarity := match.Similarity
		items[index].Similarity = &similarity
		items[index].MatchSource = match.MatchSource
	}
	if err := s.applyFeedPresentation(ctx, userID, items); err != nil {
		return related, false
	}
	if err := s.prepareItems(ctx, userID, items); err != nil {
		return related, false
	}
	for _, item := range items {
		if item.HeartedTS != "" && item.TTL == 0 {
			related.Archive = append(related.Archive, item)
		} else {
			related.Window = append(related.Window, item)
		}
	}
	return related, available
}

func (s *server) getSimilar(ctx context.Context, userID, itemID string, query map[string]string) events.APIGatewayV2HTTPResponse {
	if itemID == "" || strings.Contains(itemID, "/") {
		return response(http.StatusNotFound, map[string]string{"error": "item not found"})
	}
	if s.vectors == nil {
		return s.failure("find similar items", errors.New("vector search is not configured"))
	}
	limit, _ := strconv.Atoi(query["limit"])
	if limit < 1 || limit > 12 {
		limit = 12
	}
	vector, err := s.vectors.Get(ctx, itemID)
	if err != nil {
		if errors.Is(err, vectorstore.ErrNotFound) {
			return response(http.StatusNotFound, map[string]string{"error": "item vector not found"})
		}
		return s.failure("load item vector", err)
	}
	matches, err := s.vectors.Query(ctx, vector, limit+1, time.Now().Unix())
	if err != nil {
		return s.failure("query similar items", err)
	}
	textMatches := similarMatches(matches, itemID, limit)
	imageMatches := []vectorstore.Match{}
	if s.imageVectors != nil {
		item, imageErr := s.store.Item(ctx, userID, itemID)
		if errors.Is(imageErr, store.ErrNotFound) {
			item, imageErr = s.store.ArchiveItem(ctx, userID, itemID)
		}
		if imageErr == nil && len(item.ImageVector) > 0 {
			imageMatches, imageErr = s.imageVectors.Query(ctx, score.DecodeVector(item.ImageVector), limit+1, time.Now().Unix())
			if imageErr == nil {
				imageMatches = matchesWithoutItem(imageMatches, itemID)
			}
		}
		if imageErr != nil && !errors.Is(imageErr, store.ErrNotFound) {
			slog.WarnContext(ctx, "image similarity query failed", "user", userID, "item_id", itemID, "error", imageErr)
			imageMatches = nil
		}
	}
	fused := fuseMatches(textMatches, imageMatches, s.imageSearchFloor, limit)
	ids := make([]string, len(fused))
	for index := range fused {
		ids[index] = fused[index].Key
	}
	items, err := s.store.ResolveItemIDs(ctx, userID, ids)
	if err != nil {
		return s.failure("resolve similar items", err)
	}
	byID := make(map[string]fusedMatch, len(fused))
	for _, match := range fused {
		byID[match.Key] = match
	}
	for index := range items {
		match := byID[items[index].ItemID]
		value := match.Similarity
		items[index].Similarity = &value
		items[index].MatchSource = match.MatchSource
	}
	if err := s.applyFeedPresentation(ctx, userID, items); err != nil {
		return s.failure("apply similar feed presentation", err)
	}
	if err := s.prepareItems(ctx, userID, items); err != nil {
		return s.failure("prepare similar items", err)
	}
	return response(http.StatusOK, map[string]any{"items": items})
}

func matchesWithoutItem(matches []vectorstore.Match, itemID string) []vectorstore.Match {
	filtered := make([]vectorstore.Match, 0, len(matches))
	for _, match := range matches {
		if match.Key != itemID {
			filtered = append(filtered, match)
		}
	}
	return filtered
}

func similarMatches(matches []vectorstore.Match, self string, limit int) []vectorstore.Match {
	filtered := make([]vectorstore.Match, 0, limit)
	for _, match := range matches {
		if match.Key == self || match.Similarity < 40 {
			continue
		}
		filtered = append(filtered, match)
		if len(filtered) == limit {
			break
		}
	}
	return filtered
}

func (s *server) getMe(ctx context.Context, userID string) events.APIGatewayV2HTTPResponse {
	user, err := s.store.User(ctx, userID)
	if err != nil {
		return s.failure("get profile", err)
	}
	model, modelErr := s.store.Model(ctx, userID)
	if modelErr != nil && !errors.Is(modelErr, score.ErrModelNotFound) {
		return s.failure("get ranking model", modelErr)
	}
	return response(http.StatusOK, map[string]any{"profile": user, "signal_count": user.SignalCount, "heart_count": user.HeartCount, "model": model})
}

func (s *server) recomputeRanking(ctx context.Context, userID string) events.APIGatewayV2HTTPResponse {
	if s.rescore == nil {
		return s.failure("recompute ranking", errors.New("ranking service is not configured"))
	}
	if err := s.rescore(ctx, userID); err != nil {
		return s.failure("recompute ranking", err)
	}
	model, err := s.store.Model(ctx, userID)
	if err != nil {
		return s.failure("load recomputed ranking", err)
	}
	return response(http.StatusOK, map[string]any{"model": model})
}

func (s *server) patchMe(ctx context.Context, userID, body string) events.APIGatewayV2HTTPResponse {
	var input struct {
		OrderPref        *domain.Order `json:"order_pref"`
		InterestPosition *string       `json:"interest_position"`
		TagPref          *string       `json:"tag_pref"`
		FeedPref         *string       `json:"feed_pref"`
	}
	if err := decodeJSON(body, &input); err != nil {
		return badRequest(err)
	}
	if input.OrderPref != nil && *input.OrderPref != domain.OrderChrono && *input.OrderPref != domain.OrderInterest {
		return badRequest(errors.New("order_pref must be chrono or interest"))
	}
	if input.TagPref != nil {
		*input.TagPref = strings.ToLower(strings.TrimSpace(*input.TagPref))
		if len([]rune(*input.TagPref)) > 32 {
			return badRequest(errors.New("tag_pref must be at most 32 characters"))
		}
	}
	if input.FeedPref != nil {
		*input.FeedPref = strings.TrimSpace(*input.FeedPref)
		if len([]rune(*input.FeedPref)) > 128 {
			return badRequest(errors.New("feed_pref must be at most 128 characters"))
		}
	}
	if input.TagPref != nil && input.FeedPref != nil && *input.TagPref != "" && *input.FeedPref != "" {
		return badRequest(errors.New("tag_pref and feed_pref cannot both be set"))
	}
	empty := ""
	if input.FeedPref != nil && *input.FeedPref != "" {
		input.TagPref = &empty
	} else if input.TagPref != nil && *input.TagPref != "" {
		input.FeedPref = &empty
	}
	if err := s.store.UpdateUser(ctx, userID, input.OrderPref, input.InterestPosition, input.TagPref, input.FeedPref); err != nil {
		return s.failure("update profile", err)
	}
	return response(http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) getItems(ctx context.Context, userID string, query map[string]string) events.APIGatewayV2HTTPResponse {
	order := domain.Order(query["order"])
	if order == "" {
		order = domain.OrderChrono
	}
	if order != domain.OrderChrono && order != domain.OrderInterest {
		return badRequest(errors.New("order must be chrono or interest"))
	}
	includeRead, err := parseIncludeRead(query["include_read"])
	if err != nil {
		return badRequest(err)
	}
	limit, _ := strconv.Atoi(query["limit"])
	allowed, err := s.allowedFeedIDs(ctx, userID, query["tag"], query["feed"])
	if err != nil {
		if errors.Is(err, errInvalidFeedTag) {
			return badRequest(err)
		}
		return s.failure("load feeds for item filtering", err)
	}
	excludeStories, err := parseBool(query["exclude_stories"], "exclude_stories")
	if err != nil {
		return badRequest(err)
	}
	var hidden map[string]bool
	if excludeStories {
		_, hidden, err = s.loadAndRenderStories(ctx, userID, allowed, !includeRead)
		if err != nil {
			return s.failure("render stories for item filtering", err)
		}
	}
	items, next, readAnchor, err := s.store.ItemsForFeeds(ctx, userID, order, query["cursor"], limit, includeRead, allowed, hidden)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return badRequest(err)
		}
		return s.failure("list items", err)
	}
	if err := s.applyFeedPresentation(ctx, userID, items); err != nil {
		return s.failure("apply feed presentation", err)
	}
	if err := s.prepareItems(ctx, userID, items); err != nil {
		return s.failure("prepare items", err)
	}
	payload := map[string]any{"items": items, "next_cursor": next}
	if !includeRead && readAnchor != nil {
		payload["read_anchor"] = map[string]string{
			"item_id":      readAnchor.ItemID,
			"published_ts": readAnchor.PublishedTS,
		}
	}
	return response(http.StatusOK, payload)
}

func (s *server) getStories(ctx context.Context, userID string, query map[string]string) events.APIGatewayV2HTTPResponse {
	includeRead, err := parseIncludeRead(query["include_read"])
	if err != nil {
		return badRequest(err)
	}
	allowed, err := s.allowedFeedIDs(ctx, userID, query["tag"], query["feed"])
	if err != nil {
		if errors.Is(err, errInvalidFeedTag) {
			return badRequest(err)
		}
		return s.failure("load feeds for story filtering", err)
	}
	stories, _, err := s.loadAndRenderStories(ctx, userID, allowed, !includeRead)
	if err != nil {
		return s.failure("list stories", err)
	}
	return response(http.StatusOK, map[string]any{"stories": stories})
}

func (s *server) loadAndRenderStories(ctx context.Context, userID string, allowed map[string]bool, unreadOnly bool) ([]storycluster.Rendered, map[string]bool, error) {
	rows, err := s.store.Stories(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	ids := make([]string, 0)
	for _, row := range rows {
		ids = append(ids, row.MemberIDs...)
	}
	items, err := s.store.ResolveItemIDs(ctx, userID, ids)
	if err != nil {
		return nil, nil, err
	}
	live := items[:0]
	now := time.Now().Unix()
	for _, item := range items {
		if strings.HasPrefix(item.SK, "I#") && item.TTL > now {
			live = append(live, item)
		}
	}
	items = live
	if err := s.applyFeedPresentation(ctx, userID, items); err != nil {
		return nil, nil, err
	}
	if err := s.prepareItems(ctx, userID, items); err != nil {
		return nil, nil, err
	}
	byID := make(map[string]domain.Item, len(items))
	for _, item := range items {
		byID[item.ItemID] = item
	}
	members := make(map[string][]domain.Item, len(rows))
	for _, row := range rows {
		for _, itemID := range row.MemberIDs {
			if item, ok := byID[itemID]; ok {
				members[row.StoryID] = append(members[row.StoryID], item)
			}
		}
	}
	model, modelErr := s.store.Model(ctx, userID)
	if modelErr != nil && !errors.Is(modelErr, score.ErrModelNotFound) {
		return nil, nil, modelErr
	}
	rendered, hidden := storycluster.Render(rows, members, allowed, unreadOnly, model)
	return rendered, hidden, nil
}

func (s *server) getArchive(ctx context.Context, userID string, query map[string]string) events.APIGatewayV2HTTPResponse {
	limit, _ := strconv.Atoi(query["limit"])
	items, next, err := s.store.Archives(ctx, userID, query["cursor"], limit)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return badRequest(err)
		}
		return s.failure("list archive", err)
	}
	if err := s.applyFeedPresentation(ctx, userID, items); err != nil {
		return s.failure("apply archive feed presentation", err)
	}
	if err := s.prepareItems(ctx, userID, items); err != nil {
		return s.failure("prepare archive", err)
	}
	return response(http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}

func (s *server) getArchiveItem(ctx context.Context, userID, itemID string) events.APIGatewayV2HTTPResponse {
	if itemID == "" || strings.Contains(itemID, "/") {
		return response(http.StatusNotFound, map[string]string{"error": "archive item not found"})
	}
	item, err := s.store.ArchiveItem(ctx, userID, itemID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return response(http.StatusNotFound, map[string]string{"error": "archive item not found"})
		}
		return s.failure("get archive item", err)
	}
	items := []domain.Item{item}
	if err := s.prepareItems(ctx, userID, items); err != nil {
		return s.failure("prepare archive item", err)
	}
	return response(http.StatusOK, items[0])
}

func (s *server) prepareItems(ctx context.Context, userID string, items []domain.Item) error {
	itemIDs := make([]string, len(items))
	for i := range items {
		itemIDs[i] = items[i].ItemID
	}
	byID, err := s.store.SignalValues(ctx, userID, itemIDs)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].Signal = byID[items[i].ItemID]
		items[i] = s.store.PublicItem(items[i])
	}
	return nil
}

func parseIncludeRead(value string) (bool, error) {
	return parseBool(value, "include_read")
}

func parseBool(value, name string) (bool, error) {
	switch value {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, fmt.Errorf("%s must be true or false", name)
	}
}

func (s *server) itemRoute(ctx context.Context, userID, method, suffix, body string) events.APIGatewayV2HTTPResponse {
	parts := strings.Split(suffix, "/")
	itemID := parts[0]
	if itemID == "" || len(parts) > 2 {
		return response(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	if method == http.MethodGet && len(parts) == 1 {
		item, err := s.store.Item(ctx, userID, itemID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return response(http.StatusNotFound, map[string]string{"error": "item not found"})
			}
			return s.failure("get item", err)
		}
		items := []domain.Item{item}
		if err := s.store.ResolveRead(ctx, userID, items); err != nil {
			return s.failure("resolve read state", err)
		}
		if err := s.prepareItems(ctx, userID, items); err != nil {
			return s.failure("prepare item", err)
		}
		return response(http.StatusOK, items[0])
	}
	if method != http.MethodPost || len(parts) != 2 {
		return response(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	switch parts[1] {
	case "retry":
		item, err := s.store.Item(ctx, userID, itemID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return response(http.StatusNotFound, map[string]string{"error": "item not found"})
			}
			return s.failure("get item for retry", err)
		}
		message := domain.ItemMessage{
			User: userID, FeedID: item.FeedID, ItemID: item.ItemID, URL: item.URL, ExternalURL: item.ExternalURL, PostType: item.PostType, Title: item.Title, Author: item.Author,
			PublishedTS: item.PublishedTS, DisplayDate: item.DisplayDate, Reprocess: true, ForceExtract: true, ForceSummary: true,
		}
		encoded, _ := json.Marshal(message)
		output, err := s.queue.SendMessageBatch(ctx, &sqs.SendMessageBatchInput{
			QueueUrl: aws.String(s.itemsURL), Entries: []types.SendMessageBatchRequestEntry{{Id: aws.String("retry-" + item.ItemID), MessageBody: aws.String(string(encoded))}},
		})
		if err != nil || output == nil || len(output.Failed) > 0 {
			if err == nil {
				err = fmt.Errorf("item retry enqueue failed")
			}
			return s.failure("enqueue item retry", err)
		}
		return response(http.StatusAccepted, map[string]bool{"queued": true})
	case "heart":
		var input struct {
			Hearted *bool `json:"hearted"`
		}
		if err := decodeJSON(body, &input); err != nil {
			return badRequest(err)
		}
		if input.Hearted == nil {
			return badRequest(errors.New("hearted is required"))
		}
		archiveSK, heartCount, err := s.store.SetHeart(ctx, userID, itemID, *input.Hearted)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return response(http.StatusNotFound, map[string]string{"error": "item not found"})
			}
			return s.failure("set heart", err)
		}
		if s.vectors != nil {
			if vectorErr := s.syncHeartVector(ctx, userID, itemID, *input.Hearted); vectorErr != nil {
				slog.WarnContext(ctx, "heart vector update failed", "user", userID, "item_id", itemID, "hearted", *input.Hearted, "error", vectorErr)
			}
		}
		return response(http.StatusOK, map[string]any{"archive_sk": archiveSK, "heart_count": heartCount})
	case "signal":
		item, err := s.store.Item(ctx, userID, itemID)
		if errors.Is(err, store.ErrNotFound) {
			item, err = s.store.ArchiveItem(ctx, userID, itemID)
		}
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return response(http.StatusNotFound, map[string]string{"error": "item not found"})
			}
			return s.failure("get item for signal", err)
		}
		var input struct {
			Value int `json:"value"`
		}
		if err := decodeJSON(body, &input); err != nil {
			return badRequest(err)
		}
		if input.Value != -1 && input.Value != 0 && input.Value != 1 {
			return badRequest(errors.New("value must be -1, 0, or 1"))
		}
		if input.Value != 0 && len(item.Vector) == 0 {
			return response(http.StatusConflict, map[string]string{"error": "item has no embedding"})
		}
		if err := s.store.SetSignal(ctx, userID, item, input.Value); err != nil {
			return s.failure("set signal", err)
		}
	case "read":
		if _, err := s.store.Item(ctx, userID, itemID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return response(http.StatusNotFound, map[string]string{"error": "item not found"})
			}
			return s.failure("get item for read state", err)
		}
		var input struct {
			Read bool `json:"read"`
		}
		if err := decodeJSON(body, &input); err != nil {
			return badRequest(err)
		}
		if err := s.store.SetRead(ctx, userID, []string{itemID}, input.Read); err != nil {
			return s.failure("set read state", err)
		}
	case "events":
		item, err := s.store.Item(ctx, userID, itemID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return response(http.StatusNotFound, map[string]string{"error": "item not found"})
			}
			return s.failure("get item for behaviour event", err)
		}
		var input struct {
			Opened         bool   `json:"opened"`
			DwellMS        *int64 `json:"dwell_ms"`
			ClickedThrough bool   `json:"clicked_through"`
			Shared         bool   `json:"shared"`
		}
		if err := decodeJSON(body, &input); err != nil {
			return badRequest(err)
		}
		if !input.Opened && input.DwellMS == nil && !input.ClickedThrough && !input.Shared {
			return badRequest(errors.New("at least one behaviour event is required"))
		}
		if input.DwellMS != nil && (*input.DwellMS < 0 || *input.DwellMS > int64((24*time.Hour)/time.Millisecond)) {
			return badRequest(errors.New("dwell_ms must be between 0 and 86400000"))
		}
		if len(item.Vector) == 0 {
			return response(http.StatusConflict, map[string]string{"error": "item has no embedding"})
		}
		if err := s.store.RecordBehaviour(ctx, userID, item, store.BehaviourEvent{
			Opened: input.Opened, DwellMS: input.DwellMS, ClickedThrough: input.ClickedThrough, Shared: input.Shared,
		}); err != nil {
			return s.failure("record behaviour event", err)
		}
	default:
		return response(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	return response(http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) syncHeartVector(ctx context.Context, userID, itemID string, hearted bool) error {
	if hearted {
		item, err := s.store.ArchiveItem(ctx, userID, itemID)
		if err != nil {
			return err
		}
		if err := s.vectors.Put(ctx, vectorstore.FromItem(item, vectorstore.KindArchive)); err != nil {
			return err
		}
		if s.imageVectors != nil {
			if record, ok := vectorstore.ImageRecordFromItem(item, vectorstore.KindArchive); ok {
				return s.imageVectors.Put(ctx, record)
			}
		}
		return nil
	}
	item, err := s.store.Item(ctx, userID, itemID)
	if err == nil {
		if err := s.vectors.Put(ctx, vectorstore.FromItem(item, vectorstore.KindLive)); err != nil {
			return err
		}
		if s.imageVectors != nil {
			if record, ok := vectorstore.ImageRecordFromItem(item, vectorstore.KindLive); ok {
				return s.imageVectors.Put(ctx, record)
			}
			return s.imageVectors.Delete(ctx, itemID)
		}
		return nil
	}
	if errors.Is(err, store.ErrNotFound) {
		if err := s.vectors.Delete(ctx, itemID); err != nil {
			return err
		}
		if s.imageVectors != nil {
			return s.imageVectors.Delete(ctx, itemID)
		}
		return nil
	}
	return err
}

func (s *server) readBatch(ctx context.Context, userID, body string) events.APIGatewayV2HTTPResponse {
	var input struct {
		IDs  []string `json:"ids"`
		Read *bool    `json:"read,omitempty"`
	}
	if err := decodeJSON(body, &input); err != nil {
		return badRequest(err)
	}
	if len(input.IDs) > 100 {
		return badRequest(errors.New("at most 100 ids are allowed"))
	}
	read := true
	if input.Read != nil {
		read = *input.Read
	}
	if err := s.store.SetRead(ctx, userID, input.IDs, read); err != nil {
		return s.failure("batch read state", err)
	}
	return response(http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) getFeeds(ctx context.Context, userID string) events.APIGatewayV2HTTPResponse {
	feeds, err := s.cachedDetailedFeeds(ctx, userID)
	if err != nil {
		return s.failure("decorate feeds", err)
	}
	return response(http.StatusOK, map[string]any{"feeds": feeds})
}

func (s *server) getFeedItemCounts(ctx context.Context, userID string) events.APIGatewayV2HTTPResponse {
	counts, err := s.store.FeedItemCounts(ctx, userID)
	if err != nil {
		return s.failure("count live feed items", err)
	}
	return response(http.StatusOK, map[string]any{"feeds": counts})
}

func (s *server) importFeeds(ctx context.Context, userID string, request events.APIGatewayV2HTTPRequest) events.APIGatewayV2HTTPResponse {
	contentType := request.Headers["content-type"]
	if contentType == "" {
		contentType = request.Headers["Content-Type"]
	}
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" || parameters["boundary"] == "" {
		return badRequest(errors.New("multipart/form-data with an OPML file is required"))
	}
	body := []byte(request.Body)
	if request.IsBase64Encoded {
		body, err = base64.StdEncoding.DecodeString(request.Body)
		if err != nil {
			return badRequest(errors.New("invalid base64 request body"))
		}
	}
	reader := multipart.NewReader(bytes.NewReader(body), parameters["boundary"])
	var subscriptions []rss.Subscription
	for {
		part, partErr := reader.NextPart()
		if partErr != nil {
			break
		}
		if part.FileName() == "" && part.FormName() != "file" {
			continue
		}
		subscriptions, err = rss.ParseOPML(part)
		break
	}
	if err != nil {
		return badRequest(err)
	}
	if len(subscriptions) == 0 {
		return badRequest(errors.New("no OPML file found"))
	}
	supported := make([]rss.Subscription, 0, len(subscriptions))
	unsupported := make([]unsupportedFeed, 0)
	for _, subscription := range subscriptions {
		if reddit.Matches(subscription.URL) {
			if _, redditErr := reddit.ParseInput(subscription.URL); redditErr != nil {
				unsupported = append(unsupported, unsupportedFeed{Title: subscription.Title, URL: subscription.URL, Reason: redditErr.Error()})
				continue
			}
		}
		if reason := rss.UnsupportedReason(subscription.URL); reason != "" {
			unsupported = append(unsupported, unsupportedFeed{Title: subscription.Title, URL: subscription.URL, Reason: reason})
			continue
		}
		supported = append(supported, subscription)
	}
	subscriptions = supported
	if len(subscriptions) == 0 {
		return response(http.StatusAccepted, importFeedsResult{Unsupported: unsupported})
	}
	for index := range subscriptions {
		feedURL, normalizeErr := normalizeFeedURL(subscriptions[index].URL)
		if normalizeErr != nil {
			return badRequest(fmt.Errorf("invalid OPML feed URL %q", subscriptions[index].URL))
		}
		if parsed, redditErr := reddit.ParseInput(feedURL); redditErr == nil {
			feedURL = reddit.CanonicalURL(parsed.Subreddit, parsed.Sort)
			subscriptions[index].IntervalH = reddit.IntervalHours(parsed.Sort)
			if strings.TrimSpace(subscriptions[index].Title) == "" {
				subscriptions[index].Title = reddit.Title(parsed.Subreddit)
			}
		}
		tags, tagsErr := normalizeTags(subscriptions[index].Tags)
		if tagsErr != nil {
			return badRequest(tagsErr)
		}
		title, titleErr := normalizeTitle(subscriptions[index].Title)
		if titleErr != nil {
			return badRequest(titleErr)
		}
		subscriptions[index].URL = feedURL
		subscriptions[index].Tags = tags
		subscriptions[index].Title = title
	}
	now := time.Now().UTC()
	messages := make([]domain.FeedMessage, len(subscriptions))
	semaphore := make(chan struct{}, 20)
	writeErrors := make(chan error, len(subscriptions))
	var group sync.WaitGroup
	for index, subscription := range subscriptions {
		group.Add(1)
		go func(index int, subscription rss.Subscription) {
			defer group.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			feedID := domain.FeedID(subscription.URL)
			connectorName := domain.ConnectorRSS
			siteURL := ""
			title := ""
			if youtube.IsFeedURL(subscription.URL) {
				connectorName = domain.ConnectorYouTube
			} else if parsed, redditErr := reddit.ParseInput(subscription.URL); redditErr == nil {
				connectorName = domain.ConnectorReddit
				feedID = domain.FeedID(reddit.SiteURL(parsed.Subreddit))
				siteURL = reddit.SiteURL(parsed.Subreddit)
				title = reddit.Title(parsed.Subreddit)
			}
			feed := domain.Feed{
				PK: domain.UserPK(userID), SK: domain.FeedSK(feedID), FeedID: feedID, Connector: connectorName, URL: subscription.URL,
				Title: title, SiteURL: siteURL, CustomTitle: subscription.Title, Tags: subscription.Tags, Muted: subscription.Muted,
				FetchIntervalH: subscription.IntervalH, NextFetchAt: domain.Timestamp(now), LastStatus: "queued",
			}
			if existing, getErr := s.store.Feed(ctx, userID, feedID); getErr == nil {
				feed = existing
				feed.Connector = connectorName
				merged, mergeErr := normalizeTags(append(append([]string{}, feed.Tags...), subscription.Tags...))
				if mergeErr != nil {
					writeErrors <- mergeErr
					return
				}
				feed.Tags = merged
				if feed.CustomTitle == "" {
					feed.CustomTitle = subscription.Title
				}
				if !feed.Muted {
					feed.NextFetchAt = domain.Timestamp(now)
				}
			} else if !errors.Is(getErr, store.ErrNotFound) {
				writeErrors <- getErr
				return
			}
			if err := s.store.PutFeed(ctx, feed); err != nil {
				writeErrors <- err
				return
			}
			if !feed.Muted {
				messages[index] = domain.FeedMessage{User: userID, FeedID: feedID}
			}
		}(index, subscription)
	}
	group.Wait()
	close(writeErrors)
	var importErrors []error
	for err := range writeErrors {
		importErrors = append(importErrors, err)
	}
	if err := errors.Join(importErrors...); err != nil {
		s.invalidateFeeds(userID)
		return s.failure("store imported feed", err)
	}
	queued := messages[:0]
	for _, message := range messages {
		if message.FeedID != "" {
			queued = append(queued, message)
		}
	}
	if err := s.enqueueFeeds(ctx, queued); err != nil {
		s.invalidateFeeds(userID)
		return s.failure("enqueue imported feeds", err)
	}
	s.invalidateFeeds(userID)
	return response(http.StatusAccepted, importFeedsResult{Imported: len(messages), Unsupported: unsupported})
}

func (s *server) enqueueFeeds(ctx context.Context, messages []domain.FeedMessage) error {
	enqueueErrors := make(chan error, (len(messages)+9)/10)
	var group sync.WaitGroup
	for offset := 0; offset < len(messages); offset += 10 {
		end := min(offset+10, len(messages))
		group.Add(1)
		go func(offset, end int) {
			defer group.Done()
			entries := make([]types.SendMessageBatchRequestEntry, 0, end-offset)
			for i, message := range messages[offset:end] {
				body, _ := json.Marshal(message)
				entries = append(entries, types.SendMessageBatchRequestEntry{Id: aws.String(fmt.Sprintf("feed-%d", offset+i)), MessageBody: aws.String(string(body))})
			}
			result, err := s.queue.SendMessageBatch(ctx, &sqs.SendMessageBatchInput{QueueUrl: aws.String(s.feedsURL), Entries: entries})
			if err != nil {
				enqueueErrors <- err
				return
			}
			if len(result.Failed) > 0 {
				enqueueErrors <- fmt.Errorf("%d feeds failed to enqueue", len(result.Failed))
			}
		}(offset, end)
	}
	group.Wait()
	close(enqueueErrors)
	var collected []error
	for err := range enqueueErrors {
		collected = append(collected, err)
	}
	return errors.Join(collected...)
}

func (s *server) deleteFeed(ctx context.Context, userID, feedID string) events.APIGatewayV2HTTPResponse {
	if feedID == "" {
		return badRequest(errors.New("feed id is required"))
	}
	if err := s.store.DeleteFeed(ctx, userID, feedID); err != nil {
		return s.failure("delete feed", err)
	}
	s.invalidateFeeds(userID)
	return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusNoContent}
}

func (s *server) failure(operation string, err error) events.APIGatewayV2HTTPResponse {
	slog.Error(operation, "error", err)
	return response(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
}

func badRequest(err error) events.APIGatewayV2HTTPResponse {
	return response(http.StatusBadRequest, map[string]string{"error": err.Error()})
}

func decodeJSON(body string, value any) error {
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func response(status int, body any) events.APIGatewayV2HTTPResponse {
	encoded, err := json.Marshal(normalizeNilJSONSlices(body))
	if err != nil {
		slog.Error("encode response", "error", err)
		status = http.StatusInternalServerError
		encoded = []byte(`{"error":"internal server error"}`)
	}
	return events.APIGatewayV2HTTPResponse{
		StatusCode: status, Body: string(encoded), Headers: map[string]string{"content-type": "application/json; charset=utf-8", "cache-control": "no-store"},
	}
}

type lambdaInvoker struct {
	config   aws.Config
	function string
	client   *http.Client
}

func (i *lambdaInvoker) invokeRescore(ctx context.Context, userID string) error {
	payload, err := json.Marshal(map[string]any{"user": userID, "on_demand": true})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf(
		"https://lambda.%s.amazonaws.com/2015-03-31/functions/%s/invocations",
		i.config.Region,
		url.PathEscape(i.function),
	)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("content-type", "application/json")
	request.Header.Set("x-amz-invocation-type", "RequestResponse")
	credentials, err := i.config.Credentials.Retrieve(ctx)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(payload)
	if err := v4.NewSigner().SignHTTP(
		ctx, credentials, request, hex.EncodeToString(hash[:]), "lambda", i.config.Region, time.Now().UTC(),
	); err != nil {
		return err
	}
	output, err := i.client.Do(request)
	if err != nil {
		return err
	}
	defer output.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(output.Body, 1<<20))
	if readErr != nil {
		return readErr
	}
	if output.StatusCode < 200 || output.StatusCode >= 300 {
		return fmt.Errorf("rescore Lambda returned %s: %s", output.Status, strings.TrimSpace(string(body)))
	}
	if functionError := output.Header.Get("x-amz-function-error"); functionError != "" {
		return fmt.Errorf("rescore Lambda failed (%s): %s", functionError, strings.TrimSpace(string(body)))
	}
	return nil
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	repository, config, err := store.FromEnv(context.Background())
	if err != nil {
		panic(err)
	}
	queueURL, itemsURL := os.Getenv("FEEDS_QUEUE_URL"), os.Getenv("ITEMS_QUEUE_URL")
	if queueURL == "" {
		panic("FEEDS_QUEUE_URL is required")
	}
	if itemsURL == "" {
		panic("ITEMS_QUEUE_URL is required")
	}
	signer, err := auth.NewCookieSigner(os.Getenv("CF_PRIVATE_KEY"), os.Getenv("CF_KEY_PAIR_ID"), time.Hour)
	if err != nil {
		panic(err)
	}
	googleClientID := strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID"))
	if googleClientID == "" {
		panic("GOOGLE_CLIENT_ID is required")
	}
	rescoreFunction := strings.TrimSpace(os.Getenv("RESCORE_FUNCTION_NAME"))
	if rescoreFunction == "" {
		panic("RESCORE_FUNCTION_NAME is required")
	}
	invoker := &lambdaInvoker{config: config, function: rescoreFunction, client: &http.Client{Timeout: 28 * time.Second}}
	discoveryHTTP := httpx.New(4*time.Second, 5<<20)
	vectorBucket, vectorIndex := strings.TrimSpace(os.Getenv("VECTOR_BUCKET")), strings.TrimSpace(os.Getenv("VECTOR_INDEX"))
	if vectorBucket == "" || vectorIndex == "" {
		panic("VECTOR_BUCKET and VECTOR_INDEX are required")
	}
	modelVersion := strings.TrimSpace(os.Getenv("MODEL_VERSION"))
	if modelVersion == "" {
		modelVersion = "amazon.titan-embed-text-v2:0"
	}
	imageModelVersion := strings.TrimSpace(os.Getenv("IMAGE_MODEL_VERSION"))
	if imageModelVersion == "" {
		imageModelVersion = titanimage.ModelID
	}
	imageSearchFloor := DefaultImageSearchFloor
	if raw := strings.TrimSpace(os.Getenv("IMAGE_SEARCH_MIN_SIMILARITY")); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || value < 0 || value > 100 {
			panic("IMAGE_SEARCH_MIN_SIMILARITY must be an integer from 0 to 100")
		}
		imageSearchFloor = value
	}
	runtime := bedrockruntime.NewFromConfig(config)
	vectorClient := s3vectors.NewFromConfig(config)
	s := &server{
		store: repository, sessions: auth.NewSessions(repository), verifyGoogle: func(ctx context.Context, credential string) (auth.Claims, error) {
			return auth.VerifyGoogle(ctx, credential, googleClientID)
		}, queue: sqs.NewFromConfig(config), feedsURL: queueURL, itemsURL: itemsURL, signer: signer,
		rescore: invoker.invokeRescore, discover: discovery.New(discoveryHTTP, !strings.EqualFold(strings.TrimSpace(os.Getenv("YOUTUBE_DISCOVERY_ENABLED")), "false")), media: media.New(httpx.New(8*time.Second, 10<<20)), feedCache: make(map[string]cachedFeedList),
		embedder: bedrockembed.NewWithModel(runtime, modelVersion),
		vectors:  vectorstore.NewS3(vectorClient, vectorBucket, vectorIndex), imageSearchFloor: imageSearchFloor, storyConfig: storycluster.FromEnv(),
	}
	if imageVectorIndex := strings.TrimSpace(os.Getenv("IMAGE_VECTOR_INDEX")); imageVectorIndex != "" {
		s.imageEmbedder = titanimage.NewWithModel(runtime, imageModelVersion)
		s.imageVectors = vectorstore.NewS3(vectorClient, vectorBucket, imageVectorIndex)
	}
	lambda.Start(s.handle)
}
