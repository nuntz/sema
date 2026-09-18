package ingest

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/extract"
	"github.com/nuntz/sema/internal/score"
)

// Extractor and Scorer are stage ports: pipeline tests can supply complete
// stage results without running article parsing or ranking persistence.
type Extractor interface {
	Extract(ExtractionInput) (extract.Result, error)
}
type ExtractionInput struct {
	RawContent, ItemURL, SiteURL string
	PageURL                      *url.URL
	PageHTML                     []byte
}
type ArticleExtractor struct{}

func (ArticleExtractor) Extract(input ExtractionInput) (extract.Result, error) {
	rawContent, itemURL, siteURL, pageURL, pageHTML := input.RawContent, input.ItemURL, input.SiteURL, input.PageURL, input.PageHTML
	if extract.Substantial(rawContent) {
		return extract.FeedContent(rawContent, pageURL)
	}
	if extract.IsLinkblogEntry(itemURL, siteURL, rawContent) {
		feedURL, err := url.Parse(siteURL)
		if err != nil {
			return extract.Result{}, fmt.Errorf("parse feed site URL: %w", err)
		}
		return extract.FeedContent(rawContent, feedURL)
	}
	if len(pageHTML) > 0 {
		article, err := extract.Article(pageHTML, pageURL)
		if err == nil && article.HTML != "" {
			return article, nil
		}
		if strings.TrimSpace(extract.PlainText(rawContent)) != "" {
			return extract.FeedContent(rawContent, pageURL)
		}
		return article, err
	}
	if strings.TrimSpace(extract.PlainText(rawContent)) != "" {
		return extract.FeedContent(rawContent, pageURL)
	}
	return extract.Result{}, nil
}

// ModelLoader permits a cached production loader or a plain model fixture.
type ModelLoader interface {
	Get(context.Context, string) (domain.Model, error)
}
type SignalLoader interface {
	Signals(context.Context, string) ([]domain.Signal, error)
}
type Scorer interface {
	Score(context.Context, ScoringInput) (ScoringOutput, error)
}
type ScoringInput struct {
	User, FeedID, FeedTitle string
	Vector                  []float32
	ImageVector             []byte
	ImageVersion            string
	HasMedia                bool
	Published, Now          time.Time
}
type ScoringOutput struct {
	Value float64
	Why   *domain.Why
	Model domain.Model
}

// Ranking supplies the production scoring stage, including the legacy model
// and explanation lookups. The pipeline only consumes its completed result.
type Ranking struct {
	Signals                            SignalLoader
	Models                             ModelLoader
	Version, TextVersion, ImageVersion string
}

func (r Ranking) Score(ctx context.Context, input ScoringInput) (ScoringOutput, error) {
	value, why := 0.0, (*domain.Why)(nil)
	model := domain.Model{}
	if r.Version == "1" {
		rows, loadErr := r.Signals.Signals(ctx, input.User)
		if loadErr != nil {
			return ScoringOutput{}, loadErr
		}
		legacy := make([]score.Signal, 0, len(rows))
		for _, row := range rows {
			legacy = append(legacy, score.Signal{Value: row.Value, Vector: score.DecodeVector(row.Vector)})
		}
		value = score.LegacyCalculate(input.Vector, legacy, input.HasMedia, input.Published, input.Now)
	} else {
		loadedModel, loadErr := r.Models.Get(ctx, input.User)
		if loadErr != nil {
			return ScoringOutput{}, loadErr
		}
		model = loadedModel
		decodedImageVector := []float32(nil)
		if score.CompatibleVersion(input.ImageVersion, r.ImageVersion) {
			decodedImageVector = score.DecodeVector(input.ImageVector)
		}
		result := score.Calculate(input.Vector, decodedImageVector, model, input.FeedID, input.HasMedia, input.Now.Sub(input.Published).Hours())
		value = result.Score
		if result.Base > 0.6 {
			rows, signalErr := r.Signals.Signals(ctx, input.User)
			if signalErr != nil {
				return ScoringOutput{}, signalErr
			}
			liked := make([]score.Candidate, 0, len(rows))
			for _, row := range rows {
				if row.Value > 0 && score.CompatibleVersion(row.ModelVersion, r.TextVersion) {
					candidateImageVector := []float32(nil)
					if score.CompatibleVersion(row.ImageModelVersion, r.ImageVersion) {
						candidateImageVector = score.DecodeVector(row.ImageVector)
					}
					liked = append(liked, score.Candidate{Title: row.Title, Vector: score.DecodeVector(row.Vector), ImageVector: candidateImageVector})
				}
			}
			why = score.Why(result, input.Vector, decodedImageVector, input.FeedTitle, liked)
		}
	}
	return ScoringOutput{Value: value, Why: why, Model: model}, nil
}
