package extract

import (
	"net/url"
	"strings"
	"testing"
)

func TestSanitize(t *testing.T) {
	base, _ := url.Parse("https://example.com/news/story")
	raw := `<article><script>alert(1)</script><p>Hello <strong>world</strong>.</p><a href="/more">More</a><img src="img.jpg" onerror="bad()"><iframe src="bad"></iframe></article>`
	got, err := Sanitize(raw, base)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"script", "iframe", "onerror"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("sanitized content contains %q: %s", forbidden, got)
		}
	}
	if !strings.Contains(got, `href="https://example.com/more"`) || !strings.Contains(got, `src="https://example.com/news/img.jpg"`) {
		t.Fatalf("relative URLs were not resolved: %s", got)
	}
}

func TestSummary(t *testing.T) {
	long := strings.Repeat("word ", 100)
	got := Summary("<p>"+long+"</p>", "fallback")
	if len([]rune(got)) > 401 || !strings.HasSuffix(got, "…") {
		t.Fatalf("summary not truncated at a word boundary: %q", got)
	}
	if got := Summary("", "<p>fallback text</p>"); got != "fallback text" {
		t.Fatalf("fallback summary = %q", got)
	}
}

func TestIsLinkblogEntry(t *testing.T) {
	commentary := func(length int) string { return `<p>` + strings.Repeat("x", length) + `</p>` }
	tests := []struct {
		name    string
		itemURL string
		siteURL string
		raw     string
		want    bool
	}{
		{name: "Daring Fireball", itemURL: "https://corporate.walmart.com/news", siteURL: "https://daringfireball.net/", raw: commentary(709), want: true},
		{name: "Hacker News", itemURL: "https://example.com/article", siteURL: "https://news.ycombinator.com/", raw: commentary(150)},
		{name: "Show HN", itemURL: "https://example.com/project", siteURL: "https://news.ycombinator.com/", raw: commentary(4000), want: true},
		{name: "same site", itemURL: "https://example.com/article", siteURL: "https://example.com/", raw: commentary(700)},
		{name: "subdomain", itemURL: "https://www.example.com/article", siteURL: "https://example.com/", raw: commentary(700)},
		{name: "blank site URL", itemURL: "https://example.com/article", raw: commentary(700)},
		{name: "unparseable item URL", itemURL: "://bad", siteURL: "https://example.com/", raw: commentary(700)},
		{name: "empty content", itemURL: "https://example.com/article", siteURL: "https://feed.example.org/"},
		{name: "exact floor", itemURL: "https://example.com/article", siteURL: "https://example.org/", raw: commentary(LinkblogCommentaryMinRunes)},
		{name: "over floor", itemURL: "https://example.com/article", siteURL: "https://example.org/", raw: commentary(LinkblogCommentaryMinRunes + 1), want: true},
		{name: "fallback different IP hosts", itemURL: "http://192.0.2.1/article", siteURL: "http://192.0.2.2/", raw: commentary(700), want: true},
		{name: "fallback same IP host", itemURL: "http://192.0.2.1/article", siteURL: "http://192.0.2.1/", raw: commentary(700)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsLinkblogEntry(test.itemURL, test.siteURL, test.raw); got != test.want {
				t.Fatalf("IsLinkblogEntry() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRemoveLeadingImage(t *testing.T) {
	const source = "https://cdn.example.com/lead.jpg"
	tests := []struct {
		name    string
		raw     string
		removed bool
	}{
		{name: "direct", raw: `<img src="https://cdn.example.com/lead.jpg"><p>Body</p>`, removed: true},
		{name: "image only wrapper", raw: `<figure><a href="https://example.com"><img src="https://cdn.example.com/lead.jpg"></a></figure><p>Body</p>`, removed: true},
		{name: "different image", raw: `<img src="https://cdn.example.com/other.jpg"><p>Body</p>`},
		{name: "image after text", raw: `<p>Introduction</p><img src="https://cdn.example.com/lead.jpg">`},
		{name: "captioned figure", raw: `<figure><img src="https://cdn.example.com/lead.jpg"><figcaption>Keep this caption</figcaption></figure>`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleaned, removed := RemoveLeadingImage(test.raw, source)
			if removed != test.removed {
				t.Fatalf("removed = %v, want %v; body = %s", removed, test.removed, cleaned)
			}
			if removed {
				if strings.Contains(cleaned, source) || !strings.Contains(cleaned, "Body") {
					t.Fatalf("unexpected cleaned body: %s", cleaned)
				}
			} else if cleaned != test.raw {
				t.Fatalf("unchanged body = %s, want %s", cleaned, test.raw)
			}
		})
	}
}
