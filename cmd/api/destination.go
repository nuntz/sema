package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/send"
	"github.com/nuntz/sema/internal/store"
)

const (
	sendsPerHour       = 60
	defaultSendLabel   = "Send"
	maxSendLabelRunes  = 24
	minDestinationKey  = 32
	maxDestinationKey  = 256
	sendOutcomeSent    = "sent"
	sendOutcomeQueued  = "queued"
	sendOutcomeFailed  = "failed"
	errorNoDestination = "no destination"
)

type destinationStore interface {
	Destination(ctx context.Context, userID string) (domain.Destination, error)
	PutDestination(ctx context.Context, userID string, destination domain.Destination) error
	DeleteDestination(ctx context.Context, userID string) error
	RecordDelivery(ctx context.Context, userID string, at time.Time, status string) error
	ReserveSend(ctx context.Context, userID string, now time.Time, limit int) (bool, error)
}

var _ destinationStore = (*store.Store)(nil)

type urlSigner interface {
	SignedURL(resource string, expires time.Time) (string, error)
}

type imageCopier interface {
	CopyForSend(ctx context.Context, source, destination string) (bool, error)
}

var _ imageCopier = (*store.Store)(nil)

// copyLifetime covers the inline attempt, every retry, and a receiver that
// fetches the image asynchronously.
const copyLifetime = time.Hour

// destinationView is the settings shape; the secret is write-only.
type destinationView struct {
	URL            string `json:"url"`
	Label          string `json:"label"`
	Enabled        bool   `json:"enabled"`
	SecretSet      bool   `json:"secret_set"`
	LastDeliveryAt string `json:"last_delivery_at,omitempty"`
	LastStatus     string `json:"last_status,omitempty"`
}

func viewDestination(destination domain.Destination) destinationView {
	return destinationView{
		URL: destination.URL, Label: destination.Label, Enabled: destination.Enabled, SecretSet: destination.Secret != "",
		LastDeliveryAt: destination.LastDeliveryAt, LastStatus: destination.LastStatus,
	}
}

