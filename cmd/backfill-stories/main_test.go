package main

import (
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
	storycluster "github.com/nuntz/sema/internal/story"
)

func TestBuildClustersPreservesExistingStoryAndIsIdempotent(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	vector := score.EncodeVector([]float32{1, 0})
	items := []domain.Item{
		{ItemID: "existing", StoryID: "founder", FeedID: "a", PublishedTS: domain.Timestamp(now), Vector: vector},
		{ItemID: "founder", StoryID: "founder", FeedID: "b", PublishedTS: domain.Timestamp(now.Add(-time.Hour)), Vector: vector},
		{ItemID: "new", FeedID: "c", PublishedTS: domain.Timestamp(now.Add(time.Hour)), Vector: vector},
	}
	config := storycluster.Config{Threshold: 80, Window: 72 * time.Hour}
	first := buildClusters(items, config)
	if len(first) != 1 || first[0].StoryID != "founder" || len(first[0].Members) != 3 {
		t.Fatalf("clusters = %#v", first)
	}
	for index := range items {
		items[index].StoryID = "founder"
	}
	second := buildClusters(items, config)
	if len(second) != 1 || second[0].StoryID != first[0].StoryID || len(second[0].Members) != len(first[0].Members) {
		t.Fatalf("second clusters = %#v", second)
	}
}

func TestBuildClustersUsesQualificationRules(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	items := []domain.Item{
		{ItemID: "a", FeedID: "a", URL: "https://example.com/a", PublishedTS: domain.Timestamp(now), Vector: score.EncodeVector([]float32{1, 0})},
		{ItemID: "b", FeedID: "b", URL: "https://example.com/b", PublishedTS: domain.Timestamp(now), Vector: score.EncodeVector([]float32{.9, .1})},
		{ItemID: "old", FeedID: "c", URL: "https://example.com/old", PublishedTS: domain.Timestamp(now.Add(-73 * time.Hour)), Vector: score.EncodeVector([]float32{1, 0})},
	}
	clusters := buildClusters(items, storycluster.Config{Threshold: 80, Window: 72 * time.Hour})
	if len(clusters) != 1 || len(clusters[0].Members) != 2 {
		t.Fatalf("clusters = %#v", clusters)
	}
}
