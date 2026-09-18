package ingest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/extract"
	"github.com/nuntz/sema/internal/media"
)

type extractionStage func(ExtractionInput) (extract.Result, error)

func (f extractionStage) Extract(input ExtractionInput) (extract.Result, error) { return f(input) }

type scoringStage func(context.Context, ScoringInput) (ScoringOutput, error)

func (f scoringStage) Score(ctx context.Context, input ScoringInput) (ScoringOutput, error) {
	return f(ctx, input)
}

type modelStage func(context.Context, string) (domain.Model, error)

func (f modelStage) Get(ctx context.Context, user string) (domain.Model, error) { return f(ctx, user) }

func TestInjectedStagesBuildStoredItem(t *testing.T) {
	repository := &recordingItemStore{}
	extracted, scored := false, false
	processor := Processor{
		Store: repository, Media: media.New(nil), Embedder: stubEmbedder{}, ScoringVersion: "2",
		Extraction: extractionStage(func(input ExtractionInput) (extract.Result, error) {
			extracted = true
			if input.RawContent != "stage input" {
				t.Fatalf("extraction input = %#v", input)
			}
			return extract.Result{HTML: "<p>Injected article</p>", Text: "Injected article", FirstParagraph: "Injected article", Quality: 0.9, Author: "Stage author"}, nil
		}),
		Scoring: scoringStage(func(_ context.Context, input ScoringInput) (ScoringOutput, error) {
			scored = true
			if input.User != "user" || input.FeedID != "feed" || len(input.Vector) == 0 {
				t.Fatalf("scoring input = %#v", input)
			}
			return ScoringOutput{Value: 0.5, Model: domain.Model{ExplicitCount: 10, SizeCutoffs: &domain.SizeCutoffs{P60: 0.6, P90: 0.8}}}, nil
		}),
	}
	body := `{"user":"user","feed_id":"feed","item_id":"item","title":"Title","content_raw":"stage input","published_ts":"` + domain.Timestamp(time.Now().UTC()) + `"}`
	if _, err := processor.process(context.Background(), body); err != nil {
		t.Fatal(err)
	}
	if !extracted || !scored || len(repository.items) != 1 {
		t.Fatalf("extracted=%v scored=%v items=%d", extracted, scored, len(repository.items))
	}
	item := repository.items[0]
	if item.Author != "Stage author" || !item.HasBody || item.Score != 0.5 || item.Size != "S" {
		t.Fatalf("stored item = %#v", item)
	}
}

func TestRankingUsesInjectedModelLoader(t *testing.T) {
	failure := errors.New("model unavailable")
	ranking := Ranking{Models: modelStage(func(_ context.Context, user string) (domain.Model, error) {
		if user != "user" {
			t.Fatalf("user = %q", user)
		}
		return domain.Model{}, failure
	})}
	if _, err := ranking.Score(context.Background(), ScoringInput{User: "user"}); !errors.Is(err, failure) {
		t.Fatalf("error = %v", err)
	}
}
