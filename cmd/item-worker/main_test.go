package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/extract"
	"github.com/nuntz/sema/internal/httpx"
	"github.com/nuntz/sema/internal/media"
	"github.com/nuntz/sema/internal/store"
	storycluster "github.com/nuntz/sema/internal/story"
	"github.com/nuntz/sema/internal/vectorstore"
)

type fakeItemStore struct {
	feedErr      error
	failures     []itemFailure
	resolved     []domain.Item
	putStory     *domain.Story
	addedStoryID string
	addedItemID  string
	setStoryItem string
	setStoryID   string
}

type itemFailure struct {
	user, item string
	ttl        int64
}

func (*fakeItemStore) Content(context.Context, string) ([]byte, string, error) {
	return nil, "", store.ErrNotFound
}
func (*fakeItemStore) ContentExists(context.Context, string) (bool, error) { return false, nil }
func (*fakeItemStore) ContentURL(string) string                            { return "" }
func (s *fakeItemStore) Feed(context.Context, string, string) (domain.Feed, error) {
	return domain.Feed{}, s.feedErr
}
func (*fakeItemStore) Item(context.Context, string, string) (domain.Item, error) {
	return domain.Item{}, store.ErrNotFound
}
func (*fakeItemStore) OverwriteItem(context.Context, domain.Item) error         { return nil }
func (*fakeItemStore) PutContent(context.Context, string, string, []byte) error { return nil }
func (*fakeItemStore) PutItem(context.Context, domain.Item) (bool, error)       { return true, nil }
func (s *fakeItemStore) PutItemFailure(_ context.Context, user, item string, ttl int64) error {
	s.failures = append(s.failures, itemFailure{user: user, item: item, ttl: ttl})
	return nil
}
func (*fakeItemStore) Signals(context.Context, string) ([]domain.Signal, error) { return nil, nil }
func (s *fakeItemStore) ResolveItemIDs(context.Context, string, []string) ([]domain.Item, error) {
	return append([]domain.Item(nil), s.resolved...), nil
}
func (s *fakeItemStore) PutStory(_ context.Context, row domain.Story) error {
	s.putStory = &row
	return nil
}
func (s *fakeItemStore) AddStoryMember(_ context.Context, _ string, storyID, itemID string, _ int64) error {
	s.addedStoryID, s.addedItemID = storyID, itemID
	return nil
}
func (s *fakeItemStore) SetItemStory(_ context.Context, item domain.Item, storyID string) error {
	s.setStoryItem, s.setStoryID = item.ItemID, storyID
	return nil
}

type stubSummarizer struct {
	value string
	err   error
}

type stubEmbedder struct{}

func (stubEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1, 0}, nil
}

type stubVectorBatchStore struct {
	calls   int
	records []vectorstore.Record
	matches []vectorstore.Match
}

func (s *stubVectorBatchStore) PutBatch(_ context.Context, records []vectorstore.Record) error {
	s.calls++
	s.records = append([]vectorstore.Record(nil), records...)
	return nil
}

func (s *stubVectorBatchStore) Query(context.Context, []float32, int, int64) ([]vectorstore.Match, error) {
	return append([]vectorstore.Match(nil), s.matches...), nil
}

func TestAssignStoryCreatesAndJoins(t *testing.T) {
	now := time.Now().UTC()
	config := storycluster.Config{Threshold: 80, Window: 72 * time.Hour}
	newItem := domain.Item{ItemID: "new", FeedID: "new-feed", URL: "https://example.com/new", PublishedTS: domain.Timestamp(now), TTL: now.Add(time.Hour).Unix()}
	founder := domain.Item{PK: "U#user", SK: "I#founder", ItemID: "founder", FeedID: "old-feed", URL: "https://example.com/founder", PublishedTS: domain.Timestamp(now.Add(-time.Hour)), TTL: now.Add(2 * time.Hour).Unix()}
	repository := &fakeItemStore{resolved: []domain.Item{founder}}
	vectors := &stubVectorBatchStore{matches: []vectorstore.Match{{Key: "new", Similarity: 100}, {Key: "other-user", Similarity: 99}, {Key: "founder", Similarity: 82}}}
	h := &handler{store: repository, vectors: vectors, storyConfig: config}
	metrics, err := h.assignStory(context.Background(), "user", []float32{1, 0}, &newItem)
	if err != nil {
		t.Fatal(err)
	}
	if newItem.StoryID != "founder" || repository.putStory == nil || repository.putStory.TTL != founder.TTL || repository.setStoryItem != "founder" || metrics["story_created"] != 1 || metrics["story_candidates"] != 1 {
		t.Fatalf("item = %#v, story = %#v, metrics = %#v", newItem, repository.putStory, metrics)
	}

	joined := founder
	joined.StoryID = "existing-story"
	repository = &fakeItemStore{resolved: []domain.Item{joined}}
	newItem.StoryID = ""
	h.store = repository
	metrics, err = h.assignStory(context.Background(), "user", []float32{1, 0}, &newItem)
	if err != nil {
		t.Fatal(err)
	}
	if newItem.StoryID != "existing-story" || repository.addedStoryID != "existing-story" || repository.addedItemID != "new" || metrics["story_joined"] != 1 {
		t.Fatalf("join item = %#v, store = %#v, metrics = %#v", newItem, repository, metrics)
	}
}

