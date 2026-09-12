package connector

import (
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/httpx"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestEntryTitleFallsBackToPostText(t *testing.T) {
	long := "<p>" + strings.Repeat("Bluesky post text ", 20) + "</p>"
	tests := []struct {
		name, title, summary, content string
		wantPrefix                    string
	}{
		{name: "feed title", title: "  Provided title  ", summary: "ignored", wantPrefix: "Provided title"},
		{name: "summary", summary: "<p>A title-less social post.</p>", wantPrefix: "A title-less social post."},
		{name: "content", content: "<p>Content-only post.</p>", wantPrefix: "Content-only post."},
		{name: "truncated", summary: long, wantPrefix: "Bluesky post text"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := EntryTitle(test.title, test.summary, test.content)
			if !strings.HasPrefix(got, test.wantPrefix) {
				t.Fatalf("EntryTitle() = %q", got)
			}
			if test.name == "truncated" && (len([]rune(got)) != derivedTitleRunes+1 || !strings.HasSuffix(got, "…")) {
				t.Fatalf("truncated title = %q (%d runes)", got, len([]rune(got)))
			}
		})
	}
}

func TestParseFeedResponseDropsUnsafeURLs(t *testing.T) {
	base, _ := url.Parse("https://example.com/feed")
	result, err := ParseFeedResponse(httpx.Response{StatusCode: http.StatusOK, FinalURL: base, Body: []byte(`<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/"><channel><title>Feed</title>
 <item><link>https://ok</link><enclosure url="data:image/png;base64,AA" type="image/png"/><media:content url="data:image/png;base64,BB" type="image/png"/><enclosure url="/image.jpg" type="image/jpeg"/></item>
 <item><link>javascript:alert(1)</link></item>
 <item><link>/relative</link></item></channel></rss>`)}, domain.Feed{URL: base.String()})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 2 || result.Entries[0].URL != "https://ok" || result.Entries[1].URL != "https://example.com/relative" {
		t.Fatalf("entries = %#v", result.Entries)
	}
	enclosures := result.Entries[0].Enclosures
	if len(enclosures) != 1 || enclosures[0].URL != "https://example.com/image.jpg" {
		t.Fatalf("enclosures = %#v", enclosures)
	}
}
