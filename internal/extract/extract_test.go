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
