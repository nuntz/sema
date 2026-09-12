package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/extract"
	"github.com/nuntz/sema/internal/httpx"
	"github.com/nuntz/sema/internal/media"
	"github.com/nuntz/sema/internal/observability"
	"github.com/nuntz/sema/internal/score"
	"github.com/nuntz/sema/internal/store"
	storycluster "github.com/nuntz/sema/internal/story"
	"github.com/nuntz/sema/internal/vectorstore"
)

type fakeItemStore struct {
	feedErr      error
	resolveErr   error
	failures     []itemFailure
	resolved     []domain.Item
	putStory     *domain.Story
	addedStoryID string
	addedItemID  string
	setStoryItem string
	setStoryID   string
	item         domain.Item
	content      map[string][]byte
	contentReads []string
	overwritten  *domain.Item
}

type itemFailure struct {
	user, item string
	ttl        int64
}

type concurrentItemStore struct {
	fakeItemStore
	active atomic.Int32
	peak   atomic.Int32
	calls  atomic.Int32
}

func (s *concurrentItemStore) Feed(context.Context, string, string) (domain.Feed, error) {
	active := s.active.Add(1)
	defer s.active.Add(-1)
	s.calls.Add(1)
	for peak := s.peak.Load(); active > peak; peak = s.peak.Load() {
		if s.peak.CompareAndSwap(peak, active) {
			break
		}
	}
	// Keep calls overlapping so an unbounded batch exposes its concurrency.
	time.Sleep(20 * time.Millisecond)
	return domain.Feed{}, errors.New("feed unavailable")
}

