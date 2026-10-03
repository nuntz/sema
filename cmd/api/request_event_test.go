package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/nuntz/sema/internal/auth"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/send"
	"github.com/nuntz/sema/internal/store"
)

// handleSignedIn signs a reader in, then handles one request as them and
// returns the response with the dimensions of the per-request event it emitted.
func handleSignedIn(t *testing.T, s *server, method, path string, query map[string]string) (events.APIGatewayV2HTTPResponse, map[string]string) {
	t.Helper()
	s.sessions = auth.NewSessions(&fakeSessions{})
	setCookie, err := s.sessions.Create(context.Background(), auth.Claims{Subject: "user", Email: "reader@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := http.ParseSetCookie(setCookie)
	if err != nil {
		t.Fatal(err)
	}
	var dimensions []map[string]string
	s.emit = func(_ map[string]float64, d map[string]string) { dimensions = append(dimensions, d) }
	request := apiRequest(method, path, "")
	request.QueryStringParameters = query
	request.Cookies = []string{auth.SessionCookieName + "=" + cookie.Value}
	got, err := s.handle(context.Background(), request)
	if err != nil || len(dimensions) != 1 {
		t.Fatalf("handle = %v, events = %v", err, dimensions)
	}
	return got, dimensions[0]
}

func TestSendRecordsItsOutcomeOnTheRequestEvent(t *testing.T) {
	for _, test := range []struct {
		name    string
		outcome send.Outcome
		want    map[string]string
	}{
		{"delivered", send.Outcome{Kind: send.Delivered, Status: 202}, map[string]string{"SendOutcome": "sent", "SendStatus": "202"}},
		{"queued", send.Outcome{Kind: send.Retryable, Status: 503}, map[string]string{"SendOutcome": "queued", "SendStatus": "503"}},
		{"failed", send.Outcome{Kind: send.Permanent, Reason: "blocked address"}, map[string]string{"SendOutcome": "failed", "SendReason": "blocked address"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, _, _, _ := sendItemServer(test.outcome)

			_, got := handleSignedIn(t, s, http.MethodPost, "/api/items/item/send", nil)

			for key, value := range test.want {
				if got[key] != value {
					t.Fatalf("dimensions = %v, want %s=%s", got, key, value)
				}
			}
		})
	}
}

func TestPingRecordsItsOutcomeOnTheRequestEvent(t *testing.T) {
	destinations := &fakeDestinationStore{destination: enabledDestination(), reserveOK: true}
	s, _ := destinationServer(destinations, &recordingAttempter{outcome: send.Outcome{Kind: send.Permanent, Status: 401}})

	_, got := handleSignedIn(t, s, http.MethodPost, "/api/destination/ping", nil)

	if got["SendOutcome"] != "failed" || got["SendStatus"] != "401" {
		t.Fatalf("dimensions = %v", got)
	}
}

func TestGetItemsRecordsTheQueryShapeOnTheRequestEvent(t *testing.T) {
	s := &server{store: &fakeAPIStore{
		feeds: func(context.Context, string) ([]domain.Feed, error) {
			return []domain.Feed{{FeedID: "feed"}}, nil
		},
		itemsForFeeds: func(context.Context, string, domain.Order, string, int, bool, bool, map[string]bool, map[string]bool, domain.FetchWindow, map[string]bool) ([]domain.Item, string, *domain.Item, error) {
			return []domain.Item{{ItemID: "a", FeedID: "feed"}, {ItemID: "b", FeedID: "feed"}}, "", nil, nil
		},
	}}
	query := map[string]string{"order": "interest", "feed": "feed"}

	_, first := handleSignedIn(t, s, http.MethodGet, "/api/items", query)
	_, second := handleSignedIn(t, s, http.MethodGet, "/api/items", query)

	want := map[string]string{"Order": "interest", "Filtered": "true", "ExcludeStories": "false", "ItemCount": "2", "ReadCache": "miss"}
	for key, value := range want {
		if first[key] != value {
			t.Fatalf("first dimensions = %v, want %s=%s", first, key, value)
		}
	}
	if second["ReadCache"] != "hit" {
		t.Fatalf("second dimensions = %v, want ReadCache=hit", second)
	}
}

func TestSendsThatStopEarlyRecordWhyOnTheRequestEvent(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*server, *fakeDestinationStore)
		path  string
		want  string
	}{
		{"rate limited send", func(_ *server, d *fakeDestinationStore) { d.reserveOK = false }, "/api/items/item/send", "rate_limited"},
		{"rate limited ping", func(_ *server, d *fakeDestinationStore) { d.reserveOK = false }, "/api/destination/ping", "rate_limited"},
		{"no destination", func(_ *server, d *fakeDestinationStore) { d.destination = nil }, "/api/items/item/send", "no_destination"},
		{"item not found", func(s *server, _ *fakeDestinationStore) {
			s.store.(*fakeAPIStore).item = func(context.Context, string, string) (domain.Item, error) { return domain.Item{}, store.ErrNotFound }
			s.store.(*fakeAPIStore).archiveItem = func(context.Context, string, string) (domain.Item, error) { return domain.Item{}, store.ErrNotFound }
		}, "/api/items/item/send", "item_not_found"},
		{"too large", func(s *server, _ *fakeDestinationStore) {
			s.store.(*fakeAPIStore).item = func(context.Context, string, string) (domain.Item, error) {
				return domain.Item{ItemID: "item", URL: "https://example.com/" + strings.Repeat("u", 70_000)}, nil
			}
		}, "/api/items/item/send", "too_large"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, destinations, attempter, _ := sendItemServer(send.Outcome{Kind: send.Delivered, Status: 202})
			test.setup(s, destinations)

			_, got := handleSignedIn(t, s, http.MethodPost, test.path, nil)

			if got["SendOutcome"] != test.want || len(attempter.deliveries) != 0 {
				t.Fatalf("dimensions = %v, attempts = %d, want SendOutcome=%s", got, len(attempter.deliveries), test.want)
			}
		})
	}
}

func TestSendThatCannotQueueItsRetryRecordsAnError(t *testing.T) {
	s, _, _, _ := sendItemServer(send.Outcome{Kind: send.Retryable, Status: 503})
	s.retries = failingRetries{}

	_, got := handleSignedIn(t, s, http.MethodPost, "/api/items/item/send", nil)

	if got["SendOutcome"] != "error" || got["SendStatus"] != "503" {
		t.Fatalf("dimensions = %v", got)
	}
}

type failingRetries struct{}

func (failingRetries) EnqueueRetry(context.Context, send.Message, time.Duration) error {
	return errors.New("queue unavailable")
}

func TestGetItemsThatFailEarlyStillRecordTheQueryShape(t *testing.T) {
	s := &server{store: &fakeAPIStore{
		feeds: func(context.Context, string) ([]domain.Feed, error) {
			return nil, errors.New("store unavailable")
		},
	}}

	got, dimensions := handleSignedIn(t, s, http.MethodGet, "/api/items", map[string]string{"order": "interest", "feed": "feed", "exclude_stories": "true"})

	if got.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", got.StatusCode, got.Body)
	}
	want := map[string]string{"Order": "interest", "Filtered": "true", "ExcludeStories": "true"}
	for key, value := range want {
		if dimensions[key] != value {
			t.Fatalf("dimensions = %v, want %s=%s", dimensions, key, value)
		}
	}
}
