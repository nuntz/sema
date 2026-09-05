package main

import (
	"testing"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
)

func TestReplayMessagesCarryForceFlagsWithoutUserState(t *testing.T) {
	items := []domain.Item{{
		ItemID: "item", FeedID: "feed", URL: "https://reddit.com/comments/item", ExternalURL: "https://example.com/story", PostType: "link", Title: "Title", Author: "Author", PublishedTS: "2026-08-20T12:00:00Z",
		Read: true, Signal: 1, Hearted: true, ArchiveSK: "A#kept", HeartedTS: "2026-08-20T13:00:00Z",
	}}
	messages := replayMessages("user", items, true, true)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	got := messages[0]
	if !got.Reprocess || !got.ForceExtract || !got.ForceSummary || got.ItemID != "item" || got.User != "user" || got.ExternalURL != items[0].ExternalURL || got.PostType != "link" {
		t.Fatalf("replay message = %#v", got)
	}
}

func TestNeedsTextReembeddingOnlyForMissingOrChangedVectors(t *testing.T) {
	vector := score.EncodeVector([]float32{1, 0})
	tests := []struct {
		name       string
		vector     []byte
		rowVersion string
		target     string
		want       bool
	}{
		{name: "current", vector: vector, rowVersion: "v1", target: "v1"},
		{name: "legacy default", vector: vector, target: score.LegacyEmbeddingVersion},
		{name: "changed model", vector: vector, rowVersion: "v1", target: "v2", want: true},
		{name: "missing vector", rowVersion: "v1", target: "v1", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := needsTextReembedding(test.vector, test.rowVersion, test.target); got != test.want {
				t.Fatalf("needsTextReembedding = %v, want %v", got, test.want)
			}
		})
	}
}