func TestRunBoundsBatchConcurrency(t *testing.T) {
	repository := &concurrentItemStore{}
	h := &handler{store: repository}
	published := domain.Timestamp(time.Now().UTC())
	event := events.SQSEvent{}
	for _, id := range []string{"one", "two", "three", "four", "five"} {
		event.Records = append(event.Records, events.SQSMessage{
			MessageId: id,
			Body:      `{"user":"user","feed_id":"feed","item_id":"` + id + `","title":"Title","published_ts":"` + published + `"}`,
		})
	}
	response, err := h.run(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if peak := repository.peak.Load(); peak < 1 || peak > 2 {
		t.Errorf("peak concurrency = %d, want between 1 and 2", peak)
	}
	if calls := repository.calls.Load(); calls != 5 {
		t.Errorf("processed records = %d, want 5", calls)
	}
	failures := map[string]bool{}
	for _, failure := range response.BatchItemFailures {
		failures[failure.ItemIdentifier] = true
	}
	if len(response.BatchItemFailures) != 5 {
		t.Errorf("failures = %#v, want 5", response.BatchItemFailures)
	}
	for _, record := range event.Records {
		if !failures[record.MessageId] {
			t.Errorf("missing failure for %s", record.MessageId)
		}
	}
}

func (s *fakeItemStore) Content(_ context.Context, key string) ([]byte, string, error) {
	s.contentReads = append(s.contentReads, key)
	if body, ok := s.content[key]; ok {
		return append([]byte(nil), body...), "image/jpeg", nil
	}
	return nil, "", store.ErrNotFound
}
func (s *fakeItemStore) ContentExists(_ context.Context, key string) (bool, error) {
	_, ok := s.content[key]
	return ok, nil
}
func (*fakeItemStore) ContentURL(string) string { return "" }
func (s *fakeItemStore) Feed(context.Context, string, string) (domain.Feed, error) {
	return domain.Feed{}, s.feedErr
}
func (s *fakeItemStore) Item(context.Context, string, string) (domain.Item, error) {
	if s.item.ItemID != "" {
		return s.item, nil
	}
	return domain.Item{}, store.ErrNotFound
}
func (s *fakeItemStore) ItemByIdentity(ctx context.Context, userID, itemID string) (domain.Item, error) {
	return s.Item(ctx, userID, itemID)
}
func (s *fakeItemStore) OverwriteItem(_ context.Context, item domain.Item) error {
	s.overwritten = &item
	return nil
}
func (*fakeItemStore) PutContent(context.Context, string, string, []byte) error { return nil }
func (*fakeItemStore) PutItem(context.Context, domain.Item) (bool, error)       { return true, nil }
func (s *fakeItemStore) PutItemFailure(_ context.Context, user, item string, ttl int64) error {
	s.failures = append(s.failures, itemFailure{user: user, item: item, ttl: ttl})
	return nil
}
func (*fakeItemStore) Signals(context.Context, string) ([]domain.Signal, error) { return nil, nil }
func (s *fakeItemStore) ResolveItemIDsConsistent(context.Context, string, []string) ([]domain.Item, error) {
	return append([]domain.Item(nil), s.resolved...), s.resolveErr
}
func (s *fakeItemStore) CreateStory(_ context.Context, row domain.Story) (bool, error) {
	if s.putStory != nil {
		return false, nil
	}
	s.putStory = &row
	return true, nil
}
func (s *fakeItemStore) AddStoryMember(_ context.Context, _ string, storyID, itemID string, _ int64) error {
	s.addedStoryID, s.addedItemID = storyID, itemID
	if s.putStory != nil && s.putStory.StoryID == storyID {
		for _, member := range s.putStory.MemberIDs {
			if member == itemID {
				return nil
			}
		}
		s.putStory.MemberIDs = append(s.putStory.MemberIDs, itemID)
	}
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

type countingTextEmbedder struct{ calls int }

func (s *countingTextEmbedder) Embed(context.Context, string) ([]float32, error) {
	s.calls++
	return []float32{1, 0}, nil
}

type stubImageEmbedder struct {
	vector []float32
	err    error
	images [][]byte
}

func (s *stubImageEmbedder) EmbedImage(_ context.Context, jpeg []byte) ([]float32, error) {
	s.images = append(s.images, append([]byte(nil), jpeg...))
	return append([]float32(nil), s.vector...), s.err
}

func (s *stubImageEmbedder) EmbedText(context.Context, string) ([]float32, error) {
	return nil, errors.New("unexpected text image embedding")
}

type stubVectorBatchStore struct {
	err     error
	calls   int
	records []vectorstore.Record
	matches []vectorstore.Match
}

func (s *stubVectorBatchStore) PutBatch(_ context.Context, records []vectorstore.Record) error {
	s.calls++
	s.records = append([]vectorstore.Record(nil), records...)
	return s.err
}

func (s *stubVectorBatchStore) Query(context.Context, string, []float32, int, int64) ([]vectorstore.Match, error) {
	return append([]vectorstore.Match(nil), s.matches...), nil
}

func TestAssignStoryPreservesMembersWithStaleFounder(t *testing.T) {
	now := time.Now().UTC()
	founder := domain.Item{PK: "U#user", SK: "I#founder", ItemID: "founder", PublishedTS: domain.Timestamp(now), TTL: now.Add(time.Hour).Unix()}
	// Simulate workers that both resolved the founder before either assigned
	// its story. Each sees the same stale snapshot, including on redelivery.
	repository := &fakeItemStore{resolved: []domain.Item{founder}}
	h := &handler{
		store: repository, vectors: &stubVectorBatchStore{matches: []vectorstore.Match{{Key: "founder", Similarity: 90}}},
		storyConfig: storycluster.Config{Threshold: 80, Window: 72 * time.Hour},
	}
	for index, id := range []string{"first", "second", "first"} {
		item := domain.Item{ItemID: id, PublishedTS: founder.PublishedTS, TTL: founder.TTL}
		metrics, err := h.assignStory(context.Background(), "user", []float32{1, 0}, &item)
		if err != nil || item.StoryID != "founder" {
			t.Fatalf("assignment %s = %s, %v", id, item.StoryID, err)
		}
		metric := "StoryCreated"
		if index > 0 {
			metric = "StoryJoined"
		}
		if metrics[metric] != 1 {
			t.Fatalf("metrics = %v, want %s", metrics, metric)
		}
	}
	if got := strings.Join(repository.putStory.MemberIDs, ","); got != "founder,first,second" {
		t.Fatalf("members = %s", got)
	}
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
	if newItem.StoryID != "founder" || repository.putStory == nil || repository.putStory.TTL != founder.TTL || repository.setStoryItem != "founder" || metrics["StoryCreated"] != 1 || metrics["StoryCandidates"] != 1 {
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
	if newItem.StoryID != "existing-story" || repository.addedStoryID != "existing-story" || repository.addedItemID != "new" || metrics["StoryJoined"] != 1 {
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
	if !items[vectorstore.Key("user", "one")] || !items[vectorstore.Key("user", "two")] {
		t.Fatalf("vector records = %#v", vectors.records)
	}
}

func TestStoryAssignmentFailureEmitsMetricAndConsumesMessage(t *testing.T) {
	repository := &fakeItemStore{resolveErr: errors.New("resolve item IDs")}
	emitted := []map[string]float64{}
	h := &handler{
		store: repository, media: media.New(nil), embedder: stubEmbedder{}, scoringVersion: "1", vectors: &stubVectorBatchStore{},
		emit: func(metrics map[string]float64, _ map[string]string) {
			emitted = append(emitted, metrics)
		},
	}
	body := `{"user":"user","feed_id":"feed","item_id":"item","title":"Title","summary_raw":"Useful summary","published_ts":"` + domain.Timestamp(time.Now().UTC()) + `"}`

	response, err := h.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "message", Body: body}}})
	if err != nil || len(response.BatchItemFailures) != 0 {
		t.Fatalf("run = %#v, %v", response, err)
	}
	found := false
	for _, metrics := range emitted {
		if metrics["StoryAssignmentFailed"] == 1 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("emitted metrics = %#v, want StoryAssignmentFailed = 1", emitted)
	}
}

