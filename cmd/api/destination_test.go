package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/send"
	"github.com/nuntz/sema/internal/store"
)

type fakeDestinationStore struct {
	destination *domain.Destination
	recorded    []string
	reserveOK   bool
	reserved    int
}

func (f *fakeDestinationStore) Destination(context.Context, string) (domain.Destination, error) {
	if f.destination == nil {
		return domain.Destination{}, store.ErrNotFound
	}
	return *f.destination, nil
}

func (f *fakeDestinationStore) PutDestination(_ context.Context, userID string, destination domain.Destination) error {
	if f.destination != nil {
		destination.ReceiverUserID = f.destination.ReceiverUserID
		destination.LastStatus, destination.LastDeliveryAt = f.destination.LastStatus, f.destination.LastDeliveryAt
	}
	f.destination = &destination
	return nil
}

func (f *fakeDestinationStore) DeleteDestination(context.Context, string) error {
	f.destination = nil
	return nil
}

func (f *fakeDestinationStore) RecordDelivery(_ context.Context, _ string, _ time.Time, status string) error {
	f.recorded = append(f.recorded, status)
	return nil
}

func (f *fakeDestinationStore) ReserveSend(context.Context, string, time.Time, int) (bool, error) {
	f.reserved++
	return f.reserveOK, nil
}

type recordingAttempter struct {
	outcome    send.Outcome
	deliveries []send.Delivery
}

func (r *recordingAttempter) Attempt(_ context.Context, delivery send.Delivery) send.Outcome {
	r.deliveries = append(r.deliveries, delivery)
	return r.outcome
}

type recordingRetries struct{ messages []send.Message }

func (r *recordingRetries) EnqueueRetry(_ context.Context, message send.Message, _ time.Duration) error {
	r.messages = append(r.messages, message)
	return nil
}

const testSecret = "0123456789abcdef0123456789abcdef"

func destinationServer(destinations *fakeDestinationStore, attempter *recordingAttempter) (*server, *recordingRetries) {
	retries := &recordingRetries{}
	ids := 0
	return &server{
		store:        &fakeAPIStore{},
		destinations: destinations,
		sender:       attempter,
		retries:      retries,
		newID: func() string {
			ids++
			return []string{"", "id-1", "id-2", "id-3"}[ids]
		},
		now: func() time.Time { return time.Date(2026, 9, 26, 18, 4, 11, 0, time.UTC) },
	}, retries
}

func enabledDestination() *domain.Destination {
	return &domain.Destination{URL: "https://receiver.example/hook", Secret: testSecret, Label: "Save it", Enabled: true, ReceiverUserID: "receiver-user"}
}

func decodeBody(t *testing.T, body string) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return decoded
}

func TestGetDestinationNeverReturnsTheSecret(t *testing.T) {
	destinations := &fakeDestinationStore{destination: enabledDestination()}
	destinations.destination.LastStatus = "202"
	s, _ := destinationServer(destinations, &recordingAttempter{})

	got := s.destinationRoute(context.Background(), "user", http.MethodGet, "", "")

	if got.StatusCode != http.StatusOK || strings.Contains(got.Body, testSecret) {
		t.Fatalf("response = %d %s", got.StatusCode, got.Body)
	}
	body := decodeBody(t, got.Body)
	if body["url"] != "https://receiver.example/hook" || body["label"] != "Save it" || body["enabled"] != true || body["secret_set"] != true || body["last_status"] != "202" {
		t.Fatalf("body = %v", body)
	}
}

func TestGetDestinationIsNullWhenNoneIsSaved(t *testing.T) {
	s, _ := destinationServer(&fakeDestinationStore{}, &recordingAttempter{})

	got := s.destinationRoute(context.Background(), "user", http.MethodGet, "", "")

	if got.StatusCode != http.StatusOK || got.Body != "null" {
		t.Fatalf("response = %d %s", got.StatusCode, got.Body)
	}
}

