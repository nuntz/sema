package domain

import (
	"testing"
	"time"
)

func TestTimestampSortsChronologically(t *testing.T) {
	earlier := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	later := earlier.Add(500 * time.Millisecond)
	if Timestamp(earlier) >= Timestamp(later) {
		t.Fatalf("timestamps are not lexically sortable: %s >= %s", Timestamp(earlier), Timestamp(later))
	}
	if ItemSK(earlier, "z") >= ItemSK(later, "a") {
		t.Fatal("item sort keys are not chronological")
	}
}