func TestReprocessEmbedsStored768VariantAndBatchesImageVector(t *testing.T) {
	now := time.Now().UTC()
	existing := domain.Item{
		PK: "U#user", SK: domain.ItemSK(now, "item"), ItemID: "item", FeedID: "feed", Title: "Title", Summary: "Summary",
		PublishedTS: domain.Timestamp(now), FetchedTS: domain.Timestamp(now), MediaKey: "lead-1280.jpg",
		MediaVariants: []domain.MediaVariant{{Key: "lead-384.jpg", Width: 384}, {Key: "lead-768.jpg", Width: 768}, {Key: "lead-1280.jpg", Width: 1280}},
		Vector:        []byte("old-text"), TTL: now.Add(time.Hour).Unix(),
	}
	repository := &fakeItemStore{item: existing, content: map[string][]byte{"lead-1280.jpg": {1}, "lead-768.jpg": {7, 6, 8}}}
	images := &stubImageEmbedder{vector: []float32{3, 4}}
	textVectors, imageVectors := &stubVectorBatchStore{}, &stubVectorBatchStore{}
	h := &handler{
		store: repository, embedder: stubEmbedder{}, imageEmbedder: images, imageModelVersion: "image-v1",
		scoringVersion: "1", vectors: textVectors, imageVectors: imageVectors,
	}
	body := `{"user":"user","feed_id":"feed","item_id":"item","title":"Title","summary_raw":"Summary","published_ts":"` + domain.Timestamp(now) + `","reprocess":true}`
	response, err := h.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "message", Body: body}}})
	if err != nil || len(response.BatchItemFailures) != 0 {
		t.Fatalf("run = %#v, %v", response, err)
	}
	if len(images.images) != 1 || string(images.images[0]) != string([]byte{7, 6, 8}) || len(repository.contentReads) != 1 || repository.contentReads[0] != "lead-768.jpg" {
		t.Fatalf("image calls = %#v, content reads = %#v", images.images, repository.contentReads)
	}
	if repository.overwritten == nil || repository.overwritten.ImageModelVersion != "image-v1" || len(repository.overwritten.ImageVector) == 0 {
		t.Fatalf("overwritten item = %#v", repository.overwritten)
	}
	if imageVectors.calls != 1 || len(imageVectors.records) != 1 || imageVectors.records[0].Key != vectorstore.Key("user", "item") {
		t.Fatalf("image vector batch = %#v", imageVectors)
	}
}

func TestImageEmbeddingFailureDoesNotFailItemAndPreservesReplayVector(t *testing.T) {
	now := time.Now().UTC()
	existingImage := []byte("existing-image-vector")
	existing := domain.Item{
		PK: "U#user", SK: domain.ItemSK(now, "item"), ItemID: "item", FeedID: "feed", Title: "Title", Summary: "Summary",
		PublishedTS: domain.Timestamp(now), FetchedTS: domain.Timestamp(now), MediaKey: "lead.jpg",
		MediaVariants: []domain.MediaVariant{{Key: "lead.jpg", Width: 768}}, Vector: []byte("old-text"),
		ImageVector: existingImage, ImageModelVersion: "image-v0", TTL: now.Add(time.Hour).Unix(),
	}
	repository := &fakeItemStore{item: existing, content: map[string][]byte{"lead.jpg": {7}}}
	images := &stubImageEmbedder{err: errors.New("Bedrock unavailable")}
	h := &handler{
		store: repository, embedder: stubEmbedder{}, imageEmbedder: images, imageModelVersion: "image-v1",
		scoringVersion: "1", vectors: &stubVectorBatchStore{}, imageVectors: &stubVectorBatchStore{},
	}
	body := `{"user":"user","feed_id":"feed","item_id":"item","title":"Title","published_ts":"` + domain.Timestamp(now) + `","reprocess":true}`
	response, err := h.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "message", Body: body}}})
	if err != nil || len(response.BatchItemFailures) != 0 {
		t.Fatalf("run = %#v, %v", response, err)
	}
	if repository.overwritten == nil || string(repository.overwritten.ImageVector) != string(existingImage) || repository.overwritten.ImageModelVersion != "image-v0" {
		t.Fatalf("replay image vector was not preserved: %#v", repository.overwritten)
	}
}

