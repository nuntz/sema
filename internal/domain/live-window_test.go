package domain

import (
	"testing"
	"time"
)

func TestLiveWindow(t *testing.T) {
	published := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	if got := LiveWindowTTL(published); got != 1789646400 {
		t.Fatalf("expiration = %d", got)
	}
	for _, tc := range []struct {
		name          string
		item          Item
		live, archive bool
	}{
		{"live", Item{TTL: 101}, true, false},
		{"kept live", Item{TTL: 101, ArchiveSK: "A#kept"}, true, false},
		{"expires at boundary", Item{TTL: 100}, false, false},
		{"expired", Item{TTL: 99}, false, false},
		{"archive", Item{SK: "A#kept"}, false, true},
		{"legacy archive", Item{HeartedTS: "kept"}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if Live(tc.item, 100) != tc.live || IsArchive(tc.item) != tc.archive {
				t.Fatal("incorrect Live Window classification")
			}
		})
	}
}
