package main

import (
	"context"
	"errors"
	"testing"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/vectorstore"
)

type fakeImageSignalStore struct {
	signals          []domain.Signal
	behaviours       []domain.Behaviour
	items            map[string]domain.Item
	archives         map[string]domain.Item
	objects          map[string][]byte
	contentReads     []string
	signalUpdates    int
	behaviourUpdates int
	itemUpdates      int
	liveLoads        int
	archiveLoads     int
}

func (*fakeImageSignalStore) UserIDs(context.Context) ([]string, error) {
	return []string{"user"}, nil
}
func (f *fakeImageSignalStore) Signals(context.Context, string) ([]domain.Signal, error) {
	return append([]domain.Signal(nil), f.signals...), nil
}
func (f *fakeImageSignalStore) Behaviours(context.Context, string) ([]domain.Behaviour, error) {
	return append([]domain.Behaviour(nil), f.behaviours...), nil
}
func (f *fakeImageSignalStore) LiveItems(context.Context, string) ([]domain.Item, error) {
	f.liveLoads++
	items := make([]domain.Item, 0, len(f.items))
	for _, item := range f.items {
		items = append(items, item)
	}
	return items, nil
}
func (f *fakeImageSignalStore) ArchiveItems(context.Context, string) ([]domain.Item, error) {
	f.archiveLoads++
	items := make([]domain.Item, 0, len(f.archives))
	for _, item := range f.archives {
		items = append(items, item)
	}
	return items, nil
}
func (f *fakeImageSignalStore) Content(_ context.Context, key string) ([]byte, string, error) {
	f.contentReads = append(f.contentReads, key)
	body, ok := f.objects[key]
	if !ok {
		return nil, "", errors.New("missing object")
	}
	return append([]byte(nil), body...), "image/jpeg", nil
}
func (f *fakeImageSignalStore) UpdateSignalImageEmbedding(context.Context, string, string, []byte, string) error {
	f.signalUpdates++
	return nil
}
func (f *fakeImageSignalStore) UpdateBehaviourImageEmbedding(context.Context, string, string, []byte, string) error {
	f.behaviourUpdates++
	return nil
}
func (f *fakeImageSignalStore) SetItemImageVector(context.Context, string, string, []byte, string) error {
	f.itemUpdates++
	return nil
}

type fakeImageEmbedder struct {
	calls int
	jpeg  []byte
}

func (f *fakeImageEmbedder) EmbedImage(_ context.Context, jpeg []byte) ([]float32, error) {
	f.calls++
	f.jpeg = append([]byte(nil), jpeg...)
	return []float32{3, 4}, nil
}
func (*fakeImageEmbedder) EmbedText(context.Context, string) ([]float32, error) {
	return nil, errors.New("unexpected text embedding")
}

type fakeImageVectors struct {
	calls   int
	records []vectorstore.Record
}

func (f *fakeImageVectors) Put(_ context.Context, record vectorstore.Record) error {
	f.calls++
	f.records = append(f.records, record)
	return nil
}