func TestCompatibleReplayPreservesTextVectorWithoutEmbedding(t *testing.T) {
	now := time.Now().UTC()
	textVector := []byte{0, 0, 128, 63, 0, 0, 0, 0}
	existing := domain.Item{
		PK: "U#user", SK: domain.ItemSK(now, "item"), ItemID: "item", FeedID: "feed", Title: "Title",
		PublishedTS: domain.Timestamp(now), FetchedTS: domain.Timestamp(now), Vector: textVector, ModelVersion: "text-v1", TTL: now.Add(time.Hour).Unix(),
	}
	repository := &fakeItemStore{item: existing}
	embedder := &countingTextEmbedder{}
	h := &handler{store: repository, embedder: embedder, modelVersion: "text-v1", scoringVersion: "1", vectors: &stubVectorBatchStore{}}
	body := `{"user":"user","feed_id":"feed","item_id":"item","title":"Title","published_ts":"` + domain.Timestamp(now) + `","reprocess":true}`
	response, err := h.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "message", Body: body}}})
	if err != nil || len(response.BatchItemFailures) != 0 {
		t.Fatalf("run = %#v, %v", response, err)
	}
	if embedder.calls != 0 || repository.overwritten == nil || string(repository.overwritten.Vector) != string(textVector) {
		t.Fatalf("replay embed calls = %d, item = %#v", embedder.calls, repository.overwritten)
	}
}

func TestForcedExtractRefreshesCompatibleTextVector(t *testing.T) {
	now := time.Now().UTC()
	oldVector := score.EncodeVector([]float32{0, 1})
	existing := domain.Item{
		PK: "U#user", SK: domain.ItemSK(now, "item"), ItemID: "item", FeedID: "feed", Title: "Title",
		PublishedTS: domain.Timestamp(now), FetchedTS: domain.Timestamp(now), Vector: oldVector, ModelVersion: "text-v1", TTL: now.Add(time.Hour).Unix(),
	}
	repository := &fakeItemStore{item: existing}
	embedder := &countingTextEmbedder{}
	h := &handler{store: repository, media: media.New(nil), embedder: embedder, modelVersion: "text-v1", scoringVersion: "1", vectors: &stubVectorBatchStore{}}
	body := `{"user":"user","feed_id":"feed","item_id":"item","title":"Title","published_ts":"` + domain.Timestamp(now) + `","reprocess":true,"force_extract":true}`
	response, err := h.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "message", Body: body}}})
	if err != nil || len(response.BatchItemFailures) != 0 {
		t.Fatalf("run = %#v, %v", response, err)
	}
	if embedder.calls != 1 || repository.overwritten == nil || string(repository.overwritten.Vector) == string(oldVector) {
		t.Fatalf("replay embed calls = %d, item = %#v", embedder.calls, repository.overwritten)
	}
}

func TestCompatibleReplayReusesImageVectorAndBatchesRecord(t *testing.T) {
	now := time.Now().UTC()
	imageVector := score.EncodeVector([]float32{1, 0})
	existing := domain.Item{
		PK: "U#user", SK: domain.ItemSK(now, "item"), ItemID: "item", FeedID: "feed", Title: "Title",
		PublishedTS: domain.Timestamp(now), FetchedTS: domain.Timestamp(now), MediaKey: "lead.jpg",
		MediaVariants: []domain.MediaVariant{{Key: "lead.jpg", Width: 768}}, Vector: score.EncodeVector([]float32{0, 1}), ModelVersion: "text-v1",
		ImageVector: imageVector, ImageModelVersion: "image-v1", TTL: now.Add(time.Hour).Unix(),
	}
	repository := &fakeItemStore{item: existing, content: map[string][]byte{"lead.jpg": {7}}}
	images := &stubImageEmbedder{vector: []float32{0, 1}}
	imageVectors := &stubVectorBatchStore{}
	h := &handler{
		store: repository, embedder: stubEmbedder{}, modelVersion: "text-v1", imageEmbedder: images, imageModelVersion: "image-v1",
		scoringVersion: "1", vectors: &stubVectorBatchStore{}, imageVectors: imageVectors,
	}
	body := `{"user":"user","feed_id":"feed","item_id":"item","title":"Title","published_ts":"` + domain.Timestamp(now) + `","reprocess":true}`
	response, err := h.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "message", Body: body}}})
	if err != nil || len(response.BatchItemFailures) != 0 {
		t.Fatalf("run = %#v, %v", response, err)
	}
	if len(images.images) != 0 {
		t.Fatalf("image embed calls = %d, want 0", len(images.images))
	}
	if repository.overwritten == nil || string(repository.overwritten.ImageVector) != string(imageVector) || repository.overwritten.ImageModelVersion != "image-v1" {
		t.Fatalf("overwritten item = %#v", repository.overwritten)
	}
	if imageVectors.calls != 1 || len(imageVectors.records) != 1 || imageVectors.records[0].Key != vectorstore.Key("user", "item") {
		t.Fatalf("image vector batch = %#v", imageVectors)
	}
}

