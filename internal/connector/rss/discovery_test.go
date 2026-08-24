package rss

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mmcdole/gofeed"
	"github.com/nuntz/sema/internal/httpx"
)

type discoveryFetcher struct {
	responses map[string]httpx.Response
	requests  []string
}

func (f *discoveryFetcher) Get(_ context.Context, rawURL string, _ http.Header) (httpx.Response, error) {
	f.requests = append(f.requests, rawURL)
	response, ok := f.responses[rawURL]
	if !ok {
		return httpx.Response{}, fmt.Errorf("not found")
	}
	if response.FinalURL == nil {
		response.FinalURL, _ = url.Parse(rawURL)
	}
	if response.StatusCode == 0 {
		response.StatusCode = http.StatusOK
	}
	return response, nil
}

const discoveryRSS = `<?xml version="1.0"?><rss version="2.0"><channel><title>Example feed</title><link>https://example.com</link><item><guid>one</guid><title>Newest</title><pubDate>Sun, 23 Aug 2026 12:00:00 GMT</pubDate></item></channel></rss>`
const discoveryAtom = `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Atom feed</title><entry><id>one</id><title>Entry</title><updated>2026-08-22T12:00:00Z</updated></entry></feed>`

func TestDiscoverDirectFeed(t *testing.T) {
	fetcher := &discoveryFetcher{responses: map[string]httpx.Response{"https://example.com/rss.xml": {Body: []byte(discoveryRSS)}}}
	candidates, err := (&Discoverer{client: fetcher, parser: newParser()}).Discover(context.Background(), "example.com/rss.xml")
	if err != nil || len(candidates) != 1 || candidates[0].FeedURL != "https://example.com/rss.xml" || candidates[0].Type != "rss" || candidates[0].ItemCount != 1 || candidates[0].NewestItemTS == "" {
		t.Fatalf("candidates = %#v, err = %v", candidates, err)
	}
}

func TestDiscoverAlternateLinksResolveRelativeAndReturnRealMetadata(t *testing.T) {
	page := `<html><head><link rel="stylesheet alternate" type="application/rss+xml" href="/rss.xml"><link rel="alternate" type="application/atom+xml; charset=utf-8" href="feeds/atom.xml"></head></html>`
	fetcher := &discoveryFetcher{responses: map[string]httpx.Response{
		"https://example.com/blog/":               {Body: []byte(page)},
		"https://example.com/rss.xml":             {Body: []byte(discoveryRSS)},
		"https://example.com/blog/feeds/atom.xml": {Body: []byte(discoveryAtom)},
	}}
	candidates, err := (&Discoverer{client: fetcher, parser: newParser()}).Discover(context.Background(), "https://example.com/blog/")
	if err != nil || len(candidates) != 2 || candidates[0].Title != "Example feed" || candidates[1].Type != "atom" {
		t.Fatalf("candidates = %#v, err = %v", candidates, err)
	}
}

func TestDiscoverProbesCommonPathsUntilTwoHits(t *testing.T) {
	fetcher := &discoveryFetcher{responses: map[string]httpx.Response{
		"https://example.com/":        {Body: []byte(`<html><head><title>Site</title></head></html>`)},
		"https://example.com/feed":    {Body: []byte(discoveryRSS)},
		"https://example.com/rss.xml": {Body: []byte(discoveryAtom)},
	}}
	candidates, err := (&Discoverer{client: fetcher, parser: newParser()}).Discover(context.Background(), "https://example.com/")
	if err != nil || len(candidates) != 2 {
		t.Fatalf("candidates = %#v, err = %v", candidates, err)
	}
	if strings.Contains(strings.Join(fetcher.requests, " "), "/atom.xml") {
		t.Fatalf("probes did not stop after two hits: %#v", fetcher.requests)
	}
}

func TestDiscoverDeadSiteReturnsRequestError(t *testing.T) {
	fetcher := &discoveryFetcher{responses: map[string]httpx.Response{}}
	_, err := (&Discoverer{client: fetcher, parser: newParser()}).Discover(context.Background(), "https://dead.example")
	if err == nil || !strings.Contains(err.Error(), "GET https://dead.example") {
		t.Fatalf("error = %v", err)
	}
}

func newParser() *gofeed.Parser { return gofeed.NewParser() }
