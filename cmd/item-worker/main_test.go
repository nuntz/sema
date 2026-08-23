package main

import (
	"net/url"
	"strings"
	"testing"

	"github.com/nuntz/sema/internal/domain"
)

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