func TestIncompatibleReplayRefreshesImageVector(t *testing.T) {
	now := time.Now().UTC()
	oldVector := score.EncodeVector([]float32{1, 0})
	existing := domain.Item{
		PK: "U#user", SK: domain.ItemSK(now, "item"), ItemID: "item", FeedID: "feed", Title: "Title",
		PublishedTS: domain.Timestamp(now), FetchedTS: domain.Timestamp(now), MediaKey: "lead.jpg",
		MediaVariants: []domain.MediaVariant{{Key: "lead.jpg", Width: 768}}, Vector: score.EncodeVector([]float32{0, 1}), ModelVersion: "text-v1",
		ImageVector: oldVector, ImageModelVersion: "image-v0", TTL: now.Add(time.Hour).Unix(),
	}
	repository := &fakeItemStore{item: existing, content: map[string][]byte{"lead.jpg": {7}}}
	images := &stubImageEmbedder{vector: []float32{0, 1}}
	imageVectors := &stubVectorBatchStore{}
	h := &handler{
		store: repository, embedder: stubEmbedder{}, modelVersion: "text-v1", imageEmbedder: images, imageModelVersion: "image-v1",
		scoringVersion: "1", vectors: &stubVectorBatchStore{}, imageVectors: imageVectors,
	}
	body := `{"user":"user","feed_id":"feed","item_id":"item","title":"Title","published_ts":"` + domain.Timestamp(now) + `","reprocess":true}`
	response, err := h.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "message", Body: body}}})
	if err != nil || len(response.BatchItemFailures) != 0 {
		t.Fatalf("run = %#v, %v", response, err)
	}
	if len(images.images) != 1 || string(images.images[0]) != string([]byte{7}) {
		t.Fatalf("image embed calls = %#v", images.images)
	}
	if repository.overwritten == nil || repository.overwritten.ImageModelVersion != "image-v1" || string(repository.overwritten.ImageVector) == string(oldVector) {
		t.Fatalf("overwritten item = %#v", repository.overwritten)
	}
	if imageVectors.calls != 1 || len(imageVectors.records) != 1 {
		t.Fatalf("image vector batch = %#v", imageVectors)
	}
}

func TestForcedExtractClearsImageVectorWhenLeadDisappears(t *testing.T) {
	now := time.Now().UTC()
	existing := domain.Item{
		PK: "U#user", SK: domain.ItemSK(now, "item"), ItemID: "item", FeedID: "feed", Title: "Title",
		PublishedTS: domain.Timestamp(now), FetchedTS: domain.Timestamp(now), MediaKey: "old-lead.jpg",
		MediaVariants: []domain.MediaVariant{{Key: "old-lead.jpg", Width: 768}}, Vector: score.EncodeVector([]float32{0, 1}), ModelVersion: "text-v1",
		ImageVector: score.EncodeVector([]float32{1, 0}), ImageModelVersion: "image-v1", TTL: now.Add(time.Hour).Unix(),
	}
	repository := &fakeItemStore{item: existing}
	images := &stubImageEmbedder{vector: []float32{0, 1}}
	imageVectors := &stubVectorBatchStore{}
	h := &handler{
		store: repository, media: media.New(nil), embedder: stubEmbedder{}, modelVersion: "text-v1", imageEmbedder: images, imageModelVersion: "image-v1",
		scoringVersion: "1", vectors: &stubVectorBatchStore{}, imageVectors: imageVectors,
	}
	body := `{"user":"user","feed_id":"feed","item_id":"item","title":"Title","published_ts":"` + domain.Timestamp(now) + `","reprocess":true,"force_extract":true}`
	response, err := h.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "message", Body: body}}})
	if err != nil || len(response.BatchItemFailures) != 0 {
		t.Fatalf("run = %#v, %v", response, err)
	}
	if len(images.images) != 0 {
		t.Fatalf("image embed calls = %d, want 0", len(images.images))
	}
	if repository.overwritten == nil || repository.overwritten.MediaKey != "" || len(repository.overwritten.ImageVector) != 0 || repository.overwritten.ImageModelVersion != "" {
		t.Fatalf("overwritten item = %#v", repository.overwritten)
	}
	if imageVectors.calls != 0 || len(imageVectors.records) != 0 {
		t.Fatalf("image vector batch = %#v", imageVectors)
	}
}

