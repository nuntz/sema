package send

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/httpx"
)

type fakePoster struct {
	url     string
	headers http.Header
	body    []byte
	status  int
	err     error
}

func (f *fakePoster) Post(_ context.Context, rawURL string, headers http.Header, body []byte) (httpx.Response, error) {
	f.url, f.headers, f.body = rawURL, headers, body
	return httpx.Response{StatusCode: f.status}, f.err
}

var attemptNow = time.Unix(1790445851, 0)

func testDelivery() Delivery {
	return Delivery{ID: "0b9f1a52-6a4e-4e38-9d3c-3f1f7a0de0a1", Event: EventPing, URL: "https://receiver.example/hook", Secret: "test-secret-0123456789abcdef0123456789", Body: []byte(`{"version":1,"event":"ping"}`)}
}

func TestAttemptSendsTheSignedContractHeaders(t *testing.T) {
	poster := &fakePoster{status: http.StatusAccepted}
	sender := &Sender{Poster: poster, Now: func() time.Time { return attemptNow }}

	sender.Attempt(context.Background(), testDelivery())

	want := map[string]string{
		"Content-Type":     "application/json",
		"User-Agent":       "Sema-Webhook/1",
		"X-Sema-Event":     "ping",
		"X-Sema-Delivery":  "0b9f1a52-6a4e-4e38-9d3c-3f1f7a0de0a1",
		"X-Sema-Timestamp": "1790445851",
		// Same openssl reference as TestSignMatchesTheDocumentedHMAC.
		"X-Sema-Signature": "v1=cb1b0fc444a721869e589679fc9901efefc4d55f9c526871a94ad2bf2a5b6364",
	}
	for name, value := range want {
		if got := poster.headers.Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	if poster.url != "https://receiver.example/hook" || string(poster.body) != `{"version":1,"event":"ping"}` {
		t.Fatalf("posted %s %s", poster.url, poster.body)
	}
}

func TestAttemptClassifiesResponses(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		err    error
		want   Outcome
	}{
		{name: "2xx delivers", status: 202, want: Outcome{Kind: Delivered, Status: 202}},
		{name: "429 retries", status: 429, want: Outcome{Kind: Retryable, Status: 429}},
		{name: "5xx retries", status: 503, want: Outcome{Kind: Retryable, Status: 503}},
		{name: "4xx is permanent", status: 401, want: Outcome{Kind: Permanent, Status: 401}},
		{name: "redirect is permanent", status: 302, want: Outcome{Kind: Permanent, Status: 302}},
		{name: "timeout retries", err: fmt.Errorf("post: %w", context.DeadlineExceeded), want: Outcome{Kind: Retryable, Reason: "timeout"}},
		{name: "network error retries", err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}, want: Outcome{Kind: Retryable, Reason: "network"}},
		{name: "dns failure retries", err: &net.DNSError{Err: "no such host", Name: "receiver.example"}, want: Outcome{Kind: Retryable, Reason: "dns"}},
		{name: "tls failure is permanent", err: &tls.CertificateVerificationError{Err: errors.New("unknown authority")}, want: Outcome{Kind: Permanent, Reason: "tls"}},
		{name: "blocked address is permanent", err: fmt.Errorf("dial: %w", httpx.ErrBlockedAddress), want: Outcome{Kind: Permanent, Reason: "blocked address"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			sender := &Sender{Poster: &fakePoster{status: test.status, err: test.err}, Now: func() time.Time { return attemptNow }}
			if got := sender.Attempt(context.Background(), testDelivery()); got != test.want {
				t.Fatalf("outcome = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestAttemptRefusesNonHTTPSDestinations(t *testing.T) {
	poster := &fakePoster{status: 200}
	delivery := testDelivery()
	delivery.URL = "http://receiver.example/hook"

	got := (&Sender{Poster: poster, Now: func() time.Time { return attemptNow }}).Attempt(context.Background(), delivery)

	if got != (Outcome{Kind: Permanent, Reason: "invalid url"}) || poster.url != "" {
		t.Fatalf("outcome = %+v, posted to %q", got, poster.url)
	}
}

func TestAttemptThroughTheGuardedClientRefusesLoopback(t *testing.T) {
	delivery := testDelivery()
	delivery.URL = "https://127.0.0.1:9/hook"

	got := NewSender().Attempt(context.Background(), delivery)

	if got != (Outcome{Kind: Permanent, Reason: "blocked address"}) {
		t.Fatalf("outcome = %+v", got)
	}
}

func TestOutcomeLabel(t *testing.T) {
	if got := (Outcome{Kind: Delivered, Status: 202}).Label(); got != "202" {
		t.Fatalf("label = %q", got)
	}
	if got := (Outcome{Kind: Retryable, Reason: "timeout"}).Label(); got != "timeout" {
		t.Fatalf("label = %q", got)
	}
}
