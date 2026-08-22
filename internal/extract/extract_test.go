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

func TestRemoveLeadImage(t *testing.T) {
	const source = "https://cdn.example.com/lead.jpg"
	tests := []struct {
		name    string
		raw     string
		source  string
		want    string
		removed bool
	}{
		{name: "direct", raw: `<img src="https://cdn.example.com/lead.jpg"><p>Body</p>`, want: `<p>Body</p>`, removed: true},
		{name: "image only wrapper", raw: `<figure><a href="https://example.com"><img src="https://cdn.example.com/lead.jpg"></a></figure><p>Body</p>`, want: `<p>Body</p>`, removed: true},
		{name: "different image", raw: `<img src="https://cdn.example.com/other.jpg"><p>Body</p>`},
		{name: "image after text", raw: `<p>Introduction</p><img src="https://cdn.example.com/lead.jpg">`, want: `<p>Introduction</p>`, removed: true},
		{name: "nested wrappers", raw: `<div><figure><a href="https://example.com"><img src="https://cdn.example.com/lead.jpg"></a></figure></div><p>Body</p>`, want: `<p>Body</p>`, removed: true},
		{name: "container with text", raw: `<p>Before<img src="https://cdn.example.com/lead.jpg"> after</p>`, want: `<p>Before after</p>`, removed: true},
		{name: "gallery", raw: `<div><img src="https://cdn.example.com/lead.jpg"><img src="https://cdn.example.com/two.jpg"><img src="https://cdn.example.com/three.jpg"></div>`, want: `<div><img src="https://cdn.example.com/two.jpg"/><img src="https://cdn.example.com/three.jpg"/></div>`, removed: true},
		{name: "captioned figure", raw: `<figure><img src="https://cdn.example.com/lead.jpg"><figcaption>Keep this caption</figcaption></figure>`},
		{name: "empty body", source: source},
		{name: "empty source", raw: `<img src="https://cdn.example.com/lead.jpg">`, source: " "},
		{name: "equivalent URL", raw: `<img src="https://cdn.example.com/lead%20image.jpg"><p>Body</p>`, source: "https://cdn.example.com/lead image.jpg", want: `<p>Body</p>`, removed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testSource := test.source
			if testSource == "" {
				testSource = source
			}
			cleaned, removed := RemoveLeadImage(test.raw, testSource)
			if removed != test.removed {
				t.Fatalf("removed = %v, want %v; body = %s", removed, test.removed, cleaned)
			}
			if removed {
				if cleaned != test.want {
					t.Fatalf("cleaned body = %s, want %s", cleaned, test.want)
				}
			} else if cleaned != test.raw {
				t.Fatalf("unchanged body = %s, want %s", cleaned, test.raw)
			}
		})
	}
}

func TestRemoveLeadImageRedditTable(t *testing.T) {
	const source = "https://external-preview.redd.it/lead.png?width=640&crop=smart"
	raw := `<table><tr><td> <a href="https://example.com/post"><img src="https://external-preview.redd.it/lead.png?width=640&amp;crop=smart"></a> </td><td> submitted by <a href="https://reddit.com/u/GaryWray">/u/GaryWray</a> <br> <a href="https://example.com/link">[link]</a> <a href="https://reddit.com/comments/1">[comments]</a> </td></tr></table>`

	cleaned, removed := RemoveLeadImage(raw, source)
	if !removed {
		t.Fatal("lead image was not removed")
	}
	for _, want := range []string{"<table>", "/u/GaryWray", "[link]", "[comments]"} {
		if !strings.Contains(cleaned, want) {
			t.Fatalf("cleaned body missing %q: %s", want, cleaned)
		}
	}
	if strings.Contains(cleaned, "<img") || strings.Contains(cleaned, "https://example.com/post") || strings.Count(cleaned, "<td") != 1 || strings.Count(cleaned, "<a ") != 3 {
		t.Fatalf("lead image cell was not pruned: %s", cleaned)
	}
}
