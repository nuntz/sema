package domain

import (
	"testing"
	"time"
)

func TestNextFeedFetchUsesStableHourlyPhase(t *testing.T) {
	started := time.Date(2026, 8, 23, 14, 20, 0, 0, time.UTC)
	first := NextFeedFetch("U#user#F#feed", started)
	if delay := first.Sub(started); delay < 30*time.Minute || delay >= 90*time.Minute {
		t.Fatalf("first delay = %v, want [30m, 90m)", delay)
	}

	second := NextFeedFetch("U#user#F#feed", first.Add(time.Minute))
	if second.Sub(first) != time.Hour {
		t.Fatalf("hourly phase moved: first %s, second %s", first, second)
	}
}

func TestNextFeedFetchDoesNotImmediatelyRepeatEarlyFetch(t *testing.T) {
	key := "early-feed"
	phase := StableOffset(key, time.Hour)
	phaseTime := time.Date(2026, 8, 23, 15, 0, 0, 0, time.UTC).Add(phase)
	started := phaseTime.Add(-time.Minute)

	if got := NextFeedFetch(key, started); got != phaseTime.Add(time.Hour) {
		t.Fatalf("next fetch = %s, want %s", got, phaseTime.Add(time.Hour))
	}
}

func TestNextFeedFetchRespectsConfiguredInterval(t *testing.T) {
	started := time.Date(2026, 8, 23, 14, 20, 0, 0, time.UTC)
	first := NextFeedFetch("six-hour-feed", started, 6)
	second := NextFeedFetch("six-hour-feed", first.Add(time.Minute), 6)
	if second.Sub(first) != 6*time.Hour {
		t.Fatalf("six-hour phase moved: first %s, second %s", first, second)
	}
	if got := FeedIntervalHours(Feed{}); got != 1 {
		t.Fatalf("default interval = %d, want 1", got)
	}
}

func TestNextFeedFetchSupportsRedditHotCadence(t *testing.T) {
	started := time.Date(2026, 8, 23, 14, 20, 0, 0, time.UTC)
	first := NextFeedFetch("reddit-hot", started, 3)
	second := NextFeedFetch("reddit-hot", first.Add(time.Minute), 3)
	if second.Sub(first) != 3*time.Hour {
		t.Fatalf("three-hour phase moved: first %s, second %s", first, second)
	}
	if got := FeedIntervalHours(Feed{FetchIntervalH: 3}); got != 3 {
		t.Fatalf("Reddit hot interval = %d, want 3", got)
	}
}

func TestStableOffset(t *testing.T) {
	const window = 5 * time.Minute
	first := StableOffset("feed", window)
	if first < 0 || first >= window {
		t.Fatalf("offset = %v, want [0, %v)", first, window)
	}
	if second := StableOffset("feed", window); second != first {
		t.Fatalf("offset changed from %v to %v", first, second)
	}
	if other := StableOffset("another-feed", window); other == first {
		t.Fatalf("sample feed offsets unexpectedly collide at %v", first)
	}
}