func TestVideoNeverEmbedsOrBatchesImageVector(t *testing.T) {
	now := time.Now().UTC()
	existing := domain.Item{
		PK: "U#user", SK: domain.ItemSK(now, "video"), ItemID: "video", FeedID: "feed", Title: "Video",
		PublishedTS: domain.Timestamp(now), FetchedTS: domain.Timestamp(now), MediaKey: "thumbnail.jpg", MediaType: "video",
		Vector: []byte("old-text"), ImageVector: []byte("must-clear"), ImageModelVersion: "image-v1", TTL: now.Add(time.Hour).Unix(),
	}
	repository := &fakeItemStore{item: existing, content: map[string][]byte{"thumbnail.jpg": {1}}}
	images := &stubImageEmbedder{vector: []float32{1, 0}}
	imageVectors := &stubVectorBatchStore{}
	h := &handler{
		store: repository, embedder: stubEmbedder{}, imageEmbedder: images, imageModelVersion: "image-v1",
		scoringVersion: "1", vectors: &stubVectorBatchStore{}, imageVectors: imageVectors,
	}
	body := `{"user":"user","feed_id":"feed","item_id":"video","title":"Video","media_type":"video","published_ts":"` + domain.Timestamp(now) + `","reprocess":true}`
	response, err := h.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "message", Body: body}}})
	if err != nil || len(response.BatchItemFailures) != 0 {
		t.Fatalf("run = %#v, %v", response, err)
	}
	if len(images.images) != 0 || imageVectors.calls != 0 || repository.overwritten == nil || len(repository.overwritten.ImageVector) != 0 {
		t.Fatalf("video image channel = calls %d, batch %d, item %#v", len(images.images), imageVectors.calls, repository.overwritten)
	}
}