func (s *server) destinationRoute(ctx context.Context, userID, method, suffix, body string) events.APIGatewayV2HTTPResponse {
	switch {
	case suffix == "" && method == http.MethodGet:
		destination, err := s.destinations.Destination(ctx, userID)
		if errors.Is(err, store.ErrNotFound) {
			return response(http.StatusOK, nil)
		}
		if err != nil {
			return s.failure("get destination", err)
		}
		return response(http.StatusOK, viewDestination(destination))
	case suffix == "" && method == http.MethodPut:
		return s.putDestination(ctx, userID, body)
	case suffix == "" && method == http.MethodDelete:
		if err := s.destinations.DeleteDestination(ctx, userID); err != nil {
			return s.failure("delete destination", err)
		}
		return response(http.StatusOK, map[string]bool{"ok": true})
	case suffix == "ping" && method == http.MethodPost:
		return s.pingDestination(ctx, userID)
	default:
		return response(http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func (s *server) putDestination(ctx context.Context, userID, body string) events.APIGatewayV2HTTPResponse {
	var input struct {
		URL     string  `json:"url"`
		Secret  *string `json:"secret"`
		Label   string  `json:"label"`
		Enabled bool    `json:"enabled"`
	}
	if err := decodeJSON(body, &input); err != nil {
		return badRequest(err)
	}
	destination := domain.Destination{URL: strings.TrimSpace(input.URL), Label: strings.TrimSpace(input.Label), Enabled: input.Enabled}
	if !send.ValidURL(destination.URL) {
		return badRequest(errors.New("url must be an https URL without credentials"))
	}
	if destination.Label == "" {
		destination.Label = defaultSendLabel
	}
	if len([]rune(destination.Label)) > maxSendLabelRunes {
		return badRequest(errors.New("label must be at most 24 characters"))
	}
	existing, err := s.destinations.Destination(ctx, userID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return s.failure("get destination", err)
	}
	if input.Secret != nil {
		destination.Secret = *input.Secret
	} else {
		destination.Secret = existing.Secret
	}
	if length := len(destination.Secret); length < minDestinationKey || length > maxDestinationKey {
		return badRequest(errors.New("secret must be 32 to 256 characters"))
	}
	destination.ReceiverUserID = existing.ReceiverUserID
	if destination.ReceiverUserID == "" {
		destination.ReceiverUserID = s.newID()
	}
	if err := s.destinations.PutDestination(ctx, userID, destination); err != nil {
		return s.failure("put destination", err)
	}
	destination.LastDeliveryAt, destination.LastStatus = existing.LastDeliveryAt, existing.LastStatus
	return response(http.StatusOK, viewDestination(destination))
}

// reserveSend loads the Destination and spends one unit of the hourly
// allowance, returning a ready response when the request cannot proceed.
func (s *server) reserveSend(ctx context.Context, userID string, requireEnabled bool) (domain.Destination, *events.APIGatewayV2HTTPResponse) {
	destination, err := s.destinations.Destination(ctx, userID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && requireEnabled && !destination.Enabled) {
		result := response(http.StatusConflict, map[string]string{"error": errorNoDestination})
		return destination, &result
	}
	if err != nil {
		result := s.failure("get destination", err)
		return destination, &result
	}
	allowed, err := s.destinations.ReserveSend(ctx, userID, s.now(), sendsPerHour)
	if err != nil {
		result := s.failure("reserve send", err)
		return destination, &result
	}
	if !allowed {
		result := response(http.StatusTooManyRequests, map[string]string{"error": "too many sends"})
		return destination, &result
	}
	return destination, nil
}

func (s *server) pingDestination(ctx context.Context, userID string) events.APIGatewayV2HTTPResponse {
	destination, blocked := s.reserveSend(ctx, userID, false)
	if blocked != nil {
		return *blocked
	}
	body, err := send.PingPayload(destination.ReceiverUserID, s.now())
	if err != nil {
		return s.failure("build ping", err)
	}
	outcome := s.sender.Attempt(ctx, send.Delivery{ID: s.newID(), Event: send.EventPing, URL: destination.URL, Secret: destination.Secret, Body: body})
	if err := s.destinations.RecordDelivery(ctx, userID, s.now(), outcome.Label()); err != nil {
		slog.WarnContext(ctx, "record ping status failed", "user", userID, "error", err)
	}
	return response(http.StatusOK, sendResult(outcome, false))
}

func (s *server) sendItem(ctx context.Context, userID, itemID string) events.APIGatewayV2HTTPResponse {
	item, err := s.store.Item(ctx, userID, itemID)
	live := err == nil
	if errors.Is(err, store.ErrNotFound) {
		item, err = s.store.ArchiveItem(ctx, userID, itemID)
	}
	if errors.Is(err, store.ErrNotFound) {
		return response(http.StatusNotFound, map[string]string{"error": "item not found"})
	}
	if err != nil {
		return s.failure("get item for send", err)
	}
	destination, blocked := s.reserveSend(ctx, userID, true)
	if blocked != nil {
		return *blocked
	}
	feed, err := s.store.Feed(ctx, userID, item.FeedID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return s.failure("get feed for send", err)
	}
	body, err := send.ItemPayload(item, feed, destination.ReceiverUserID, s.now(), s.copyImage(ctx, item))
	if err != nil {
		return response(http.StatusUnprocessableEntity, map[string]string{"error": "item is too large to send"})
	}
	deliverer := &send.Deliverer{Destinations: s.destinations, Sender: s.sender, Queue: s.retries, Now: s.now}
	result, err := deliverer.Deliver(ctx, send.Message{User: userID, DeliveryID: s.newID(), Event: send.EventItemSend, URL: destination.URL, Body: body})
	if err != nil {
		slog.ErrorContext(ctx, "send delivery failed", "user", userID, "item_id", itemID, "error", err)
	}
	if live && len(item.Vector) > 0 {
		if behaviourErr := s.store.RecordBehaviour(ctx, userID, item, store.BehaviourEvent{Shared: true}); behaviourErr != nil {
			slog.WarnContext(ctx, "record send behaviour failed", "user", userID, "item_id", itemID, "error", behaviourErr)
		}
	}
	return response(http.StatusOK, sendResult(result.Outcome, result.Queued))
}

// copyImage signs a short-lived URL to the largest stored copy of the lead
// image, so receivers are not left to fetch publisher pages that block them.
// Stored keys embed the user's identity, so the image is first copied to a
// fresh key under send/ and only that key leaves Sema.
func (s *server) copyImage(ctx context.Context, item domain.Item) *send.CopyImage {
	if s.contentSigner == nil || s.imageCopier == nil || s.publicOrigin == "" || item.MediaKey == "" {
		return nil
	}
	chosen := domain.MediaVariant{Key: item.MediaKey, Width: item.MediaW, Height: item.MediaH}
	for _, variant := range item.MediaVariants {
		if variant.Width > chosen.Width || (chosen.Width == 0 && variant.Key == item.MediaKey) {
			chosen = variant
		}
	}
	neutralKey := store.SendCopyKey(s.newID(), path.Ext(chosen.Key))
	copied, err := s.imageCopier.CopyForSend(ctx, chosen.Key, neutralKey)
	if err != nil || !copied {
		if err != nil {
			slog.WarnContext(ctx, "copy send image failed", "item_id", item.ItemID, "error", err)
		}
		return nil
	}
	expires := s.now().Add(copyLifetime)
	signed, err := s.contentSigner.SignedURL(strings.TrimRight(s.publicOrigin, "/")+"/"+neutralKey, expires)
	if err != nil {
		slog.WarnContext(ctx, "sign send image copy failed", "item_id", item.ItemID, "error", err)
		return nil
	}
	return &send.CopyImage{URL: signed, Width: chosen.Width, Height: chosen.Height, ExpiresAt: expires}
}

func sendResult(outcome send.Outcome, queued bool) map[string]any {
	result := map[string]any{"outcome": sendOutcomeFailed}
	switch {
	case outcome.Kind == send.Delivered:
		result["outcome"] = sendOutcomeSent
	case queued:
		result["outcome"] = sendOutcomeQueued
	}
	if outcome.Status != 0 {
		result["status"] = outcome.Status
	}
	if outcome.Reason != "" {
		result["reason"] = outcome.Reason
	}
	return result
}

// sendLabel names the Send button, or is nil when Send is unavailable.
func (s *server) sendLabel(ctx context.Context, userID string) *string {
	if s.destinations == nil {
		return nil
	}
	destination, err := s.destinations.Destination(ctx, userID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.WarnContext(ctx, "load destination for profile failed", "user", userID, "error", err)
		}
		return nil
	}
	if !destination.Enabled {
		return nil
	}
	return &destination.Label
}