type stubHTTP struct {
	url      string
	headers  http.Header
	response httpx.Response
}

func (s *stubHTTP) Get(_ context.Context, rawURL string, headers http.Header) (httpx.Response, error) {
	s.url = rawURL
	s.headers = headers.Clone()
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

func TestPermanentMissingTitleWritesMarkerAndConsumesMessage(t *testing.T) {
	published := time.Now().UTC()
	repository := &fakeItemStore{}
	h := &handler{store: repository}
	body := `{"user":"user","feed_id":"feed","item_id":"item","published_ts":"` + domain.Timestamp(published) + `"}`

	response, err := h.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "message", Body: body}}})
	if err != nil || len(response.BatchItemFailures) != 0 {
		t.Fatalf("run = %#v, %v", response, err)
	}
	if len(repository.failures) != 1 {
		t.Fatalf("failure markers = %#v", repository.failures)
	}
	marker := repository.failures[0]
	if marker.user != "user" || marker.item != "item" || marker.ttl != published.Add(domain.Retention).Unix() {
		t.Fatalf("marker = %#v", marker)
	}
}

func TestTransientItemFailureStillRetries(t *testing.T) {
	repository := &fakeItemStore{feedErr: errors.New("dynamo unavailable")}
	h := &handler{store: repository}
	body := `{"user":"user","feed_id":"feed","item_id":"item","title":"Title","published_ts":"` + domain.Timestamp(time.Now()) + `"}`

	response, err := h.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "message", Body: body}}})
	if err != nil || len(response.BatchItemFailures) != 1 || response.BatchItemFailures[0].ItemIdentifier != "message" {
		t.Fatalf("run = %#v, %v", response, err)
	}
	if len(repository.failures) != 0 {
		t.Fatalf("transient failure wrote markers: %#v", repository.failures)
	}
}

func TestRunBatchesVectorsAcrossWrittenItems(t *testing.T) {
	vectors := &stubVectorBatchStore{}
	h := &handler{
		store:          &fakeItemStore{},
		media:          media.New(nil),
		embedder:       stubEmbedder{},
		scoringVersion: "1",
		vectors:        vectors,
	}
	published := domain.Timestamp(time.Now().UTC())
	message := func(id string) events.SQSMessage {
		body := `{"user":"user","feed_id":"feed","item_id":"` + id + `","title":"Title","summary_raw":"Useful summary","published_ts":"` + published + `"}`
		return events.SQSMessage{MessageId: "message-" + id, Body: body}
	}

	response, err := h.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{message("one"), message("two")}})
	if err != nil || len(response.BatchItemFailures) != 0 {
		t.Fatalf("run = %#v, %v", response, err)
	}
	if vectors.calls != 1 || len(vectors.records) != 2 {
		t.Fatalf("vector batch calls = %d, records = %#v", vectors.calls, vectors.records)
	}
	items := map[string]bool{}
	for _, record := range vectors.records {
		items[record.Key] = true
	}
	if !items["one"] || !items["two"] {
		t.Fatalf("vector records = %#v", vectors.records)
	}
}

