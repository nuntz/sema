package domain

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// The web's inWindow runs the same cases, so the two can't drift apart.
func TestFetchWindowContains(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/fetch-window.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Window struct{ From, Before time.Time }
		Cases  []struct {
			Fetched  string
			InWindow bool
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	window := FetchWindow{From: fixture.Window.From, Before: fixture.Window.Before}
	for _, test := range fixture.Cases {
		if got := window.Contains(test.Fetched); got != test.InWindow {
			t.Errorf("Contains(%q) = %v, want %v", test.Fetched, got, test.InWindow)
		}
		if !(FetchWindow{}).Contains(test.Fetched) {
			t.Errorf("unfiltered window excluded %q", test.Fetched)
		}
	}
}
