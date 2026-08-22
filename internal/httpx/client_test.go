package httpx

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
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