func TestFailureMetricsCarryOnlyFeedIDDimension(t *testing.T) {
	type event struct {
		metrics    map[string]float64
		dimensions map[string]string
	}
	events := []event{}
	emitItemMetrics(map[string]float64{"ItemsWritten": 1, "BodyImageFailed": 2}, "feed", false, false, func(metrics map[string]float64, dimensions map[string]string) {
		events = append(events, event{metrics: metrics, dimensions: dimensions})
	})
	if len(events) != 2 || events[0].dimensions != nil || events[0].metrics["ItemsWritten"] != 1 || events[0].metrics["BodyImageFailed"] != 0 {
		t.Fatalf("base metric event = %#v", events)
	}
	failure := events[1]
	if failure.metrics["ExtractionFailed"] != 1 || failure.metrics["MediaFailed"] != 1 || failure.metrics["BodyImageFailed"] != 2 {
		t.Fatalf("failure metrics = %#v", failure.metrics)
	}
	if len(failure.dimensions) != 1 || failure.dimensions["FeedID"] != "feed" {
		t.Fatalf("failure dimensions = %#v", failure.dimensions)
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

func TestRedditContentURLsAvoidThreadScraping(t *testing.T) {
	thread := "https://www.reddit.com/r/example/comments/one/title/"
	external := "https://example.com/story"
	for _, test := range []struct {
		name, postType, external, wantContent, wantFetch string
	}{
		{"legacy", "", "", thread, thread},
		{"text", "text", "", thread, ""},
		{"image", "image", "https://i.redd.it/one.jpg", thread, ""},
		{"gallery", "gallery", "https://www.reddit.com/gallery/one", thread, ""},
		{"video", "video", "https://v.redd.it/one", thread, ""},
		{"link", "link", external, external, external},
	} {
		t.Run(test.name, func(t *testing.T) {
			content, fetch := itemContentURLs(domain.ItemMessage{URL: thread, ExternalURL: test.external, PostType: test.postType})
			if content != test.wantContent || fetch != test.wantFetch {
				t.Fatalf("content, fetch = %q, %q", content, fetch)
			}
		})
	}
}

func TestReplayRecoversRedditGalleryEnclosuresFromPostFeed(t *testing.T) {
	feedURL := "https://www.reddit.com/comments/1w1q9k6/.rss"
	base, _ := url.Parse(feedURL)
	body := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:media="http://search.yahoo.com/mrss/">
  <entry><id>t3_1w1q9k6</id><title>Evening vibes in Vancouver</title><published>2026-08-29T15:59:14Z</published><link href="https://www.reddit.com/r/vancouver/comments/1w1q9k6/evening_vibes_in_vancouver/"/><media:thumbnail url="https://preview.redd.it/5bpaudvx6cmh1.jpg?width=140&amp;amp;height=140&amp;amp;crop=1:1,smart&amp;amp;auto=webp&amp;amp;s=signature"/><content type="html">&lt;table&gt;&lt;tr&gt;&lt;td&gt;&lt;a href="https://www.reddit.com/gallery/1w1q9k6"&gt;[link]&lt;/a&gt;&lt;/td&gt;&lt;/tr&gt;&lt;/table&gt;</content></entry>
</feed>`
	client := &stubHTTP{response: httpx.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: []byte(body), FinalURL: base}}
	h := &handler{http: client}

	enclosures, err := h.mediaEnclosures(context.Background(), domain.ItemMessage{
		URL: "https://www.reddit.com/r/vancouver/comments/1w1q9k6/evening_vibes_in_vancouver/", PostType: "gallery",
	}, domain.Feed{Connector: domain.ConnectorReddit})
	if err != nil {
		t.Fatal(err)
	}
	if client.url != feedURL || client.headers.Get("User-Agent") != "linux:sema:rss" {
		t.Fatalf("request = %q, headers = %#v", client.url, client.headers)
	}
	if len(enclosures) != 2 || enclosures[0].URL != "https://i.redd.it/5bpaudvx6cmh1.jpg" {
		t.Fatalf("enclosures = %#v", enclosures)
	}
}

func TestRedditSelftextPreservesStoredFormatting(t *testing.T) {
	thread := "https://www.reddit.com/r/example/comments/one/title/"
	pageURL, _ := url.Parse(thread)
	raw := `<p>Opening paragraph with an <a href="https://example.com/reference">inline link</a>.</p><ul><li>First point</li><li>Second point</li></ul><blockquote><p>Quoted text</p></blockquote>`
	article, err := articleContent(raw, thread, "https://www.reddit.com/r/example/", pageURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"<p>", "<ul>", "<li>", "<blockquote>", `href="https://example.com/reference"`} {
		if !strings.Contains(article.HTML, expected) {
			t.Fatalf("sanitized selftext does not contain %q: %s", expected, article.HTML)
		}
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

func TestChooseSummaryKeepsCleanRedditExcerpt(t *testing.T) {
	h := &handler{summarizer: stubSummarizer{value: "must not be used"}}
	excerpt := "A cleaned subreddit excerpt with the submitter and link boilerplate removed."
	got, source, metrics := h.chooseSummary(context.Background(), "Post title", excerpt, extract.Result{}, false)
	if got != excerpt || source != domain.SummarySourceFeed || len(metrics) != 0 {
		t.Fatalf("summary = %q, source = %q, metrics = %#v", got, source, metrics)
	}
}

func TestChooseSummaryReportsMissingBody(t *testing.T) {
	h := &handler{summarizer: stubSummarizer{value: "must not be used"}}
	got, source, metrics := h.chooseSummary(context.Background(), "Title", "", extract.Result{}, false)
	if got != "" || source != domain.SummarySourceBody || metrics["SummaryFallbackNoBody"] != 1 || metrics["SummaryFallbackLowQuality"] != 0 {
		t.Fatalf("summary = %q, source = %q, metrics = %#v", got, source, metrics)
	}
}

func TestChooseSummaryReportsLowQualityBody(t *testing.T) {
	h := &handler{summarizer: stubSummarizer{value: "must not be used"}}
	article := extract.Result{Text: "A low-quality article body.", FirstParagraph: "A low-quality article body.", Quality: 0.2}
	got, source, metrics := h.chooseSummary(context.Background(), "Title", "", article, false)
	if got != article.FirstParagraph || source != domain.SummarySourceBody || metrics["SummaryFallbackLowQuality"] != 1 || metrics["SummaryFallbackNoBody"] != 0 {
		t.Fatalf("summary = %q, source = %q, metrics = %#v", got, source, metrics)
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
