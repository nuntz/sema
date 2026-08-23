package rss

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/mmcdole/gofeed"
	"github.com/nuntz/sema/internal/connector"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/httpx"
)

type fakeFetcher struct {
	response httpx.Response
	err      error
	check    func(http.Header)
}

func (f fakeFetcher) Get(_ context.Context, _ string, headers http.Header) (httpx.Response, error) {
	if f.check != nil {
		f.check(headers)
	}
	return f.response, f.err
}

func connectorFor(body, contentType string) *Connector {
	base, _ := url.Parse("https://example.com/feed")
	headers := make(http.Header)
	headers.Set("Content-Type", contentType)
	headers.Set("ETag", `"v1"`)
	return &Connector{client: fakeFetcher{response: httpx.Response{StatusCode: http.StatusOK, Header: headers, Body: []byte(body), FinalURL: base}}, parser: gofeed.NewParser()}
}

func TestFetchFeedFormats(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantTitle   string
		wantURL     string
		wantImage   bool
	}{
		{
			name: "RSS 2.0 with CDATA and enclosure", contentType: "application/rss+xml",
			body:      `<?xml version="1.0"?><rss version="2.0"><channel><title>RSS Feed</title><link>/site</link><item><guid>one</guid><title><![CDATA[RSS title]]></title><link>/rss-item</link><description><![CDATA[<p>Summary</p>]]></description><pubDate>Thu, 20 Aug 2026 12:00:00 GMT</pubDate><enclosure url="/lead.jpg" type="image/jpeg" length="12"/></item></channel></rss>`,
			wantTitle: "RSS title", wantURL: "/rss-item", wantImage: true,
		},
		{
			name: "Atom", contentType: "application/atom+xml",
			body:      `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Atom Feed</title><link href="/site"/><entry><id>two</id><title>Atom title</title><link href="/atom-item"/><updated>2026-08-20T12:00:00Z</updated><summary>Summary</summary></entry></feed>`,
			wantTitle: "Atom title", wantURL: "/atom-item",
		},
		{
			name: "JSON Feed", contentType: "application/feed+json",
			body:      `{"version":"https://jsonfeed.org/version/1.1","title":"JSON Feed","home_page_url":"/site","items":[{"id":"three","url":"/json-item","title":"JSON title","date_published":"2026-08-20T12:00:00Z","summary":"Summary"}]}`,
			wantTitle: "JSON title", wantURL: "/json-item",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := connectorFor(tt.body, tt.contentType).Fetch(context.Background(), domain.Feed{URL: "https://example.com/feed"})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Entries) != 1 {
				t.Fatalf("got %d entries", len(result.Entries))
			}
			entry := result.Entries[0]
			if entry.Title != tt.wantTitle || entry.URL != "https://example.com"+tt.wantURL {
				t.Fatalf("entry = %#v", entry)
			}
			if entry.DisplayDate != "2026-08-20T12:00:00.000000000Z" {
				t.Fatalf("display date = %q", entry.DisplayDate)
			}
			if (len(entry.Enclosures) > 0) != tt.wantImage {
				t.Fatalf("enclosures = %#v", entry.Enclosures)
			}
			if result.ETag != `"v1"` {
				t.Fatalf("etag = %q", result.ETag)
			}
		})
	}
}

func TestFetchLeavesDisplayDateEmptyWhenFeedHasNoDate(t *testing.T) {
	body := `<?xml version="1.0"?><rss version="2.0"><channel><title>RSS Feed</title><link>/site</link><item><guid>one</guid><title>Undated</title><link>/item</link></item></channel></rss>`
	result, err := connectorFor(body, "application/rss+xml").Fetch(context.Background(), domain.Feed{URL: "https://example.com/feed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 || result.Entries[0].DisplayDate != "" {
		t.Fatalf("entry = %#v", result.Entries)
	}
}

func TestConditionalFetch(t *testing.T) {
	connector := &Connector{client: fakeFetcher{
		response: httpx.Response{StatusCode: http.StatusNotModified, Header: make(http.Header)},
		check: func(headers http.Header) {
			if got := headers.Get("If-None-Match"); got != `"old"` {
				t.Errorf("If-None-Match = %q", got)
			}
			if got := headers.Get("If-Modified-Since"); got != "yesterday" {
				t.Errorf("If-Modified-Since = %q", got)
			}
		}}, parser: gofeed.NewParser()}
	result, err := connector.Fetch(context.Background(), domain.Feed{URL: "https://example.com/feed", ETag: `"old"`, LastModified: "yesterday"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.NotModified {
		t.Fatal("expected a not-modified result")
	}
}

func TestFetchPreservesHTTPStatusMetadata(t *testing.T) {
	headers := make(http.Header)
	headers.Set("Retry-After", "120")
	feedConnector := &Connector{client: fakeFetcher{
		response: httpx.Response{StatusCode: http.StatusTooManyRequests, Header: headers},
	}, parser: gofeed.NewParser()}

	_, err := feedConnector.Fetch(context.Background(), domain.Feed{URL: "https://example.com/feed"})
	var statusErr *connector.HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error = %v, want HTTPStatusError", err)
	}
	if statusErr.StatusCode != http.StatusTooManyRequests || statusErr.Header.Get("Retry-After") != "120" || statusErr.Error() != "feed returned HTTP 429" {
		t.Fatalf("status error = %#v", statusErr)
	}
}

func TestFetchMediaRSSEnclosures(t *testing.T) {
	body := `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom" xmlns:media="http://search.yahoo.com/mrss/"><title>Reddit</title><link href="/site"/><entry><id>reddit-one</id><title>Reddit post</title><link href="/comments/one"/><updated>2026-08-20T12:00:00Z</updated><media:thumbnail url="/preview.jpeg?width=640"/><media:group><media:content url="https://cdn.example.com/full.webp" medium="image" fileSize="42"/></media:group></entry></feed>`
	result, err := connectorFor(body, "application/atom+xml").Fetch(context.Background(), domain.Feed{URL: "https://example.com/feed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("entries = %#v", result.Entries)
	}
	enclosures := result.Entries[0].Enclosures
	if len(enclosures) != 2 {
		t.Fatalf("enclosures = %#v", enclosures)
	}
	if enclosures[0].URL != "https://example.com/preview.jpeg?width=640" || enclosures[0].Type != "image/*" {
		t.Fatalf("thumbnail = %#v", enclosures[0])
	}
	if enclosures[1].URL != "https://cdn.example.com/full.webp" || enclosures[1].Type != "image/*" || enclosures[1].Length != "42" {
		t.Fatalf("content = %#v", enclosures[1])
	}
}

func TestFetchRejectsInvalidFeed(t *testing.T) {
	if _, err := connectorFor("this is not a feed", "text/plain").Fetch(context.Background(), domain.Feed{URL: "https://example.com/feed"}); err == nil {
		t.Fatal("expected a parse error")
	}
}
