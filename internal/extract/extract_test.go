package extract

import (
	"fmt"
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

func TestExtractionV2PreservesStructureAndPrunesBoilerplate(t *testing.T) {
	base, _ := url.Parse("https://example.com/posts/story")
	raw := `<article>
		<p>` + strings.Repeat("A useful opening sentence with enough article prose. ", 20) + `</p>
		<pre><code class="language-go highlighted">func main() { println("ok") }</code></pre>
		<table><thead><tr><th scope="col">Year</th><th>Value</th></tr></thead><tbody><tr><td>2026</td><td>42</td></tr></tbody></table>
		<figure><img data-src="images/lazy.jpg" data-srcset="images/small.jpg 640w, images/large.jpg 1600w, images/huge.jpg 2400w"><figcaption>Useful chart</figcaption></figure>
		<div class="newsletter-signup"><p>Subscribe to our newsletter and read related posts.</p></div>
	</article>`
	result, err := FeedContent(raw, base)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="language-go"`, "<pre>", "<code", "<table>", "<thead>", "<tbody>", "<figure>", "<figcaption>", `src="https://example.com/posts/images/large.jpg"`} {
		if !strings.Contains(result.HTML, want) {
			t.Fatalf("extracted HTML missing %q: %s", want, result.HTML)
		}
	}
	for _, unwanted := range []string{"newsletter-signup", "Subscribe to our newsletter", "huge.jpg"} {
		if strings.Contains(result.HTML, unwanted) {
			t.Fatalf("extracted HTML contains %q: %s", unwanted, result.HTML)
		}
	}
	if result.Quality <= 0.3 || result.Quality > 1 {
		t.Fatalf("quality = %f, want readable range", result.Quality)
	}
}

func TestMediaCardsAreProviderLinksWithoutThirdPartyLoadsAfterResolution(t *testing.T) {
	base, _ := url.Parse("https://example.com/story")
	raw := `<p>` + strings.Repeat("Article prose. ", 50) + `</p><iframe title="A useful talk" src="https://www.youtube-nocookie.com/embed/abc123"></iframe><iframe src="https://tracker.example/ad"></iframe>`
	cleaned, err := Sanitize(raw, base)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="media-card"`, `data-provider="YouTube"`, `href="https://www.youtube.com/watch?v=abc123"`, "A useful talk", "opens on youtube.com"} {
		if !strings.Contains(cleaned, want) {
			t.Fatalf("media card missing %q: %s", want, cleaned)
		}
	}
	if strings.Contains(cleaned, "tracker.example") || strings.Contains(cleaned, "<iframe") {
		t.Fatalf("unknown embed survived: %s", cleaned)
	}
	resolved, failures := ResolveMediaCards(cleaned, func(card MediaCard) (string, error) {
		if card.Provider != "YouTube" || card.ThumbnailURL == "" {
			t.Fatalf("card = %#v", card)
		}
		return "/media/user/item/embed-0.webp", nil
	})
	if len(failures) != 0 || !strings.Contains(resolved, `src="/media/user/item/embed-0.webp"`) || strings.Contains(resolved, "i.ytimg.com") || strings.Contains(resolved, "data-thumbnail") {
		t.Fatalf("resolved card = %s, failures = %v", resolved, failures)
	}

	compact, failures := ResolveMediaCards(cleaned, func(MediaCard) (string, error) { return "", fmt.Errorf("offline") })
	if len(failures) != 1 || strings.Contains(compact, "<img") || strings.Contains(compact, "i.ytimg.com") {
		t.Fatalf("compact card = %s, failures = %v", compact, failures)
	}
}

func TestArticleExtractionRetainsYouTubeEmbed(t *testing.T) {
	base, _ := url.Parse("https://example.com/story")
	document := []byte(`<html><head><title>Video story</title></head><body><article><p>` + strings.Repeat("Substantial article prose before the video. ", 50) + `</p><iframe title="Field report" src="https://www.youtube.com/embed/video42"></iframe><p>` + strings.Repeat("More reporting after the video. ", 30) + `</p></article></body></html>`)
	result, err := Article(document, base)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.HTML, `class="media-card"`) || !strings.Contains(result.HTML, "video42") {
		t.Fatalf("article extraction dropped embed: %s", result.HTML)
	}
}

func TestVimeoCardDefersThumbnailResolutionToWorker(t *testing.T) {
	base, _ := url.Parse("https://example.com/story")
	cleaned, err := Sanitize(`<iframe title="Documentary" src="https://player.vimeo.com/video/98765"></iframe>`, base)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	resolved, failures := ResolveMediaCards(cleaned, func(card MediaCard) (string, error) {
		called = true
		if card.Provider != "Vimeo" || card.URL != "https://vimeo.com/98765" || card.ThumbnailURL != "" {
			t.Fatalf("card = %#v", card)
		}
		return "/media/user/item/embed-0.webp", nil
	})
	if !called || len(failures) != 0 || !strings.Contains(resolved, `/media/user/item/embed-0.webp`) {
		t.Fatalf("resolved = %q, failures = %v", resolved, failures)
	}
}

func TestArticleCapturesMetadataAndQuality(t *testing.T) {
	base, _ := url.Parse("https://example.com/story")
	document := []byte(`<html><head><title>Metadata story</title><meta name="author" content="Ada Lovelace"><meta property="article:published_time" content="2026-08-20T12:30:00Z"></head><body><article><h1>Metadata story</h1><p>` + strings.Repeat("Substantial readable article text. ", 80) + `</p><p>Second paragraph completes the extraction.</p></article><aside>` + strings.Repeat("navigation ", 100) + `</aside></body></html>`)
	result, err := Article(document, base)
	if err != nil {
		t.Fatal(err)
	}
	if result.Author != "Ada Lovelace" || result.DisplayDate != "2026-08-20T12:30:00Z" || result.Quality <= 0.3 || result.Quality > 1 {
		t.Fatalf("result metadata/quality = %#v", result)
	}
}

func TestQualityRegressionBaselines(t *testing.T) {
	paragraphs := func(count, words int, linked bool) string {
		var body strings.Builder
		for range count {
			text := strings.Repeat("word ", words)
			if linked {
				text = `<a href="https://example.com">` + text + `</a>`
			}
			body.WriteString("<p>" + text + "</p>")
		}
		return body.String()
	}
	tests := []struct {
		name      string
		html      string
		pageWords int
		baseline  float64
	}{
		{name: "complete article", html: paragraphs(5, 10, false), pageWords: 50, baseline: 1},
		{name: "half page coverage", html: paragraphs(5, 10, false), pageWords: 100, baseline: 0.75},
		{name: "link dense result", html: paragraphs(5, 10, true), pageWords: 50, baseline: 0.7},
		{name: "thin result", html: paragraphs(1, 10, false), pageWords: 100, baseline: 0.39},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Quality(test.html, test.pageWords); got != test.baseline {
				t.Fatalf("quality = %.4f, baseline %.4f", got, test.baseline)
			}
		})
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
