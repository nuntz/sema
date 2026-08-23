package main

import (
	"testing"

	"github.com/nuntz/sema/internal/domain"
)

func TestReplayMessagesCarryForceFlagsWithoutUserState(t *testing.T) {
	items := []domain.Item{{
		ItemID: "item", FeedID: "feed", URL: "https://example.com/story", Title: "Title", Author: "Author", PublishedTS: "2026-08-20T12:00:00Z",
		Read: true, Signal: 1, Hearted: true, ArchiveSK: "A#kept", HeartedTS: "2026-08-20T13:00:00Z",
	}}
	messages := replayMessages("user", items, true, true)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	got := messages[0]
	if !got.Reprocess || !got.ForceExtract || !got.ForceSummary || got.ItemID != "item" || got.User != "user" {
		t.Fatalf("replay message = %#v", got)
	}
}