func TestSelectedImageVariantKey(t *testing.T) {
	tests := []struct {
		name     string
		variants []domain.MediaVariant
		fallback string
		want     string
	}{
		{name: "largest within limit", variants: []domain.MediaVariant{{Key: "1280", Width: 1280}, {Key: "384", Width: 384}, {Key: "768", Width: 768}}, want: "768"},
		{name: "smallest fallback", variants: []domain.MediaVariant{{Key: "1280", Width: 1280}, {Key: "900", Width: 900}}, want: "900"},
		{name: "manifest absent", fallback: "lead", want: "lead"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := selectedImageVariantKey(test.variants, test.fallback); got != test.want {
				t.Fatalf("selected key = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFailureMetricsCarryOnlyFeedIDDimension(t *testing.T) {
	type event struct {
		metrics    map[string]float64
		dimensions map[string]string
	}
	events := []event{}
	emitItemMetrics(map[string]float64{"ItemsWritten": 1, "BodyImageFailed": 2}, domain.Feed{FeedID: "feed"}, false, false, func(metrics map[string]float64, dimensions map[string]string) {
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

func TestVectorFailureRetriesStoredEmbeddings(t *testing.T) {
	for _, imageFailure := range []bool{false, true} {
		repository := &fakeItemStore{item: domain.Item{PK: "U#user", ItemID: "item", Vector: score.EncodeVector([]float32{1, 0}), ImageVector: score.EncodeVector([]float32{0, 1}), ArchiveSK: "A#saved"}}
		text, image := &stubVectorBatchStore{}, &stubVectorBatchStore{}
		failed := text
		if imageFailure {
			failed = image
		}
		failed.err = errors.New("temporary outage")
		h := &handler{store: repository, vectors: text, imageVectors: image}
		event := events.SQSEvent{Records: []events.SQSMessage{{MessageId: "retry", Body: `{"user":"user","item_id":"item","published_ts":"` + domain.Timestamp(time.Now()) + `"}`}}}
		response, err := h.run(context.Background(), event)
		if err != nil || len(response.BatchItemFailures) != 1 || response.BatchItemFailures[0].ItemIdentifier != "retry" {
			t.Fatalf("failure = %#v, %v", response, err)
		}
		failed.err = nil
		response, err = h.run(context.Background(), event)
		if err != nil || len(response.BatchItemFailures) != 0 {
			t.Fatalf("retry = %#v, %v", response, err)
		}
		if len(text.records) != 1 || len(image.records) != 1 || text.records[0].Kind != vectorstore.KindArchive || text.calls != 2 || image.calls != 2 {
			t.Fatal("stored vectors were not retried")
		}
	}
}

type dedupRaceStore struct {
	fakeItemStore
	reads int
}

func (s *dedupRaceStore) ItemByIdentity(context.Context, string, string) (domain.Item, error) {
	s.reads++
	if s.reads == 1 {
		return domain.Item{}, store.ErrNotFound
	}
	return s.item, nil
}
func (s *dedupRaceStore) PutItem(context.Context, domain.Item) (bool, error) { return false, nil }
func TestDedupRaceStillIndexesWinningStoredVectors(t *testing.T) {
	repository := &dedupRaceStore{fakeItemStore: fakeItemStore{item: domain.Item{PK: "U#user", ItemID: "item", Vector: score.EncodeVector([]float32{0, 1}), ArchiveSK: "A#saved"}}}
	vectors := &stubVectorBatchStore{}
	h := &handler{store: repository, media: media.New(nil), embedder: stubEmbedder{}, vectors: vectors, scoringVersion: "1"}
	body := `{"user":"user","feed_id":"feed","item_id":"item","title":"Title","summary_raw":"Summary","published_ts":"` + domain.Timestamp(time.Now()) + `"}`
	response, err := h.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "dedup", Body: body}}})
	if err != nil || len(response.BatchItemFailures) != 0 {
		t.Fatalf("%#v %v", response, err)
	}
	if len(vectors.records) != 1 || vectors.records[0].Data[1] != 1 || vectors.records[0].Kind != vectorstore.KindArchive {
		t.Fatal(vectors.records)
	}
}

type ingestionLookupStore struct {
	fakeItemStore
	lookupErr    error
	overwriteErr error
	dedup        bool
}

func (s *ingestionLookupStore) Item(context.Context, string, string) (domain.Item, error) {
	return domain.Item{}, errors.New("legacy item lookup must not run during ingestion")
}
func (s *ingestionLookupStore) ItemByIdentity(context.Context, string, string) (domain.Item, error) {
	if s.lookupErr != nil {
		return domain.Item{}, s.lookupErr
	}
	return domain.Item{}, store.ErrNotFound
}
func (s *ingestionLookupStore) PutItem(context.Context, domain.Item) (bool, error) {
	return !s.dedup, nil
}

func TestIngestionUsesPointReadsAndAcknowledgesTerminalDedupe(t *testing.T) {
	for _, test := range []struct {
		name      string
		dedup     bool
		lookupErr error
		failed    bool
	}{
		{name: "fresh"},
		{name: "failure marker", dedup: true},
		{name: "lookup unavailable", lookupErr: errors.New("throttled"), failed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &ingestionLookupStore{dedup: test.dedup, lookupErr: test.lookupErr}
			vectors := &stubVectorBatchStore{}
			h := &handler{store: repository, media: media.New(nil), embedder: stubEmbedder{}, vectors: vectors, scoringVersion: "1"}
			response, err := h.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "item", Body: `{"user":"user","item_id":"item","feed_id":"feed","title":"Title","summary_raw":"Summary","published_ts":"` + domain.Timestamp(time.Now()) + `"}`}}})
			if err != nil || (len(response.BatchItemFailures) > 0) != test.failed {
				t.Fatalf("%#v %v", response, err)
			}
			expected := 1
			if test.dedup || test.failed {
				expected = 0
			}
			if vectors.calls != expected {
				t.Fatalf("index calls=%d", vectors.calls)
			}
		})
	}
}

type disappearingReplayStore struct {
	fakeItemStore
	overwriteErr error
}

func (s *disappearingReplayStore) OverwriteItem(context.Context, domain.Item) error {
	return s.overwriteErr
}

