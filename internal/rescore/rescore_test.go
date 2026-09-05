package rescore

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
)

type fakeRepository struct {
	model        domain.Model
	signals      []domain.Signal
	feeds        []domain.Feed
	items        []domain.Item
	recomputed   bool
	replacements []domain.Item
}

func (f *fakeRepository) Model(context.Context, string) (domain.Model, error) {
	if f.model.SK == "" {
		return domain.Model{}, score.ErrModelNotFound
	}
	return f.model, nil
}
func (f *fakeRepository) PutModel(_ context.Context, model domain.Model) error {
	f.model = model
	return nil
}
func (f *fakeRepository) RecomputeModel(_ context.Context, userID, version string) (domain.Model, error) {
	f.recomputed = true
	f.model = score.BuildModel(userID, f.signals, nil, time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC), version)
	return f.model, nil
}
func (f *fakeRepository) Signals(context.Context, string) ([]domain.Signal, error) {
	return f.signals, nil
}
func (f *fakeRepository) Feeds(context.Context, string) ([]domain.Feed, error) {
	return f.feeds, nil
}
func (f *fakeRepository) LiveItems(context.Context, string) ([]domain.Item, error) {
	return append([]domain.Item(nil), f.items...), nil
}
func (*fakeRepository) LoadItemVectors(context.Context, string, []domain.Item) error { return nil }
func (f *fakeRepository) UpdateItemRankings(_ context.Context, items []domain.Item) error {
	f.replacements = append([]domain.Item(nil), items...)
	return nil
}

func TestGoldenRescoreUpdatesScoreSizeAndWhy(t *testing.T) {
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	vector := score.EncodeVector([]float32{1, 0})
	signals := make([]domain.Signal, 10)
	for index := range signals {
		signals[index] = domain.Signal{
			ItemID: string(rune('a' + index)), Value: 1, Vector: vector, Title: "Liked story",
			FeedID: "feed", CreatedAt: domain.Timestamp(now), ModelVersion: "v",
		}
	}
	repository := &fakeRepository{
		model:   domain.Model{PK: "U#user", SK: "MODEL", Version: "v"},
		signals: signals,
		feeds:   []domain.Feed{{FeedID: "feed", Title: "Example Feed"}},
		items: []domain.Item{{
			PK: "U#user", SK: domain.ItemSK(now, "new"), ItemID: "new", FeedID: "feed", FeedTitle: "Example Feed",
			PublishedTS: domain.Timestamp(now), Vector: vector, Score: 0.1, Size: "S", TTL: now.Add(time.Hour).Unix(),
		}},
	}
	engine := Engine{Repository: repository, Version: "v", Now: func() time.Time { return now }}
	result, err := engine.RunUser(context.Background(), "user", true)
	if err != nil {
		t.Fatal(err)
	}
	if result.ItemsRescored != 1 || len(repository.replacements) != 1 {
		t.Fatalf("result = %#v, items = %#v", result, repository.replacements)
	}
	item := repository.replacements[0]
	if item.Score < 0.99 || item.Size != "L" || item.Why == nil || item.Why.Title != "Liked story" {
		t.Fatalf("rescored item = %#v", item)
	}
}

func TestNightlyGuardrailHonoursRecentReplay(t *testing.T) {
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	repository := &fakeRepository{model: domain.Model{
		PK: "U#user", SK: "MODEL", Version: "old", ReplayTS: domain.Timestamp(now.Add(-30 * time.Minute)),
	}}
	engine := Engine{Repository: repository, Version: "new", Now: func() time.Time { return now }}
	_, err := engine.RunUser(context.Background(), "user", false)
	if !errors.Is(err, ErrReplayActive) || repository.recomputed {
		t.Fatalf("guard result = %v, recomputed %v", err, repository.recomputed)
	}
	if _, err := engine.RunUser(context.Background(), "user", true); err != nil {
		t.Fatalf("on-demand rescore was blocked: %v", err)
	}
}

func TestRescoreSkipsMissingItemEmbedding(t *testing.T) {
	now := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
	vector := score.EncodeVector([]float32{1, 0})
	repository := &fakeRepository{
		model: domain.Model{PK: "U#user", SK: "MODEL", Version: "v"},
		items: []domain.Item{
			{
				PK: "U#user", SK: domain.ItemSK(now, "missing"), ItemID: "missing", FeedID: "feed",
				PublishedTS: domain.Timestamp(now), TTL: now.Add(time.Hour).Unix(),
			},
			{
				PK: "U#user", SK: domain.ItemSK(now, "normal"), ItemID: "normal", FeedID: "feed",
				PublishedTS: domain.Timestamp(now), Vector: vector, TTL: now.Add(time.Hour).Unix(),
			},
		},
	}
	engine := Engine{Repository: repository, Version: "v", Now: func() time.Time { return now }}
	result, err := engine.RunUser(context.Background(), "user", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.ItemsRescored != 1 || result.ItemsSkippedNoVector != 1 {
		t.Fatalf("result = %#v, want one rescored and one skipped", result)
	}
	if len(repository.replacements) != 1 || repository.replacements[0].ItemID != "normal" {
		t.Fatalf("ranked items = %#v, want only normal item", repository.replacements)
	}
}

func TestRescoreDerivesCutoffsFromFreshScoresBeforeBucketing(t *testing.T) {
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	vector := score.EncodeVector([]float32{1, 0})
	signals := make([]domain.Signal, 10)
	items := make([]domain.Item, 10)
	for index := range signals {
		signals[index] = domain.Signal{
			ItemID: string(rune('a' + index)), Value: 1, Vector: vector, FeedID: "feed",
			CreatedAt: domain.Timestamp(now), ModelVersion: "v",
		}
		published := now.Add(-time.Duration(index) * time.Hour)
		items[index] = domain.Item{
			PK: "U#user", SK: domain.ItemSK(published, string(rune('k'+index))), ItemID: string(rune('k' + index)),
			FeedID: "feed", PublishedTS: domain.Timestamp(published), Vector: vector, Score: 99, Size: "L",
		}
	}
	repository := &fakeRepository{
		model: domain.Model{PK: "U#user", SK: "MODEL", Version: "v"}, signals: signals, items: items,
	}
	engine := Engine{Repository: repository, Version: "v", Now: func() time.Time { return now }}
	if _, err := engine.RunUser(context.Background(), "user", true); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	fresh := make([]float64, len(repository.replacements))
	for index, item := range repository.replacements {
		fresh[index] = item.Score
		counts[item.Size]++
	}
	want := score.QuantileCutoffs(fresh)
	got := repository.model.SizeCutoffs
	if got == nil || math.Abs(got.P60-want.P60) > 1e-12 || math.Abs(got.P90-want.P90) > 1e-12 {
		t.Fatalf("stored cutoffs = %#v, want %#v from fresh scores", got, want)
	}
	if counts["S"] != 6 || counts["M"] != 3 || counts["L"] != 1 {
		t.Fatalf("bucket counts = %#v, want S=6 M=3 L=1", counts)
	}
}
