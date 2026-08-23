package score

import (
	"math"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

func TestCalculateV2(t *testing.T) {
	unitX := []float32{1, 0}
	unitY := []float32{0, 1}
	tests := []struct {
		name  string
		model domain.Model
		media bool
		age   float64
		want  float64
	}{
		{name: "cold start", model: domain.Model{}, want: 0.5},
		{name: "cold start media", model: domain.Model{}, media: true, want: 0.55},
		{name: "taste only", model: domain.Model{ExplicitCount: 10, LikedCount: 5, LikedCentroid: EncodeVector(unitX)}, want: 1},
		{name: "disliked cap", model: domain.Model{ExplicitCount: 10, DislikedCount: 5, DislikedCentroid: EncodeVector(unitX)}, want: 0},
		{name: "prior only", model: domain.Model{ExplicitCount: 10, FeedPrior: map[string]float64{"feed": 0.1}}, want: 0.6},
		{name: "taste and prior cap", model: domain.Model{ExplicitCount: 10, LikedCount: 5, DislikedCount: 5, LikedCentroid: EncodeVector(unitX), DislikedCentroid: EncodeVector(unitY), FeedPrior: map[string]float64{"feed": 0.15}}, want: 1},
		{name: "old item", model: domain.Model{}, age: 96, want: 0.5 * (0.7 + 0.3*math.Exp(-2))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Calculate(unitX, test.model, "feed", test.media, test.age).Score
			if math.Abs(got-test.want) > 0.0001 {
				t.Fatalf("Calculate() = %f, want %f", got, test.want)
			}
		})
	}
}

func TestCalculatePolarityGateAtFiveSignals(t *testing.T) {
	vector := []float32{1, 0}
	centroid := EncodeVector(vector)
	tests := []struct {
		name  string
		model domain.Model
		want  float64
	}{
		{name: "liked off at four", model: domain.Model{ExplicitCount: 10, LikedCount: 4, LikedCentroid: centroid}, want: 0},
		{name: "liked on at five", model: domain.Model{ExplicitCount: 10, LikedCount: 5, LikedCentroid: centroid}, want: 1},
		{name: "disliked off at four", model: domain.Model{ExplicitCount: 10, DislikedCount: 4, DislikedCentroid: centroid}, want: 0},
		{name: "disliked on at five", model: domain.Model{ExplicitCount: 10, DislikedCount: 5, DislikedCentroid: centroid}, want: -1},
		{name: "twenty one up two down ignores disliked centroid", model: domain.Model{ExplicitCount: 23, LikedCount: 21, DislikedCount: 2, LikedCentroid: centroid, DislikedCentroid: centroid}, want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Calculate(vector, test.model, "", false, 0).Taste; got != test.want {
				t.Fatalf("taste = %f, want %f", got, test.want)
			}
		})
	}
}

func TestBuildModelCentroidsPriorsAndExplicitSupersedesImplicit(t *testing.T) {
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	x, y := EncodeVector([]float32{1, 0}), EncodeVector([]float32{0, 1})
	signals := []domain.Signal{
		{ItemID: "explicit", Value: -1, Vector: x, FeedID: "feed", CreatedAt: domain.Timestamp(now), ModelVersion: "v"},
		{ItemID: "liked", Value: 1, Vector: y, FeedID: "feed", CreatedAt: domain.Timestamp(now), ModelVersion: "v"},
	}
	behaviours := []domain.Behaviour{
		{ItemID: "explicit", OpenedAt: domain.Timestamp(now), Opened: true, DwellMS: 60_000, Vector: x, FeedID: "feed", ModelVersion: "v"},
		{ItemID: "implicit", OpenedAt: domain.Timestamp(now), Opened: true, ClickedThrough: true, Vector: y, FeedID: "feed", ModelVersion: "v"},
	}
	model := BuildModel("user", signals, behaviours, now, "v")
	if model.ExplicitCount != 2 || model.LikedCount != 1 || model.DislikedCount != 1 || model.ImplicitCount != 2 {
		t.Fatalf("counts = explicit %d liked %d disliked %d implicit %d", model.ExplicitCount, model.LikedCount, model.DislikedCount, model.ImplicitCount)
	}
	if got := Dot(DecodeVector(model.LikedCentroid), []float32{0, 1}); math.Abs(got-1) > 0.0001 {
		t.Fatalf("liked centroid = %v", DecodeVector(model.LikedCentroid))
	}
	if got := Dot(DecodeVector(model.DislikedCentroid), []float32{1, 0}); math.Abs(got-1) > 0.0001 {
		t.Fatalf("disliked centroid = %v", DecodeVector(model.DislikedCentroid))
	}
	wantPrior := 0.15 * math.Tanh((1-1+0.3*2)/5)
	if math.Abs(model.FeedPrior["feed"]-wantPrior) > 0.000001 || model.FeedSignalCount["feed"] != 4 {
		t.Fatalf("prior = %f (%d signals), want %f", model.FeedPrior["feed"], model.FeedSignalCount["feed"], wantPrior)
	}
}

func TestFeedPriorIsTimidAtLowCountsAndCapped(t *testing.T) {
	now := time.Now().UTC()
	makeLikes := func(count int) []domain.Signal {
		rows := make([]domain.Signal, count)
		for i := range rows {
			rows[i] = domain.Signal{ItemID: string(rune(i + 1)), Value: 1, Vector: EncodeVector([]float32{1}), FeedID: "feed", CreatedAt: domain.Timestamp(now)}
		}
		return rows
	}
	one := BuildModel("user", makeLikes(1), nil, now, "v").FeedPrior["feed"]
	many := BuildModel("user", makeLikes(1_000), nil, now, "v").FeedPrior["feed"]
	if !(one > 0 && one < 0.04) {
		t.Fatalf("one-signal prior = %f", one)
	}
	if many > 0.15 || many < 0.149 {
		t.Fatalf("capped prior = %f", many)
	}
}