func TestPutDestinationValidatesAndMintsTheReceiverUserID(t *testing.T) {
	destinations := &fakeDestinationStore{}
	s, _ := destinationServer(destinations, &recordingAttempter{})

	got := s.destinationRoute(context.Background(), "user", http.MethodPut, "", `{"url":" https://receiver.example/hook ","secret":"`+testSecret+`","label":"","enabled":true}`)

	if got.StatusCode != http.StatusOK || strings.Contains(got.Body, testSecret) {
		t.Fatalf("response = %d %s", got.StatusCode, got.Body)
	}
	saved := destinations.destination
	if saved == nil || saved.URL != "https://receiver.example/hook" || saved.Secret != testSecret || saved.Label != "Send" || !saved.Enabled || saved.ReceiverUserID != "id-1" {
		t.Fatalf("saved = %+v", saved)
	}
}

func TestPutDestinationKeepsTheSecretWhenOmitted(t *testing.T) {
	destinations := &fakeDestinationStore{destination: enabledDestination()}
	s, _ := destinationServer(destinations, &recordingAttempter{})

	got := s.destinationRoute(context.Background(), "user", http.MethodPut, "", `{"url":"https://receiver.example/new","label":"Keep","enabled":false}`)

	if got.StatusCode != http.StatusOK || destinations.destination.Secret != testSecret || destinations.destination.URL != "https://receiver.example/new" || destinations.destination.Enabled {
		t.Fatalf("response = %d %s, saved %+v", got.StatusCode, got.Body, destinations.destination)
	}
}

