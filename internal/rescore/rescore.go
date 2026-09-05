package rescore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
	storycluster "github.com/nuntz/sema/internal/story"
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
	UpdateItemRankings(context.Context, []domain.Item) error
	Stories(context.Context, string) ([]domain.Story, error)
	PutStory(context.Context, domain.Story) error
	DeleteStory(context.Context, string, string) error
	SetItemStory(context.Context, domain.Item, string) error
}

type Engine struct {
	Repository  Repository
	Version     string
	Now         func() time.Time
	StoryConfig storycluster.Config
}

type Result struct {
	Model                domain.Model
	ItemsRescored        int
	ItemsSkippedNoVector int
	CentroidDrift        float64
	StoriesConsolidated  int
	StoriesDeleted       int
	Duration             time.Duration
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
	allLiveItems := append([]domain.Item(nil), items...)
	if err := e.Repository.LoadItemVectors(ctx, userID, items); err != nil {
		return Result{}, err
	}
	itemsWithVectors := items[:0]
	itemsSkippedNoVector := 0
	for _, item := range items {
		if len(item.Vector) == 0 {
			itemsSkippedNoVector++
			slog.Warn("rescore skipped item without vector", "user", userID, "item_id", item.ItemID)
			continue
		}
		itemsWithVectors = append(itemsWithVectors, item)
	}
	items = itemsWithVectors
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
	if err := e.Repository.UpdateItemRankings(ctx, items); err != nil {
		return Result{}, err
	}
	storiesConsolidated, storiesDeleted, err := e.consolidateStories(ctx, userID, allLiveItems, started)
	if err != nil {
		return Result{}, err
	}
	model.ReplayTS, model.ReplayVersion = "", ""
	if err := e.Repository.PutModel(ctx, model); err != nil {
		return Result{}, err
	}
	drift := centroidDrift(old.LikedCentroid, model.LikedCentroid)
	return Result{
		Model:                model,
		ItemsRescored:        len(items),
		ItemsSkippedNoVector: itemsSkippedNoVector,
		CentroidDrift:        drift,
		StoriesConsolidated:  storiesConsolidated,
		StoriesDeleted:       storiesDeleted,
		Duration:             e.now().Sub(started),
	}, nil
}

func (e *Engine) consolidateStories(ctx context.Context, userID string, liveItems []domain.Item, now time.Time) (int, int, error) {
	rows, err := e.Repository.Stories(ctx, userID)
	if err != nil {
		return 0, 0, err
	}
	live := make(map[string]domain.Item, len(liveItems))
	for _, item := range liveItems {
		live[item.ItemID] = item
	}
	consolidated, deleted := 0, 0
	for _, row := range rows {
		memberIDs := make([]string, 0, len(row.MemberIDs))
		seen := make(map[string]bool, len(row.MemberIDs))
		var ttl int64
		for _, itemID := range row.MemberIDs {
			item, ok := live[itemID]
			if !ok || seen[itemID] {
				continue
			}
			seen[itemID] = true
			memberIDs = append(memberIDs, itemID)
			ttl = max(ttl, item.TTL)
		}
		if len(memberIDs) < 2 {
			if err := e.Repository.DeleteStory(ctx, userID, row.StoryID); err != nil {
				return consolidated, deleted, err
			}
			for _, itemID := range memberIDs {
				item := live[itemID]
				if item.StoryID == row.StoryID {
					if err := e.Repository.SetItemStory(ctx, item, ""); err != nil {
						return consolidated, deleted, err
					}
				}
			}
			deleted++
			continue
		}
		row.MemberIDs, row.TTL, row.UpdatedAt = memberIDs, ttl, domain.Timestamp(now)
		if err := e.Repository.PutStory(ctx, row); err != nil {
			return consolidated, deleted, err
		}
		consolidated++
	}
	// Story merging is intentionally left to a follow-up pass; consolidation only removes stale membership.
	return consolidated, deleted, nil
}

func centroidDrift(old, current []byte) float64 {
	oldVector := score.DecodeVector(old)
	currentVector := score.DecodeVector(current)
	if len(oldVector) == 0 || len(currentVector) == 0 {
		return 0
	}
	return 1 - score.Dot(oldVector, currentVector)
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
