package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/extract"
	"github.com/nuntz/sema/internal/httpx"
)

type stubSummarizer struct {
	value string
	err   error
}

type stubHTTP struct {
	url      string
	response httpx.Response
}

func (s *stubHTTP) Get(_ context.Context, rawURL string, _ http.Header) (httpx.Response, error) {
	s.url = rawURL
	return s.response, nil
}

func (s stubSummarizer) Summarize(context.Context, string, string) (string, error) {
	return s.value, s.err
}

func TestIngestSizeUsesStoredCutoffs(t *testing.T) {
	model := domain.Model{ExplicitCount: 10, SizeCutoffs: &domain.SizeCutoffs{P60: 0.6, P90: 0.8}}
	if got := ingestSize(0.5, "2", model); got != "S" {
		t.Fatalf("ingest size = %s, want stored-cutoff S", got)
	}
	if got := ingestSize(0.5, "1", model); got != "M" {
		t.Fatalf("legacy ingest size = %s, want fixed-threshold M", got)
	}
}

func TestArticleContentDecision(t *testing.T) {
	shortCommentary := `<p>` + strings.Repeat("feed-commentary ", 45) + `<a href="/foo">source</a></p>`
	longCommentary := `<p>` + strings.Repeat("feed-commentary ", 220) + `</p>`
	shortAggregator := `<p>Article URL: https://linked.example/story Comments URL: https://news.ycombinator.com/item?id=1 ` + strings.Repeat("x", 40) + `</p>`
	redditContent := `<table><tr><td><img src="https://preview.redd.it/post.jpeg"></td><td><p>reddit-feed-body</p></td></tr></table>`
	linkedPage := []byte(`<html><head><title>Linked article</title></head><body><article><h1>Linked article</h1><p>` + strings.Repeat("linked-page ", 120) + `</p></article></body></html>`)
	tests := []struct {
		name       string
		raw        string
		itemURL    string
		siteURL    string
		wantText   string
		wantLink   string
		rejectText string
		pageHTML   []byte
	}{
		{name: "Daring Fireball commentary", raw: shortCommentary, itemURL: "https://corporate.walmart.com/story", siteURL: "https://daringfireball.net/", wantText: "feed-commentary", wantLink: `href="https://daringfireball.net/foo"`, rejectText: "linked-page"},
		{name: "Hacker News boilerplate", raw: shortAggregator, itemURL: "https://linked.example/story", siteURL: "https://news.ycombinator.com/", wantText: "linked-page", rejectText: "Article URL"},
		{name: "Show HN author writeup", raw: longCommentary, itemURL: "https://project.example/", siteURL: "https://news.ycombinator.com/", wantText: "feed-commentary", rejectText: "linked-page"},
		{name: "same-site short teaser", raw: shortCommentary, itemURL: "https://example.com/story", siteURL: "https://www.example.com/", wantText: "linked-page", rejectText: "feed-commentary"},
		{name: "same-site substantial content", raw: longCommentary, itemURL: "https://example.com/story", siteURL: "https://example.com/", wantText: "feed-commentary", rejectText: "linked-page"},
		{name: "blocked same-site page falls back to feed content", raw: redditContent, itemURL: "https://www.reddit.com/comments/one", siteURL: "https://www.reddit.com/", wantText: "reddit-feed-body", pageHTML: []byte(`<html><head><title>Reddit</title></head><body></body></html>`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pageURL, _ := url.Parse(test.itemURL)
			pageHTML := test.pageHTML
			if pageHTML == nil {
				pageHTML = linkedPage
			}
			article, err := articleContent(test.raw, test.itemURL, test.siteURL, pageURL, pageHTML)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(article.Text, test.wantText) || (test.rejectText != "" && strings.Contains(article.Text, test.rejectText)) {
				t.Fatalf("article text = %q", article.Text)
			}
			if test.wantLink != "" && !strings.Contains(article.HTML, test.wantLink) {
				t.Fatalf("article HTML does not contain %q: %s", test.wantLink, article.HTML)
			}
		})
	}
}

func TestChooseSummaryGenerationAndFailureFallback(t *testing.T) {
	article := extract.Result{
		Text:           "A first factual paragraph about the subject. A second paragraph adds detail.",
		FirstParagraph: "A first factual paragraph about the subject.", Quality: 0.8,
	}
	h := &handler{summarizer: stubSummarizer{value: "A generated first sentence. A generated second sentence."}}
	got, source, metrics := h.chooseSummary(context.Background(), "Title", "Read more…", article, false)
	if got == "" || source != domain.SummarySourceGenerated || metrics["SummariesGenerated"] != 1 {
		t.Fatalf("generated summary = %q, %q, %#v", got, source, metrics)
	}
	h.summarizer = stubSummarizer{err: errors.New("offline")}
	got, source, metrics = h.chooseSummary(context.Background(), "Title", "Read more…", article, false)
	if got != article.FirstParagraph || source != domain.SummarySourceBody || metrics["SummaryFallbackError"] != 1 {
		t.Fatalf("fallback summary = %q, %q, %#v", got, source, metrics)
	}
}

func TestForcedSummaryReplayStillKeepsHealthyFeedSummaries(t *testing.T) {
	if forceSummaryGeneration(false, domain.SummarySourceFeed) {
		t.Fatal("healthy feed summary was forced through generation")
	}
	for _, source := range []string{domain.SummarySourceGenerated, domain.SummarySourceBody} {
		if !forceSummaryGeneration(false, source) {
			t.Fatalf("summary source %q was not regenerated", source)
		}
	}
	if !forceSummaryGeneration(true, domain.SummarySourceFeed) {
		t.Fatal("always-generate feed did not force generation")
	}
}

func TestVimeoThumbnailUsesOfficialOEmbedMetadata(t *testing.T) {
	client := &stubHTTP{response: httpx.Response{StatusCode: http.StatusOK, Body: []byte(`{"thumbnail_url":"https://i.vimeocdn.com/video/42.jpg"}`)}}
	h := &handler{http: client}
	got, err := h.embedThumbnailURL(context.Background(), extract.MediaCard{Provider: "Vimeo", URL: "https://vimeo.com/12345"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://i.vimeocdn.com/video/42.jpg" || !strings.HasPrefix(client.url, "https://vimeo.com/api/oembed.json?url=") {
		t.Fatalf("thumbnail = %q, request = %q", got, client.url)
	}
}
