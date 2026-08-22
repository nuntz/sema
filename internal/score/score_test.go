package score

import (
	"math"
	"testing"
	"time"
)

func TestCalculate(t *testing.T) {
	now := time.Now()
	unitX := []float32{1, 0}
	unitY := []float32{0, 1}
	tests := []struct {
		name      string
		signals   []Signal
		media     bool
		published time.Time
		want      float64
	}{
		{name: "cold start", want: 0.5, published: now},
		{name: "cold start media", media: true, want: 0.55, published: now},
		{name: "all liked", signals: repeat(Signal{1, unitX}, 10), want: 1, published: now},
		{name: "all disliked", signals: repeat(Signal{-1, unitX}, 10), want: 0, published: now},
		{name: "unrelated", signals: repeat(Signal{1, unitY}, 10), want: 0.5, published: now},
		{name: "old item", want: 0.5 * (0.7 + 0.3*math.Exp(-2)), published: now.Add(-96 * time.Hour)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Calculate(unitX, tt.signals, tt.media, tt.published, now)
			if math.Abs(got-tt.want) > 0.0001 {
				t.Fatalf("Calculate() = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestSize(t *testing.T) {
	for score, want := range map[float64]string{0.1: "S", 0.449: "S", 0.45: "M", 0.749: "M", 0.75: "L"} {
		if got := Size(score); got != want {
			t.Errorf("Size(%f) = %s, want %s", score, got, want)
		}
	}
}

func TestVectorRoundTrip(t *testing.T) {
	want := []float32{-1.25, 0, 3.5}
	got := DecodeVector(EncodeVector(want))
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("value %d = %f, want %f", i, got[i], want[i])
		}
	}
}

func repeat(signal Signal, count int) []Signal {
	result := make([]Signal, count)
	for i := range result {
		result[i] = signal
	}
	return result
}
