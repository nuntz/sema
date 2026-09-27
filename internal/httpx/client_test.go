package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestSafeIP(t *testing.T) {
	tests := map[string]bool{
		"8.8.8.8": true, "2606:4700:4700::1111": true,
		"127.0.0.1": false, "10.0.0.1": false, "169.254.169.254": false,
		"100.64.0.1": false, "192.0.2.1": false, "::1": false, "fc00::1": false,
	}
	for raw, want := range tests {
		if got := safeIP(netip.MustParseAddr(raw)); got != want {
			t.Errorf("safeIP(%s) = %v, want %v", raw, got, want)
		}
	}
}

func TestGetCallerHeadersReplaceDefaults(t *testing.T) {
	var accept []string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		accept = request.Header.Values("Accept")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    request,
		}, nil
	})
	client := &Client{http: &http.Client{Transport: transport}, maxBody: 1024, agent: DefaultUserAgent}

	_, err := client.Get(context.Background(), "https://example.com/image.jpg", http.Header{"Accept": []string{"image/*"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(accept) != 1 || accept[0] != "image/*" {
		t.Fatalf("Accept = %v, want [image/*]", accept)
	}
}

func TestPostReturnsRedirectsWithoutFollowingThem(t *testing.T) {
	var requests []string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		requests = append(requests, request.Method+" "+request.URL.String()+" "+string(body))
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"https://elsewhere.example/"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    request,
		}, nil
	})
	client := &Client{http: &http.Client{Transport: transport}, maxBody: 1024, agent: DefaultUserAgent}

	got, err := client.Post(context.Background(), "https://example.com/hook", http.Header{"Content-Type": []string{"application/json"}}, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.StatusCode != http.StatusFound || len(requests) != 1 || requests[0] != "POST https://example.com/hook {}" {
		t.Fatalf("status = %d, requests = %v", got.StatusCode, requests)
	}
}

func TestPostRefusesNonPublicAddressesWithATypedError(t *testing.T) {
	_, err := New(time.Second, 1024).Post(context.Background(), "https://127.0.0.1:9/hook", nil, []byte(`{}`))
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("err = %v, want ErrBlockedAddress", err)
	}
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failingBody) Close() error             { return nil }

func TestPostReportsTheStatusEvenWhenTheBodyCannotBeRead(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusAccepted, Header: make(http.Header), Body: failingBody{}, Request: request}, nil
	})
	client := &Client{http: &http.Client{Transport: transport}, maxBody: 1024, agent: DefaultUserAgent}

	got, err := client.Post(context.Background(), "https://example.com/hook", nil, []byte(`{}`))

	if err != nil || got.StatusCode != http.StatusAccepted {
		t.Fatalf("Post = %d, %v; want 202 without error", got.StatusCode, err)
	}
}