func TestIncrementalExplicitUpdateMatchesFullRecompute(t *testing.T) {
	now := time.Now().UTC()
	x, y := EncodeVector([]float32{1, 0}), EncodeVector([]float32{0, 1})
	first := domain.Signal{ItemID: "first", Value: 1, Vector: x, FeedID: "feed", CreatedAt: domain.Timestamp(now), ModelVersion: "v"}
	implicit := domain.Behaviour{ItemID: "second", OpenedAt: domain.Timestamp(now), Opened: true, DwellMS: 35_000, Vector: y, FeedID: "feed", ModelVersion: "v"}
	model := BuildModel("user", []domain.Signal{first}, []domain.Behaviour{implicit}, now, "v")
	second := domain.Signal{ItemID: "second", Value: -1, Vector: y, FeedID: "feed", CreatedAt: domain.Timestamp(now), ModelVersion: "v"}
	if !ApplyExplicit(&model, nil, &second, &implicit, now.Add(time.Second)) {
		t.Fatal("incremental update unexpectedly requested full recompute")
	}
	full := BuildModel("user", []domain.Signal{first, second}, []domain.Behaviour{implicit}, now.Add(time.Second), "v")
	for _, comparison := range []struct {
		name string
		got  []byte
		want []byte
	}{
		{name: "liked", got: model.LikedCentroid, want: full.LikedCentroid},
		{name: "disliked", got: model.DislikedCentroid, want: full.DislikedCentroid},
	} {
		if Cosine(DecodeVector(comparison.got), DecodeVector(comparison.want)) < 0.999999 {
			t.Fatalf("%s centroid = %v, want %v", comparison.name, DecodeVector(comparison.got), DecodeVector(comparison.want))
		}
	}
	if model.ExplicitCount != full.ExplicitCount || model.LikedCount != full.LikedCount || model.DislikedCount != full.DislikedCount || model.ImplicitCount != full.ImplicitCount ||
		math.Abs(model.FeedPrior["feed"]-full.FeedPrior["feed"]) > 0.000001 {
		t.Fatalf("incremental model = %#v; full = %#v", model, full)
	}
}

func TestBuildModelNeverMixesEmbeddingVersions(t *testing.T) {
	now := time.Now().UTC()
	model := BuildModel("user", []domain.Signal{
		{ItemID: "legacy", Value: 1, Vector: EncodeVector([]float32{1, 0}), CreatedAt: domain.Timestamp(now)},
		{ItemID: "new", Value: 1, Vector: EncodeVector([]float32{0, 1}), CreatedAt: domain.Timestamp(now), ModelVersion: "new-model"},
	}, nil, now, "new-model")
	if got := DecodeVector(model.LikedCentroid); Dot(got, []float32{0, 1}) < 0.999999 {
		t.Fatalf("mixed-version centroid = %v", got)
	}
}

func TestWhyChoosesLikedItemOrFeed(t *testing.T) {
	vector := []float32{1, 0}
	candidates := []Candidate{{Title: "closest", Vector: []float32{1, 0}}, {Title: "other", Vector: []float32{0, 1}}}
	item := Why(Result{Base: 0.8, Taste: 0.3, Prior: 0.02}, vector, "Feed", candidates)
	if item == nil || item.Title != "closest" {
		t.Fatalf("item why = %#v", item)
	}
	feed := Why(Result{Base: 0.7, Taste: 0.05, Prior: 0.09}, vector, "Feed", candidates)
	if feed == nil || feed.Title != "" || feed.FeedTitle != "Feed" {
		t.Fatalf("feed why = %#v", feed)
	}
	if cold := Why(Result{Base: 0.6}, vector, "Feed", candidates); cold != nil {
		t.Fatalf("cold why = %#v", cold)
	}
}

func TestSize(t *testing.T) {
	for value, want := range map[float64]string{0.1: "S", 0.449: "S", 0.45: "M", 0.749: "M", 0.75: "L"} {
		if got := Size(value, domain.Model{}); got != want {
			t.Errorf("Size(%f) = %s, want %s", value, got, want)
		}
	}
}

func TestQuantileSizeBucketsTightDistribution(t *testing.T) {
	values := []float64{0.5000, 0.5001, 0.5002, 0.5003, 0.5004, 0.5005, 0.5006, 0.5007, 0.5008, 0.5009}
	model := domain.Model{ExplicitCount: 10, SizeCutoffs: QuantileCutoffs(values)}
	counts := map[string]int{}
	for _, value := range values {
		counts[Size(value, model)]++
	}
	if counts["S"] != 6 || counts["M"] != 3 || counts["L"] != 1 {
		t.Fatalf("bucket counts = %#v, want S=6 M=3 L=1", counts)
	}
}

func TestSizeFallsBackToFixedThresholds(t *testing.T) {
	cutoffs := &domain.SizeCutoffs{P60: 0.1, P90: 0.2}
	if got := Size(0.3, domain.Model{ExplicitCount: 9, SizeCutoffs: cutoffs}); got != "S" {
		t.Fatalf("low-signal size = %s, want fixed-threshold S", got)
	}
	if got := Size(0.5, domain.Model{ExplicitCount: 10}); got != "M" {
		t.Fatalf("missing-cutoff size = %s, want fixed-threshold M", got)
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
