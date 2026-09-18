package ingest

import (
	"strings"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

func TestReplayPlanAndBuild(t *testing.T) {
	now := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	existing := domain.Item{PK: "U#user", SK: "I#original", TTL: now.Add(time.Hour).Unix(), Title: "Original", Vector: []byte{1}, ModelVersion: "v1", ImageVector: []byte{1}, ImageModelVersion: "image-v1", ArchiveSK: "A#kept", FetchedTS: "original-fetch", StoryID: "story"}
	message := domain.ItemMessage{Reprocess: true, PublishedTS: domain.Timestamp(now.Add(-6 * 24 * time.Hour))}
	work, err := Plan(message, existing, now, "v1", "image-v1")
	if err != nil || work.Expired || work.Text || work.Assets || !work.ReuseImage || work.Message.Title != "Original" {
		t.Fatalf("plan=%+v error=%v", work, err)
	}
	item := Build(work, existing, domain.Item{Title: "Updated", Summary: "Summary"}, now, "2", domain.Model{})
	if item.SK != "I#original" || item.TTL != existing.TTL || item.ArchiveSK != "A#kept" || item.FetchedTS != "original-fetch" || item.StoryID != "story" || item.SearchText != "updated summary" {
		t.Fatalf("item=%+v", item)
	}
	message.ForceExtract = true
	forced, _ := Plan(message, existing, now, "v1", "image-v1")
	if !forced.Assets || !forced.Text || forced.ReuseImage {
		t.Fatal("forced extraction reused vectors")
	}
	message.MediaType = "video"
	video, _ := Plan(message, existing, now, "v1", "image-v1")
	if video.ReuseImage || len(Build(video, existing, existing, now, "2", domain.Model{}).ImageVector) > 0 {
		t.Fatal("video retained image vector")
	}
}
func TestIngestBoundaries(t *testing.T) {
	now := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	work, err := Plan(domain.ItemMessage{PublishedTS: "2026-09-10T00:00:00Z"}, domain.Item{}, now, "", "")
	if err != nil || !work.Expired {
		t.Fatal("admitted expired item")
	}
	if HasBody(false, "body", 0.29) || !HasBody(false, "body", 0.3) || HasBody(true, "body", 1) || HasBody(false, "", 1) {
		t.Fatal("body quality admission")
	}
	input := TextInput(strings.Repeat("世", 2050), "", "")
	if len([]rune(input)) != 2048 {
		t.Fatal("embedding input must cap runes")
	}
}
