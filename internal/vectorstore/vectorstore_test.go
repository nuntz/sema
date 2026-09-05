package vectorstore

import (
	"testing"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
)

func TestFromItemLifecycleMetadata(t *testing.T) {
	item := domain.Item{ItemID: "item", FeedID: "feed", Title: "Title", PublishedTS: "2026-08-20T00:00:00Z", TTL: 42, Vector: score.EncodeVector([]float32{1, 2}), ImageVector: score.EncodeVector([]float32{3, 4})}
	live := FromItem(item, KindLive)
	archive := FromItem(item, KindArchive)
	if live.ExpiresTS != 42 || live.Kind != KindLive || archive.ExpiresTS != 0 || archive.Kind != KindArchive {
		t.Fatalf("live = %#v, archive = %#v", live, archive)
	}
	imageLive, ok := ImageRecordFromItem(item, KindLive)
	imageArchive, archiveOK := ImageRecordFromItem(item, KindArchive)
	if !ok || !archiveOK || imageLive.ExpiresTS != 42 || imageArchive.ExpiresTS != 0 || len(imageLive.Data) != 2 || imageLive.Data[0] != 3 {
		t.Fatalf("image live = %#v (%v), archive = %#v (%v)", imageLive, ok, imageArchive, archiveOK)
	}
	if _, ok := ImageRecordFromItem(domain.Item{}, KindLive); ok {
		t.Fatal("empty image embedding produced a record")
	}
}

func TestSimilarityFromCosineDistance(t *testing.T) {
	if Similarity(0.09) != 91 || Similarity(2) != 0 || Similarity(-1) != 100 {
		t.Fatalf("unexpected similarities: %d %d %d", Similarity(.09), Similarity(2), Similarity(-1))
	}
}
