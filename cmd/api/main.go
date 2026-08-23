package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/nuntz/sema/internal/auth"
	"github.com/nuntz/sema/internal/connector/rss"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
)

type queueAPI interface {
	SendMessageBatch(context.Context, *sqs.SendMessageBatchInput, ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error)
}

type server struct {
	store    *store.Store
	queue    queueAPI
	feedsURL string
	signer   *auth.CookieSigner
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
	claims, err := auth.FromRequest(request)
	if err != nil {
		return response(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}), nil
	}
	if err := s.store.EnsureUser(ctx, claims.Subject, claims.Email); err != nil {
		return s.failure("ensure user", err), nil
	}
	path := strings.TrimSuffix(request.RawPath, "/")
	path = strings.TrimPrefix(path, "/api")
	if path == "" {
		path = "/"
	}
	method := request.RequestContext.HTTP.Method

	var result events.APIGatewayV2HTTPResponse
	switch {
	case method == http.MethodGet && path == "/me":
		result = s.getMe(ctx, claims.Subject)
	case method == http.MethodPatch && path == "/me":
		result = s.patchMe(ctx, claims.Subject, request.Body)
	case method == http.MethodGet && path == "/items":
		result = s.getItems(ctx, claims.Subject, request.QueryStringParameters)
	case method == http.MethodPost && path == "/items/read-batch":
		result = s.readBatch(ctx, claims.Subject, request.Body)
	case method == http.MethodGet && path == "/archive":
		result = s.getArchive(ctx, claims.Subject, request.QueryStringParameters)
	case method == http.MethodGet && strings.HasPrefix(path, "/archive/"):
		result = s.getArchiveItem(ctx, claims.Subject, strings.TrimPrefix(path, "/archive/"))
	case strings.HasPrefix(path, "/items/"):
		result = s.itemRoute(ctx, claims.Subject, method, strings.TrimPrefix(path, "/items/"), request.Body)
	case method == http.MethodGet && path == "/feeds":
		result = s.getFeeds(ctx, claims.Subject)
	case method == http.MethodPost && path == "/feeds/import":
		result = s.importFeeds(ctx, claims.Subject, request)
	case method == http.MethodDelete && strings.HasPrefix(path, "/feeds/"):
		result = s.deleteFeed(ctx, claims.Subject, strings.TrimPrefix(path, "/feeds/"))
	default:
		result = response(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	if cookies, cookieErr := s.signer.Cookies(claims.Subject, time.Now().UTC()); cookieErr != nil {
		return s.failure("sign content cookies", cookieErr), nil
	} else {
		result.Cookies = cookies
	}
	return result, nil
}

func (s *server) getMe(ctx context.Context, userID string) events.APIGatewayV2HTTPResponse {
	user, err := s.store.User(ctx, userID)
	if err != nil {
		return s.failure("get profile", err)
	}
	return response(http.StatusOK, map[string]any{"profile": user, "signal_count": user.SignalCount, "heart_count": user.HeartCount})
}

func (s *server) patchMe(ctx context.Context, userID, body string) events.APIGatewayV2HTTPResponse {
	var input struct {
		OrderPref        *domain.Order `json:"order_pref"`
		InterestPosition *string       `json:"interest_position"`
	}
	if err := decodeJSON(body, &input); err != nil {
		return badRequest(err)
	}
	if input.OrderPref != nil && *input.OrderPref != domain.OrderChrono && *input.OrderPref != domain.OrderInterest {
		return badRequest(errors.New("order_pref must be chrono or interest"))
	}
	if err := s.store.UpdateUser(ctx, userID, input.OrderPref, input.InterestPosition); err != nil {
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
	items, next, err := s.store.Items(ctx, userID, order, query["cursor"], limit, includeRead)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return badRequest(err)
		}
		return s.failure("list items", err)
	}
	if err := s.prepareItems(ctx, userID, items); err != nil {
		return s.failure("prepare items", err)
	}
	return response(http.StatusOK, map[string]any{"items": items, "next_cursor": next})
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
	switch value {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("include_read must be true or false")
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
	default:
		return response(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	return response(http.StatusOK, map[string]bool{"ok": true})
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
	feeds, err := s.store.Feeds(ctx, userID)
	if err != nil {
		return s.failure("list feeds", err)
	}
	for i := range feeds {
		feeds[i].FaviconKey = s.store.ContentURL(feeds[i].FaviconKey)
	}
	return response(http.StatusOK, map[string]any{"feeds": feeds})
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
	now := time.Now().UTC()
	messages := make([]domain.FeedMessage, len(subscriptions))
	semaphore := make(chan struct{}, 20)
	errors := make(chan error, len(subscriptions))
	var group sync.WaitGroup
	for index, subscription := range subscriptions {
		group.Add(1)
		go func(index int, subscription rss.Subscription) {
			defer group.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			feedID := domain.FeedID(subscription.URL)
			feed := domain.Feed{
				PK: domain.UserPK(userID), SK: domain.FeedSK(feedID), FeedID: feedID, URL: subscription.URL,
				Title: subscription.Title, NextFetchAt: domain.Timestamp(now), LastStatus: "queued",
			}
			if existing, getErr := s.store.Feed(ctx, userID, feedID); getErr == nil {
				feed = existing
				feed.NextFetchAt = domain.Timestamp(now)
			}
			if err := s.store.PutFeed(ctx, feed); err != nil {
				errors <- err
				return
			}
			messages[index] = domain.FeedMessage{User: userID, FeedID: feedID}
		}(index, subscription)
	}
	group.Wait()
	close(errors)
	if err := <-errors; err != nil {
		return s.failure("store imported feed", err)
	}
	if err := s.enqueueFeeds(ctx, messages); err != nil {
		return s.failure("enqueue imported feeds", err)
	}
	return response(http.StatusAccepted, importFeedsResult{Imported: len(messages), Unsupported: unsupported})
}

func (s *server) enqueueFeeds(ctx context.Context, messages []domain.FeedMessage) error {
	errors := make(chan error, (len(messages)+9)/10)
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
				errors <- err
				return
			}
			if len(result.Failed) > 0 {
				errors <- fmt.Errorf("%d feeds failed to enqueue", len(result.Failed))
			}
		}(offset, end)
	}
	group.Wait()
	close(errors)
	return <-errors
}

func (s *server) deleteFeed(ctx context.Context, userID, feedID string) events.APIGatewayV2HTTPResponse {
	if feedID == "" {
		return badRequest(errors.New("feed id is required"))
	}
	if err := s.store.DeleteFeed(ctx, userID, feedID); err != nil {
		return s.failure("delete feed", err)
	}
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
	encoded, _ := json.Marshal(body)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: status, Body: string(encoded), Headers: map[string]string{"content-type": "application/json; charset=utf-8", "cache-control": "no-store"},
	}
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	repository, config, err := store.FromEnv(context.Background())
	if err != nil {
		panic(err)
	}
	queueURL := os.Getenv("FEEDS_QUEUE_URL")
	if queueURL == "" {
		panic("FEEDS_QUEUE_URL is required")
	}
	signer, err := auth.NewCookieSigner(os.Getenv("CF_PRIVATE_KEY"), os.Getenv("CF_KEY_PAIR_ID"), time.Hour)
	if err != nil {
		panic(err)
	}
	lambda.Start((&server{store: repository, queue: sqs.NewFromConfig(config), feedsURL: queueURL, signer: signer}).handle)
}
