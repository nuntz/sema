package vectorstore

import (
	"testing"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
)

func TestFromItemLifecycleMetadata(t *testing.T) {
	item := domain.Item{ItemID: "item", FeedID: "feed", Title: "Title", PublishedTS: "2026-08-20T00:00:00Z", TTL: 42, Vector: score.EncodeVector([]float32{1, 2})}
	live := FromItem(item, KindLive)
	archive := FromItem(item, KindArchive)
	if live.ExpiresTS != 42 || live.Kind != KindLive || archive.ExpiresTS != 0 || archive.Kind != KindArchive {
		t.Fatalf("live = %#v, archive = %#v", live, archive)
	}
}

func TestSimilarityFromCosineDistance(t *testing.T) {
	if Similarity(0.09) != 91 || Similarity(2) != 0 || Similarity(-1) != 100 {
		t.Fatalf("unexpected similarities: %d %d %d", Similarity(.09), Similarity(2), Similarity(-1))
	}
}