func TestSelectedVariantKey(t *testing.T) {
	tests := []struct {
		name     string
		variants []domain.MediaVariant
		fallback string
		want     string
	}{
		{name: "largest at most 768", variants: []domain.MediaVariant{{Key: "1280", Width: 1280}, {Key: "384", Width: 384}, {Key: "768", Width: 768}}, want: "768"},
		{name: "smallest when all too large", variants: []domain.MediaVariant{{Key: "1280", Width: 1280}, {Key: "900", Width: 900}}, want: "900"},
		{name: "media fallback", fallback: "lead", want: "lead"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := selectedVariantKey(test.variants, test.fallback); got != test.want {
				t.Fatalf("key = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDryRunReportsEligibilityWithoutReadingOrWriting(t *testing.T) {
	item := domain.Item{ItemID: "photo", MediaKey: "lead", MediaVariants: []domain.MediaVariant{{Key: "lead-768", Width: 768}}}
	repository := &fakeImageSignalStore{
		signals: []domain.Signal{{ItemID: "photo"}}, items: map[string]domain.Item{"photo": item}, objects: map[string][]byte{"lead-768": {1}},
	}
	embedder, vectors := &fakeImageEmbedder{}, &fakeImageVectors{}
	result, err := run(context.Background(), repository, embedder, vectors, "image-v1", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned != 1 || result.Eligible != 1 || result.Embedded != 0 || result.Failed != 0 {
		t.Fatalf("report = %#v", result)
	}
	if len(repository.contentReads) != 0 || embedder.calls != 0 || vectors.calls != 0 || repository.signalUpdates != 0 || repository.itemUpdates != 0 {
		t.Fatalf("dry-run performed work: store %#v, embed calls %d, vector calls %d", repository, embedder.calls, vectors.calls)
	}
	if repository.liveLoads != 1 || repository.archiveLoads != 1 {
		t.Fatalf("item loads = live %d, archive %d; want one each", repository.liveLoads, repository.archiveLoads)
	}
}

func TestRunIndexesLiveAndArchiveItemsOncePerUser(t *testing.T) {
	repository := &fakeImageSignalStore{
		signals: []domain.Signal{{ItemID: "live"}, {ItemID: "archive"}, {ItemID: "missing-one"}, {ItemID: "missing-two"}},
		items: map[string]domain.Item{
			"live": {ItemID: "live", MediaKey: "live-lead"},
		},
		archives: map[string]domain.Item{
			"archive": {ItemID: "archive", MediaKey: "archive-lead"},
		},
	}
	result, err := run(context.Background(), repository, &fakeImageEmbedder{}, &fakeImageVectors{}, "image-v1", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned != 4 || result.Eligible != 2 || result.Failed != 2 {
		t.Fatalf("report = %#v", result)
	}
	if repository.liveLoads != 1 || repository.archiveLoads != 1 {
		t.Fatalf("item loads = live %d, archive %d; want one each", repository.liveLoads, repository.archiveLoads)
	}
}

func TestApplyEmbedsOnceAndUpdatesSignalBehaviourItemAndIndex(t *testing.T) {
	item := domain.Item{
		ItemID: "photo", FeedID: "feed", Title: "Photo", MediaKey: "lead-1280",
		MediaVariants: []domain.MediaVariant{{Key: "lead-384", Width: 384}, {Key: "lead-768", Width: 768}, {Key: "lead-1280", Width: 1280}}, TTL: 42,
	}
	repository := &fakeImageSignalStore{
		signals: []domain.Signal{{ItemID: "photo"}, {ItemID: "done", ImageVector: []byte{1}}}, behaviours: []domain.Behaviour{{ItemID: "photo"}},
		items: map[string]domain.Item{"photo": item}, objects: map[string][]byte{"lead-768": {7, 6, 8}},
	}
	embedder, vectors := &fakeImageEmbedder{}, &fakeImageVectors{}
	result, err := run(context.Background(), repository, embedder, vectors, "image-v1", true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned != 3 || result.Eligible != 1 || result.Embedded != 1 || result.Failed != 0 {
		t.Fatalf("report = %#v", result)
	}
	if embedder.calls != 1 || string(embedder.jpeg) != string([]byte{7, 6, 8}) || repository.signalUpdates != 1 || repository.behaviourUpdates != 1 || repository.itemUpdates != 1 {
		t.Fatalf("apply calls = embed %d jpeg %v signal %d behaviour %d item %d", embedder.calls, embedder.jpeg, repository.signalUpdates, repository.behaviourUpdates, repository.itemUpdates)
	}
	if vectors.calls != 1 || len(vectors.records) != 1 || vectors.records[0].Kind != vectorstore.KindLive || vectors.records[0].Key != "photo" {
		t.Fatalf("vector records = %#v", vectors.records)
	}
}

func TestRunSkipsVideosAndItemsWithoutMedia(t *testing.T) {
	repository := &fakeImageSignalStore{
		signals: []domain.Signal{{ItemID: "video"}, {ItemID: "plain"}},
		items: map[string]domain.Item{
			"video": {ItemID: "video", MediaKey: "thumbnail", VideoID: "abc"},
			"plain": {ItemID: "plain"},
		},
	}
	result, err := run(context.Background(), repository, &fakeImageEmbedder{}, &fakeImageVectors{}, "image-v1", true)
	if err != nil || result.SkippedVideo != 1 || result.SkippedNoMedia != 1 || result.Eligible != 0 || result.Embedded != 0 {
		t.Fatalf("report = %#v, err = %v", result, err)
	}
}