func TestPutDestinationRejectsInvalidSettings(t *testing.T) {
	for _, test := range []struct {
		name     string
		existing *domain.Destination
		body     string
	}{
		{name: "http url", body: `{"url":"http://receiver.example/hook","secret":"` + testSecret + `","label":"Send","enabled":true}`},
		{name: "userinfo url", body: `{"url":"https://user:pass@receiver.example/hook","secret":"` + testSecret + `","label":"Send","enabled":true}`},
		{name: "short secret", body: `{"url":"https://receiver.example/hook","secret":"short","label":"Send","enabled":true}`},
		{name: "long label", body: `{"url":"https://receiver.example/hook","secret":"` + testSecret + `","label":"` + strings.Repeat("x", 25) + `","enabled":true}`},
		{name: "missing secret for a new destination", body: `{"url":"https://receiver.example/hook","label":"Send","enabled":true}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			destinations := &fakeDestinationStore{destination: test.existing}
			s, _ := destinationServer(destinations, &recordingAttempter{})

			got := s.destinationRoute(context.Background(), "user", http.MethodPut, "", test.body)

			if got.StatusCode != http.StatusBadRequest || destinations.destination != nil {
				t.Fatalf("response = %d %s", got.StatusCode, got.Body)
			}
		})
	}
}

func TestDeleteDestinationRemovesIt(t *testing.T) {
	destinations := &fakeDestinationStore{destination: enabledDestination()}
	s, _ := destinationServer(destinations, &recordingAttempter{})

	got := s.destinationRoute(context.Background(), "user", http.MethodDelete, "", "")

	if got.StatusCode != http.StatusOK || destinations.destination != nil {
		t.Fatalf("response = %d %s", got.StatusCode, got.Body)
	}
}

func TestPingSendsASignedPingAndReportsTheStatus(t *testing.T) {
	destinations := &fakeDestinationStore{destination: enabledDestination(), reserveOK: true}
	attempter := &recordingAttempter{outcome: send.Outcome{Kind: send.Permanent, Status: 401}}
	s, retries := destinationServer(destinations, attempter)

	got := s.destinationRoute(context.Background(), "user", http.MethodPost, "ping", "")

	if got.StatusCode != http.StatusOK {
		t.Fatalf("response = %d %s", got.StatusCode, got.Body)
	}
	if body := decodeBody(t, got.Body); body["status"] != float64(401) || body["outcome"] != "failed" {
		t.Fatalf("body = %v", body)
	}
	delivery := attempter.deliveries[0]
	if delivery.Event != send.EventPing || delivery.Secret != testSecret || delivery.ID != "id-1" || string(delivery.Body) != `{"version":1,"event":"ping","sent_at":"2026-09-26T18:04:11Z","user_id":"receiver-user"}` {
		t.Fatalf("delivery = %+v", delivery)
	}
	if len(destinations.recorded) != 1 || destinations.recorded[0] != "401" || len(retries.messages) != 0 {
		t.Fatalf("recorded = %v, retries = %v", destinations.recorded, retries.messages)
	}
}

func TestPingNeverRetries(t *testing.T) {
	destinations := &fakeDestinationStore{destination: enabledDestination(), reserveOK: true}
	s, retries := destinationServer(destinations, &recordingAttempter{outcome: send.Outcome{Kind: send.Retryable, Reason: "timeout"}})

	got := s.destinationRoute(context.Background(), "user", http.MethodPost, "ping", "")

	if body := decodeBody(t, got.Body); body["reason"] != "timeout" || body["outcome"] != "failed" || len(retries.messages) != 0 {
		t.Fatalf("body = %v, retries = %v", body, retries.messages)
	}
}

func TestSendAndPingAreRateLimited(t *testing.T) {
	for _, route := range []string{"ping", "send"} {
		destinations := &fakeDestinationStore{destination: enabledDestination(), reserveOK: false}
		attempter := &recordingAttempter{}
		s, _ := destinationServer(destinations, attempter)
		s.store = &fakeAPIStore{item: func(context.Context, string, string) (domain.Item, error) {
			return domain.Item{ItemID: "item", URL: "https://example.com/a"}, nil
		}}

		var got int
		if route == "ping" {
			got = s.destinationRoute(context.Background(), "user", http.MethodPost, "ping", "").StatusCode
		} else {
			got = s.itemRoute(context.Background(), "user", http.MethodPost, "item/send", "").StatusCode
		}

		if got != http.StatusTooManyRequests || len(attempter.deliveries) != 0 {
			t.Fatalf("%s: status = %d, attempts = %d", route, got, len(attempter.deliveries))
		}
	}
}

func sendItemServer(outcome send.Outcome) (*server, *fakeDestinationStore, *recordingAttempter, *recordingRetries) {
	destinations := &fakeDestinationStore{destination: enabledDestination(), reserveOK: true}
	attempter := &recordingAttempter{outcome: outcome}
	s, retries := destinationServer(destinations, attempter)
	s.store = &fakeAPIStore{
		item: func(context.Context, string, string) (domain.Item, error) {
			return domain.Item{ItemID: "item", FeedID: "feed", Connector: "rss", URL: "https://example.com/a", Title: "A"}, nil
		},
		feed: func(context.Context, string, string) (domain.Feed, error) {
			return domain.Feed{FeedID: "feed", URL: "https://example.com/feed.xml", Title: "Example", Tags: []string{"news"}}, nil
		},
	}
	return s, destinations, attempter, retries
}

func TestSendDeliversTheItemInline(t *testing.T) {
	s, destinations, attempter, _ := sendItemServer(send.Outcome{Kind: send.Delivered, Status: 202})

	got := s.itemRoute(context.Background(), "user", http.MethodPost, "item/send", "")

	if got.StatusCode != http.StatusOK {
		t.Fatalf("response = %d %s", got.StatusCode, got.Body)
	}
	if body := decodeBody(t, got.Body); body["outcome"] != "sent" || body["status"] != float64(202) {
		t.Fatalf("body = %v", body)
	}
	want := `{"version":1,"event":"item.send","sent_at":"2026-09-26T18:04:11Z","user_id":"receiver-user","item":{"id":"item","url":"https://example.com/a","title":"A","feed":{"connector":"rss","title":"Example","url":"https://example.com/feed.xml"},"images":[],"kept":false,"tags":["news"]}}`
	delivery := attempter.deliveries[0]
	if string(delivery.Body) != want || delivery.Event != send.EventItemSend || delivery.ID != "id-1" || delivery.URL != "https://receiver.example/hook" {
		t.Fatalf("delivery = %+v\nbody %s", delivery, delivery.Body)
	}
	if destinations.reserved != 1 || destinations.recorded[0] != "202" {
		t.Fatalf("reserved = %d, recorded = %v", destinations.reserved, destinations.recorded)
	}
}

func TestSendQueuesARetryableFailure(t *testing.T) {
	s, _, _, retries := sendItemServer(send.Outcome{Kind: send.Retryable, Status: 503})

	got := s.itemRoute(context.Background(), "user", http.MethodPost, "item/send", "")

	if body := decodeBody(t, got.Body); got.StatusCode != http.StatusOK || body["outcome"] != "queued" || body["status"] != float64(503) {
		t.Fatalf("response = %d %v", got.StatusCode, body)
	}
	if len(retries.messages) != 1 || retries.messages[0].Attempts != 1 || retries.messages[0].User != "user" || retries.messages[0].DeliveryID != "id-1" {
		t.Fatalf("retries = %+v", retries.messages)
	}
}

func TestSendReportsAPermanentFailure(t *testing.T) {
	s, _, _, retries := sendItemServer(send.Outcome{Kind: send.Permanent, Reason: "blocked address"})

	got := s.itemRoute(context.Background(), "user", http.MethodPost, "item/send", "")

	if body := decodeBody(t, got.Body); got.StatusCode != http.StatusOK || body["outcome"] != "failed" || body["reason"] != "blocked address" || len(retries.messages) != 0 {
		t.Fatalf("response = %d %v", got.StatusCode, body)
	}
}

func TestSendNeedsAnEnabledDestination(t *testing.T) {
	for _, destination := range []*domain.Destination{nil, {URL: "https://receiver.example/hook", Secret: testSecret, Enabled: false}} {
		s, destinations, attempter, _ := sendItemServer(send.Outcome{})
		destinations.destination = destination

		got := s.itemRoute(context.Background(), "user", http.MethodPost, "item/send", "")

		if got.StatusCode != http.StatusConflict || len(attempter.deliveries) != 0 || destinations.reserved != 0 {
			t.Fatalf("response = %d %s", got.StatusCode, got.Body)
		}
	}
}

func TestSendFallsBackToTheArchiveRow(t *testing.T) {
	s, _, attempter, _ := sendItemServer(send.Outcome{Kind: send.Delivered, Status: 200})
	s.store.(*fakeAPIStore).item = func(context.Context, string, string) (domain.Item, error) { return domain.Item{}, store.ErrNotFound }
	s.store.(*fakeAPIStore).archiveItem = func(context.Context, string, string) (domain.Item, error) {
		return domain.Item{ItemID: "item", SK: "A#item", URL: "https://example.com/kept", HeartedTS: "2026-09-20T00:00:00.000000000Z"}, nil
	}

	got := s.itemRoute(context.Background(), "user", http.MethodPost, "item/send", "")

	if got.StatusCode != http.StatusOK || !strings.Contains(string(attempter.deliveries[0].Body), `"url":"https://example.com/kept"`) || !strings.Contains(string(attempter.deliveries[0].Body), `"kept":true`) {
		t.Fatalf("response = %d %s, body %s", got.StatusCode, got.Body, attempter.deliveries[0].Body)
	}
}

func TestMeCarriesTheSendLabelOnlyWhenEnabled(t *testing.T) {
	for _, test := range []struct {
		destination *domain.Destination
		want        any
	}{
		{destination: enabledDestination(), want: "Save it"},
		{destination: &domain.Destination{Label: "Off", Enabled: false}, want: nil},
		{destination: nil, want: nil},
	} {
		s, _ := destinationServer(&fakeDestinationStore{destination: test.destination}, &recordingAttempter{})
		s.store = &fakeAPIStore{user: func(context.Context, string) (domain.User, error) { return domain.User{}, nil }}

		got := s.getMe(context.Background(), "user")

		body := decodeBody(t, got.Body)
		if value, present := body["send_label"]; !present || value != test.want {
			t.Fatalf("send_label = %v (present %v), want %v", value, present, test.want)
		}
	}
}

type fakeURLSigner struct {
	resource string
	expires  time.Time
}

func (f *fakeURLSigner) SignedURL(resource string, expires time.Time) (string, error) {
	f.resource, f.expires = resource, expires
	return resource + "?Signature=sig&Key-Pair-Id=K1", nil
}

type fakeImageCopier struct {
	source, destination string
	missing             bool
}

func (f *fakeImageCopier) CopyForSend(_ context.Context, source, destination string) (bool, error) {
	f.source, f.destination = source, destination
	return !f.missing, nil
}

func TestSendAddsASignedNeutralCopyOfTheLargestStoredImage(t *testing.T) {
	s, _, attempter, _ := sendItemServer(send.Outcome{Kind: send.Delivered, Status: 202})
	signer, copier := &fakeURLSigner{}, &fakeImageCopier{}
	s.contentSigner, s.publicOrigin, s.imageCopier = signer, "https://sema.example", copier
	s.store.(*fakeAPIStore).item = func(context.Context, string, string) (domain.Item, error) {
		return domain.Item{
			ItemID: "item", URL: "https://www.reddit.com/r/x/comments/1/", MediaKey: "media/user/item/lead.jpg", MediaW: 1280, MediaH: 853,
			MediaVariants: []domain.MediaVariant{
				{Key: "media/user/item/lead-384.jpg", Width: 384, Height: 256},
				{Key: "media/user/item/lead.jpg", Width: 1280, Height: 853},
				{Key: "media/user/item/lead-768.jpg", Width: 768, Height: 512},
			},
		}, nil
	}

	s.itemRoute(context.Background(), "user", http.MethodPost, "item/send", "")

	// The copy's key carries no user identifier: it is a fresh id under send/.
	if copier.source != "media/user/item/lead.jpg" || copier.destination != "send/id-1.jpg" {
		t.Fatalf("copied %s to %s", copier.source, copier.destination)
	}
	if signer.resource != "https://sema.example/send/id-1.jpg" || !signer.expires.Equal(time.Date(2026, 9, 26, 19, 4, 11, 0, time.UTC)) {
		t.Fatalf("signed %s until %s", signer.resource, signer.expires)
	}
	want := `"images":[{"url":"https://sema.example/send/id-1.jpg?Signature=sig&Key-Pair-Id=K1","width":1280,"height":853,"role":"copy","expires_at":"2026-09-26T19:04:11Z"}]`
	body := string(attempter.deliveries[0].Body)
	if !strings.Contains(body, want) || strings.Contains(body, "/user/") {
		t.Fatalf("body = %s", body)
	}
}

func TestSendOmitsTheCopyWhenTheStoredImageIsGone(t *testing.T) {
	s, _, attempter, _ := sendItemServer(send.Outcome{Kind: send.Delivered, Status: 202})
	signer := &fakeURLSigner{}
	s.contentSigner, s.publicOrigin, s.imageCopier = signer, "https://sema.example", &fakeImageCopier{missing: true}
	s.store.(*fakeAPIStore).item = func(context.Context, string, string) (domain.Item, error) {
		return domain.Item{ItemID: "item", URL: "https://example.com/a", MediaKey: "media/user/item/lead.jpg"}, nil
	}

	s.itemRoute(context.Background(), "user", http.MethodPost, "item/send", "")

	if signer.resource != "" || !strings.Contains(string(attempter.deliveries[0].Body), `"images":[]`) {
		t.Fatalf("signed %q, body %s", signer.resource, attempter.deliveries[0].Body)
	}
}

func TestSendOmitsTheCopyWithoutStoredMediaOrAPublicOrigin(t *testing.T) {
	for _, origin := range []string{"", "https://sema.example"} {
		s, _, attempter, _ := sendItemServer(send.Outcome{Kind: send.Delivered, Status: 202})
		signer := &fakeURLSigner{}
		s.contentSigner, s.publicOrigin, s.imageCopier = signer, origin, &fakeImageCopier{}
		if origin == "" {
			s.store.(*fakeAPIStore).item = func(context.Context, string, string) (domain.Item, error) {
				return domain.Item{ItemID: "item", URL: "https://example.com/a", MediaKey: "media/user/item/lead.jpg"}, nil
			}
		}

		s.itemRoute(context.Background(), "user", http.MethodPost, "item/send", "")

		if signer.resource != "" || !strings.Contains(string(attempter.deliveries[0].Body), `"images":[]`) {
			t.Fatalf("origin %q: signed %q, body %s", origin, signer.resource, attempter.deliveries[0].Body)
		}
	}
}

func TestSendRefusesAnItemTooLargeToSend(t *testing.T) {
	s, destinations, attempter, _ := sendItemServer(send.Outcome{Kind: send.Delivered, Status: 202})
	s.store.(*fakeAPIStore).item = func(context.Context, string, string) (domain.Item, error) {
		return domain.Item{ItemID: "item", URL: "https://example.com/" + strings.Repeat("u", 70_000)}, nil
	}

	got := s.itemRoute(context.Background(), "user", http.MethodPost, "item/send", "")

	if got.StatusCode != http.StatusUnprocessableEntity || len(attempter.deliveries) != 0 || len(destinations.recorded) != 0 {
		t.Fatalf("response = %d %s", got.StatusCode, got.Body)
	}
}
