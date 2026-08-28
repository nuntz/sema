package reddit

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/httpx"
)

type fakeFetcher struct {
	response httpx.Response
	check    func(string, http.Header)
}

func (f fakeFetcher) Get(_ context.Context, rawURL string, headers http.Header) (httpx.Response, error) {
	if f.check != nil {
		f.check(rawURL, headers)
	}
	return f.response, nil
}

func TestFetchTransformsObservedRedditAtomShapes(t *testing.T) {
	feedURL := "https://www.reddit.com/r/example/top.rss?t=day"
	base, _ := url.Parse(feedURL)
	body := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:media="http://search.yahoo.com/mrss/">
  <title>top scoring links : example</title><link rel="alternate" href="https://www.reddit.com/r/example/top?t=day"/>
  <entry><author><name>/u/writer</name></author><id>t3_self</id><title>Self post</title><published>2026-08-26T12:00:00Z</published><link href="https://www.reddit.com/r/example/comments/self/title/"/><content type="html">&lt;!-- SC_OFF --&gt;&lt;div class="md"&gt;&lt;p&gt;Body with &lt;a href="https://inside.example"&gt;an inner link&lt;/a&gt;.&lt;/p&gt;&lt;/div&gt;&lt;!-- SC_ON --&gt; submitted by &lt;a href="https://www.reddit.com/u/writer"&gt;/u/writer&lt;/a&gt;&lt;br/&gt;&lt;span&gt;&lt;a href="https://www.reddit.com/r/example/comments/self/title/"&gt;[link]&lt;/a&gt;&lt;/span&gt; &lt;span&gt;&lt;a href="https://www.reddit.com/r/example/comments/self/title/"&gt;[comments]&lt;/a&gt;&lt;/span&gt;</content></entry>
  <entry><author><name>/u/linker</name></author><id>t3_link</id><title>Link post</title><published>2026-08-26T13:00:00Z</published><link href="https://www.reddit.com/r/example/comments/link/title/"/><content type="html">&lt;div class="md"&gt;&lt;p&gt;Submitter context.&lt;/p&gt;&lt;/div&gt; submitted by &lt;a href="https://www.reddit.com/u/linker"&gt;/u/linker&lt;/a&gt;&lt;br/&gt;&lt;span&gt;&lt;a href="https://arxiv.org/abs/123"&gt;[link]&lt;/a&gt;&lt;/span&gt; &lt;span&gt;&lt;a href="https://www.reddit.com/r/example/comments/link/title/"&gt;[comments]&lt;/a&gt;&lt;/span&gt;</content></entry>
  <entry><author><name>/u/photo</name></author><id>t3_image</id><title>Image post</title><published>2026-08-26T14:00:00Z</published><link href="https://www.reddit.com/r/example/comments/image/title/"/><media:thumbnail url="https://preview.redd.it/image.jpeg?width=640&amp;amp;crop=smart"/><content type="html">&lt;table&gt;&lt;tr&gt;&lt;td&gt;&lt;img src="https://preview.redd.it/fallback.jpeg?width=320&amp;amp;crop=smart"/&gt;&lt;/td&gt;&lt;td&gt;submitted by /u/photo&lt;br/&gt;&lt;span&gt;&lt;a href="https://i.redd.it/image.jpeg"&gt;[link]&lt;/a&gt;&lt;/span&gt; &lt;span&gt;&lt;a href="https://www.reddit.com/r/example/comments/image/title/"&gt;[comments]&lt;/a&gt;&lt;/span&gt;&lt;/td&gt;&lt;/tr&gt;&lt;/table&gt;</content></entry>
</feed>`
	connector := &Connector{client: fakeFetcher{
		response: httpx.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: []byte(body), FinalURL: base},
		check: func(gotURL string, headers http.Header) {
			if gotURL != feedURL || headers.Get("User-Agent") != UserAgent || headers.Get("If-None-Match") != `"old"` || headers.Get("If-Modified-Since") != "yesterday" {
				t.Fatalf("request URL = %q, headers = %#v", gotURL, headers)
			}
		},
	}}
	result, err := connector.Fetch(context.Background(), domain.Feed{URL: feedURL, ETag: `"old"`, LastModified: "yesterday"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "r/example" || result.SiteURL != "https://www.reddit.com/r/example/" || len(result.Entries) != 3 {
		t.Fatalf("result = %#v", result)
	}
	self := result.Entries[0]
	if self.GUID != "t3_self" || self.PostType != "text" || self.ExternalURL != "" || self.SummaryRaw != "Body with an inner link." || strings.Contains(self.ContentRaw, "submitted by") || !strings.Contains(self.ContentRaw, "inside.example") {
		t.Fatalf("self entry = %#v", self)
	}
	link := result.Entries[1]
	if link.GUID != "t3_link" || link.PostType != "link" || link.ExternalURL != "https://arxiv.org/abs/123" || link.SummaryRaw != "Submitter context." {
		t.Fatalf("link entry = %#v", link)
	}
	image := result.Entries[2]
	if image.GUID != "t3_image" || image.PostType != "image" || image.ExternalURL != "https://i.redd.it/image.jpeg" || len(image.Enclosures) != 1 || strings.Contains(image.Enclosures[0].URL, "&amp;") {
		t.Fatalf("image entry = %#v", image)
	}
}

func TestImageFallbackAndVideoInference(t *testing.T) {
	base, _ := url.Parse("https://www.reddit.com/r/example/.rss")
	entry := domain.Entry{URL: "https://www.reddit.com/r/example/comments/one/title/", ContentRaw: `<table><tr><td><img src="https://preview.redd.it/fallback.jpeg?width=320&amp;crop=smart"></td><td><a href="https://v.redd.it/abc">[link]</a></td></tr></table>`}
	transformEntry(&entry, base)
	if entry.PostType != "video" || entry.ExternalURL != "https://v.redd.it/abc" || len(entry.Enclosures) != 1 || entry.Enclosures[0].URL != "https://preview.redd.it/fallback.jpeg?width=320&crop=smart" {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestThumbnailIsMediaSignalForExternalImageTarget(t *testing.T) {
	if got := inferPostType("https://images.example/post.jpg", true); got != "image" {
		t.Fatalf("post type = %q, want image", got)
	}
	if got := inferPostType("https://example.com/articles/story", true); got != "link" {
		t.Fatalf("article post type = %q, want link", got)
	}
	if got := inferPostType("https://www.reddit.com/gallery/abc123", true); got != "gallery" {
		t.Fatalf("gallery post type = %q, want gallery", got)
	}
	if got := inferPostType("", true); got != "text" {
		t.Fatalf("thumbnail-only post type = %q, want text", got)
	}
}

func TestParseInputAndCanonicalURLs(t *testing.T) {
	tests := []struct {
		input string
		sort  Sort
	}{
		{"r/Castles", SortTopDay},
		{"/r/Castles/", SortTopDay},
		{"https://www.reddit.com/r/Castles/", SortTopDay},
		{"reddit.com/r/castles/.rss", SortHot},
		{"https://old.reddit.com/r/castles/top.rss?t=day", SortTopDay},
		{"https://m.reddit.com/r/castles/new.rss", SortNew},
		{"https://www.reddit.com//r//Castles//?utm_source=share#fragment", SortTopDay},
		{"https://www.reddit.com/r/castles/.RSS?utm_source=share", SortHot},
		{"https://www.reddit.com/r/castles/top.rss?t=DAY&utm_source=share", SortTopDay},
		{"https://www.reddit.com/r/castles/new.rss?utm_source=share", SortNew},
	}
	for _, test := range tests {
		got, err := ParseInput(test.input)
		if err != nil || got.Subreddit != "castles" || got.Sort != test.sort {
			t.Errorf("ParseInput(%q) = %#v, %v", test.input, got, err)
		}
	}
	if got := CanonicalURL("Castles", SortHot); got != "https://www.reddit.com/r/castles/.rss" {
		t.Fatalf("hot URL = %q", got)
	}
	for _, rejected := range []string{"castles", "https://reddit.com/user/name/.rss", "https://reddit.com/r/a/comments/one", "https://reddit.com/r/castles/top.rss?t=week", "https://reddit.com.example/r/castles/.rss"} {
		if _, err := ParseInput(rejected); err == nil {
			t.Errorf("ParseInput(%q) succeeded", rejected)
		}
	}
}

func TestDiscoveryUsesCanonicalSortFixedUAAndLogo(t *testing.T) {
	base, _ := url.Parse("https://www.reddit.com/r/example/new.rss")
	body := `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>newest submissions : example</title><logo>https://styles.redditmedia.com/logo.png</logo><entry><id>t3_one</id><title>One</title><link href="https://www.reddit.com/r/example/comments/one/title/"/><published>2026-08-26T14:00:00Z</published></entry></feed>`
	discoverer := &Discoverer{client: fakeFetcher{
		response: httpx.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: []byte(body), FinalURL: base},
		check: func(rawURL string, headers http.Header) {
			if rawURL != base.String() || headers.Get("User-Agent") != UserAgent {
				t.Fatalf("request = %q, %#v", rawURL, headers)
			}
		},
	}}
	candidates, err := discoverer.Discover(context.Background(), "https://m.reddit.com/r/Example/new.rss")
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates = %#v, %v", candidates, err)
	}
	got := candidates[0]
	if got.FeedURL != base.String() || got.Title != "r/example" || got.SiteURL != "https://www.reddit.com/r/example/" || got.BadgeURL != "https://styles.redditmedia.com/logo.png" || got.Cadence != "New" || got.ItemCount != 1 {
		t.Fatalf("candidate = %#v", got)
	}
}

func TestDiscoveryReturnsStructuredHTTPError(t *testing.T) {
	discoverer := &Discoverer{client: fakeFetcher{response: httpx.Response{StatusCode: http.StatusForbidden, Header: make(http.Header)}}}
	_, err := discoverer.Discover(context.Background(), "r/example")
	status, ok := err.(*DiscoveryError)
	if !ok || status.Kind != "unavailable" || status.StatusCode != http.StatusForbidden || !strings.Contains(status.Error(), "no Reddit login") {
		t.Fatalf("error = %#v", err)
	}
}
