package youtube

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/httpx"
)

type fakeFetcher struct {
	responses map[string]httpx.Response
	requests  []string
	headers   http.Header
}

func (f *fakeFetcher) Get(_ context.Context, rawURL string, headers http.Header) (httpx.Response, error) {
	f.requests = append(f.requests, rawURL)
	f.headers = headers.Clone()
	response, ok := f.responses[rawURL]
	if !ok {
		return httpx.Response{}, errors.New("not found")
	}
	if response.StatusCode == 0 {
		response.StatusCode = http.StatusOK
	}
	if response.FinalURL == nil {
		response.FinalURL, _ = url.Parse(rawURL)
	}
	return response, nil
}

func fixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/uploads.xml")
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestConnectorParsesNamespacedUploadsAndMapsVideoFields(t *testing.T) {
	feedURL := feedBase + "UC123"
	fetcher := &fakeFetcher{responses: map[string]httpx.Response{feedURL: {
		Body: fixture(t), Header: http.Header{"Etag": []string{`"uploads"`}, "Last-Modified": []string{"Sun, 23 Aug 2026 12:00:00 GMT"}},
	}}}
	result, err := (&Connector{client: fetcher}).Fetch(context.Background(), domain.Feed{URL: feedURL, ETag: `"old"`})
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "Sema Channel" || result.SiteURL != "https://www.youtube.com/channel/UC123" || len(result.Entries) != 3 {
		t.Fatalf("result = %#v", result)
	}
	entry := result.Entries[0]
	if entry.VideoID != "regular1" || entry.URL != watchBase+"regular1" || entry.Title != "Regular upload" || !strings.Contains(entry.ContentRaw, "Full description") || len(entry.Enclosures) != 1 {
		t.Fatalf("entry = %#v", entry)
	}
	if result.Entries[1].ContentRaw != "" || fetcher.headers.Get("If-None-Match") != `"old"` || result.ETag != `"uploads"` {
		t.Fatalf("conditional fetch = headers %#v result %#v", fetcher.headers, result)
	}
}

func TestConnectorHandlesNotModified(t *testing.T) {
	feedURL := feedBase + "UC123"
	fetcher := &fakeFetcher{responses: map[string]httpx.Response{feedURL: {StatusCode: http.StatusNotModified}}}
	result, err := (&Connector{client: fetcher}).Fetch(context.Background(), domain.Feed{URL: feedURL, ETag: "etag", LastModified: "modified"})
	if err != nil || !result.NotModified || result.ETag != "etag" || result.Modified != "modified" {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
}

func TestDiscoveryResolvesEveryYouTubeFormToChannelCandidate(t *testing.T) {
	feedURL := feedBase + "UC123"
	page := []byte(`<html><head><meta itemprop="channelId" content="UC123"><meta property="og:title" content="Sema Channel - YouTube"><meta property="og:image" content="https://yt3.example/avatar.jpg"></head></html>`)
	forms := []string{
		"youtube.com/channel/UC123", "youtube.com/@sema", "youtube.com/c/sema", "youtube.com/user/sema",
		"youtu.be/regular1", "youtube.com/watch?v=regular1", "youtube.com/shorts/short3",
	}
	for _, raw := range forms {
		t.Run(raw, func(t *testing.T) {
			normalized, err := normalizeURL(raw)
			if err != nil {
				t.Fatal(err)
			}
			fetcher := &fakeFetcher{responses: map[string]httpx.Response{
				normalized: {Body: page}, feedURL: {Body: fixture(t)},
			}}
			candidates, err := (&Discoverer{client: fetcher}).Discover(context.Background(), raw)
			if err != nil || len(candidates) != 1 {
				t.Fatalf("candidates = %#v, requests = %#v, err = %v", candidates, fetcher.requests, err)
			}
			candidate := candidates[0]
			if candidate.FeedURL != feedURL || candidate.Connector != domain.ConnectorYouTube || candidate.Title != "Sema Channel" || candidate.AvatarURL != "https://yt3.example/avatar.jpg" || candidate.Type != "uploads" {
				t.Fatalf("candidate = %#v", candidate)
			}
		})
	}
}

func TestDiscoveryAcceptsBareHandleAndRejectsNonChannelPage(t *testing.T) {
	handleURL := "https://www.youtube.com/@sema"
	feedURL := feedBase + "UC123"
	fetcher := &fakeFetcher{responses: map[string]httpx.Response{
		handleURL: {Body: []byte(`<meta itemprop="channelId" content="UC123"><meta property="og:image" content="https://yt3.example/avatar.jpg">`)},
		feedURL:   {Body: fixture(t)},
	}}
	if candidates, err := (&Discoverer{client: fetcher}).Discover(context.Background(), "@sema"); err != nil || len(candidates) != 1 {
		t.Fatalf("bare handle candidates = %#v, err = %v", candidates, err)
	}
	nonChannel := "https://www.youtube.com/results?search_query=sema"
	fetcher = &fakeFetcher{responses: map[string]httpx.Response{nonChannel: {Body: []byte(`<html><title>Search</title></html>`)}}}
	if _, err := (&Discoverer{client: fetcher}).Discover(context.Background(), nonChannel); err == nil || !strings.Contains(err.Error(), "does not identify a channel") {
		t.Fatalf("error = %v", err)
	}
}

func TestPageMetadataPrefersChannelAvatarOverVideoOpenGraphImage(t *testing.T) {
	finalURL, _ := url.Parse("https://www.youtube.com/watch?v=regular1")
	metadata := pageMetadata(httpx.Response{
		FinalURL: finalURL,
		Body: []byte(`<html><head>
			<meta property="og:title" content="Regular upload - YouTube">
			<meta property="og:image" content="https://i.ytimg.com/vi/regular1/maxresdefault.jpg">
			<script>{"channelThumbnail":"https:\/\/yt3.ggpht.com\/channel-avatar=s88-c-k-c0x00ffffff-no-rj"}</script>
		</head></html>`),
	})
	if metadata.title != "Regular upload" {
		t.Fatalf("title = %q", metadata.title)
	}
	if metadata.avatar != "https://yt3.ggpht.com/channel-avatar=s88-c-k-c0x00ffffff-no-rj" {
		t.Fatalf("avatar = %q", metadata.avatar)
	}
}

type fakeHead struct {
	status int
	err    error
	calls  int
}

func (f *fakeHead) Head(context.Context, string, http.Header) (httpx.Response, error) {
	f.calls++
	return httpx.Response{StatusCode: f.status}, f.err
}

func TestShortsStatusTrick(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		err    error
		want   bool
	}{{"200 is Short", http.StatusOK, nil, true}, {"redirect is regular", http.StatusFound, nil, false}, {"failure defaults regular", 0, errors.New("offline"), false}} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeHead{status: test.status, err: test.err}
			if got := (&ShortsDetector{client: client}).IsShort(context.Background(), "video"); got != test.want || client.calls != 1 {
				t.Fatalf("IsShort = %v, calls = %d", got, client.calls)
			}
		})
	}
}