func TestReplayAcknowledgesMissingRowsButRetriesWriteFailures(t *testing.T) {
	for _, test := range []struct {
		name     string
		absent   bool
		writeErr error
		failed   bool
	}{
		{name: "gone before read", absent: true},
		{name: "gone before update", writeErr: store.ErrNotFound},
		{name: "write unavailable", writeErr: errors.New("throttled"), failed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now()
			repository := &disappearingReplayStore{overwriteErr: test.writeErr}
			if !test.absent {
				repository.item = domain.Item{PK: "U#user", SK: domain.ItemSK(now, "item"), ItemID: "item", Title: "Title", Vector: score.EncodeVector([]float32{1, 0}), TTL: now.Add(time.Hour).Unix()}
			}
			vectors := &stubVectorBatchStore{}
			h := &handler{store: repository, media: media.New(nil), embedder: stubEmbedder{}, vectors: vectors, scoringVersion: "1"}
			response, err := h.run(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "item", Body: `{"user":"user","item_id":"item","feed_id":"feed","reprocess":true,"published_ts":"` + domain.Timestamp(now) + `"}`}}})
			if err != nil || (len(response.BatchItemFailures) > 0) != test.failed || vectors.calls != 0 {
				t.Fatalf("%#v %v index calls=%d", response, err, vectors.calls)
			}
		})
	}
}

type deadlineItemStore struct {
	fakeItemStore
	t *testing.T
}

func (s *deadlineItemStore) ItemByIdentity(ctx context.Context, _, itemID string) (domain.Item, error) {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > itemTimeout {
		s.t.Error("item lookup has no bounded deadline")
	}
	if itemID == "slow" {
		<-ctx.Done()
		return domain.Item{}, ctx.Err()
	}
	return domain.Item{ItemID: itemID}, nil
}

func TestItemDeadlineReportsOnlyExpiredMessage(t *testing.T) {
	var deadlineEvents atomic.Int32
	h := &handler{store: &deadlineItemStore{t: t}, emit: func(metrics map[string]float64, fields map[string]string) {
		if metrics["ItemDeadlineExceeded"] == 1 {
			deadlineEvents.Add(1)
			if fields["feed_id"] != "feed" || fields["item_id"] != "slow" {
				t.Errorf("deadline attribution = %#v", fields)
			}
		}
	}}
	body := func(id string) string {
		return `{"user":"user","feed_id":"feed","item_id":"` + id + `","published_ts":"` + domain.Timestamp(time.Now()) + `"}`
	}
	// Exercise an actual per-item timer without waiting for the production budget.
	if _, err := h.processWithinDeadline(context.Background(), body("slow"), time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("per-item timeout = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	response, err := h.run(ctx, events.SQSEvent{Records: []events.SQSMessage{
		{MessageId: "slow-message", Body: body("slow")},
		{MessageId: "healthy-message", Body: body("healthy")},
	}})
	if err != nil || len(response.BatchItemFailures) != 1 || response.BatchItemFailures[0].ItemIdentifier != "slow-message" {
		t.Fatalf("batch response = %#v, error %v", response, err)
	}
	if deadlineEvents.Load() != 2 {
		t.Fatalf("deadline events = %d, want 2", deadlineEvents.Load())
	}
}

func TestExtractionMetricsForAggregatorFeeds(t *testing.T) {
	for _, feed := range []domain.Feed{
		{FeedID: "reddit", Connector: domain.ConnectorReddit},
		{FeedID: "hn", Connector: "hackernews"},
		{FeedID: "techmeme", Connector: domain.ConnectorRSS, NoBodyExpected: true},
	} {
		for _, hasContent := range []bool{false, true} {
			fields := map[string]any{}
			emitItemMetrics(map[string]float64{"ItemsWritten": 1, "BodyImageFailed": 2}, feed, hasContent, hasContent, func(metrics map[string]float64, dimensions map[string]string) {
				for name, value := range observability.Event(metrics, dimensions) {
					fields[name] = value
				}
			})
			if fields["ExtractionFailed"] != nil || fields["MediaFailed"] != nil || fields["ExtractionNotExpected"] != float64(1) || fields["FeedID"] != feed.FeedID || fields["BodyImageFailed"] != float64(2) {
				t.Fatalf("aggregator %s fields = %#v", feed.FeedID, fields)
			}
			if hasContent && (fields["ExtractionSucceeded"] != float64(1) || fields["MediaSucceeded"] != float64(1)) {
				t.Fatalf("successful content lost: %#v", fields)
			}
		}
	}
	if observability.ExtractedMetrics["ExtractionNotExpected"] || observability.ExtractedMetrics["ItemDeadlineExceeded"] {
		t.Fatal("diagnostic fields must not become custom metrics")
	}
}
