package domain

import (
	"testing"
	"time"
)

func TestFetchWindowContains(t *testing.T) {
	from := time.Date(2026, 9, 7, 7, 0, 0, 0, time.UTC)
	window := FetchWindow{From: from, Before: from.AddDate(0, 0, 1)}
	for _, test := range []struct {
		timestamp string
		want      bool
	}{
		{"2026-09-07T06:59:59.999Z", false},
		{"2026-09-07T07:00:00Z", true},
		{"2026-09-07T00:00:00-07:00", true},
		{"2026-09-08T06:59:59.999Z", true},
		{"2026-09-08T07:00:00Z", false},
		{"", false},
		{"invalid", false},
	} {
		if got := window.Contains(test.timestamp); got != test.want {
			t.Errorf("Contains(%q) = %v, want %v", test.timestamp, got, test.want)
		}
		if !(FetchWindow{}).Contains(test.timestamp) {
			t.Errorf("unfiltered window excluded %q", test.timestamp)
		}
	}
}
