package rescore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
)

var ErrReplayActive = errors.New("embedding replay is still in progress")

type Repository interface {
	Model(context.Context, string) (domain.Model, error)
	PutModel(context.Context, domain.Model) error
	RecomputeModel(context.Context, string, string) (domain.Model, error)
	Signals(context.Context, string) ([]domain.Signal, error)
	Feeds(context.Context, string) ([]domain.Feed, error)
	LiveItems(context.Context, string) ([]domain.Item, error)
	LoadItemVectors(context.Context, string, []domain.Item) error
	ReplaceItems(context.Context, []domain.Item) error
}

type Engine struct {
	Repository Repository
	Version    string
	Now        func() time.Time
}

type Result struct {
	Model         domain.Model
	ItemsRescored int
	CentroidDrift float64
	Duration      time.Duration
}

func (e *Engine) RunUser(ctx context.Context, userID string, onDemand bool) (Result, error) {
	started := e.now()
	old, oldErr := e.Repository.Model(ctx, userID)
	if oldErr != nil && !errors.Is(oldErr, score.ErrModelNotFound) {
		return Result{}, oldErr
	}
	if !onDemand && replayRecent(old.ReplayTS, started) {
		return Result{}, ErrReplayActive
	}
	model, err := e.Repository.RecomputeModel(ctx, userID, e.Version)
	if err != nil {
		return Result{}, err
	}
	signals, err := e.Repository.Signals(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	feeds, err := e.Repository.Feeds(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	feedTitles := make(map[string]string, len(feeds))
	for _, feed := range feeds {
		feedTitles[feed.FeedID] = feed.Title
	}
	candidates := make([]score.Candidate, 0, len(signals))
	for _, signal := range signals {
		if signal.Value > 0 && score.CompatibleVersion(signal.ModelVersion, e.Version) {
			candidates = append(candidates, score.Candidate{
				Title: signal.Title, FeedTitle: feedTitles[signal.FeedID], Vector: score.DecodeVector(signal.Vector),
			})
		}
	}
	items, err := e.Repository.LiveItems(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	if err := e.Repository.LoadItemVectors(ctx, userID, items); err != nil {
		return Result{}, err
	}
	scores := make([]float64, len(items))
	for index := range items {
		published, parseErr := time.Parse(time.RFC3339Nano, items[index].PublishedTS)
		if parseErr != nil {
			return Result{}, fmt.Errorf("item %s published_ts: %w", items[index].ItemID, parseErr)
		}
		vector := score.DecodeVector(items[index].Vector)
		calculated := score.Calculate(vector, model, items[index].FeedID, items[index].MediaKey != "", started.Sub(published).Hours())
		items[index].Score = calculated.Score
		scores[index] = calculated.Score
		items[index].Why = score.Why(calculated, vector, items[index].FeedTitle, candidates)
	}
	model.SizeCutoffs = score.QuantileCutoffs(scores)
	for index := range items {
		items[index].Size = score.Size(items[index].Score, model)
	}
	if err := e.Repository.ReplaceItems(ctx, items); err != nil {
		return Result{}, err
	}
	model.ReplayTS, model.ReplayVersion = "", ""
	if err := e.Repository.PutModel(ctx, model); err != nil {
		return Result{}, err
	}
	drift := score.Dot(score.DecodeVector(old.LikedCentroid), score.DecodeVector(model.LikedCentroid))
	return Result{Model: model, ItemsRescored: len(items), CentroidDrift: drift, Duration: e.now().Sub(started)}, nil
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

func replayRecent(value string, now time.Time) bool {
	if value == "" {
		return false
	}
	started, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && now.Sub(started) >= 0 && now.Sub(started) < time.Hour
}
