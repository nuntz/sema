package rescore

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
	storycluster "github.com/nuntz/sema/internal/story"
)

var ErrReplayActive = errors.New("embedding replay is still in progress")

type Repository interface {
	Model(context.Context, string) (domain.Model, error)
	PutModelIfUnchanged(context.Context, domain.Model, string) error
	RecomputeModel(context.Context, string, string, string) (domain.Model, error)
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
	Repository   Repository
	Version      string
	ImageVersion string
	Now          func() time.Time
	StoryConfig  storycluster.Config
}

type Result struct {
	Model                    domain.Model
	ItemsRescored            int
	ItemsSkippedBadTimestamp int
	ItemsSkippedNoVector     int
	CentroidDrift            float64
	StoriesConsolidated      int
	StoriesDeleted           int
	Duration                 time.Duration
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
	model, err := e.Repository.RecomputeModel(ctx, userID, e.Version, e.ImageVersion)
	if err != nil {
		return Result{}, err
	}
	computedAt := model.ComputedAt
	signals, err := e.Repository.Signals(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	feeds, err := e.Repository.Feeds(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	feedTitles := make(map[string]string, len(feeds))
	feedTags := make(map[string][]string, len(feeds))
	for _, feed := range feeds {
		feedTitles[feed.FeedID] = feed.Title
		seenTags := make(map[string]bool, len(feed.Tags))
		for _, rawTag := range feed.Tags {
			tag := strings.ToLower(strings.TrimSpace(rawTag))
			if tag == "" || seenTags[tag] {
				continue
			}
			seenTags[tag] = true
			feedTags[feed.FeedID] = append(feedTags[feed.FeedID], tag)
		}
	}
	candidates := make([]score.Candidate, 0, len(signals))
	for _, signal := range signals {
		if signal.Value > 0 && score.CompatibleVersion(signal.ModelVersion, e.Version) {
			imageVector := []float32(nil)
			if score.CompatibleVersion(signal.ImageModelVersion, e.ImageVersion) {
				imageVector = score.DecodeVector(signal.ImageVector)
			}
			candidates = append(candidates, score.Candidate{
				Title: signal.Title, FeedTitle: feedTitles[signal.FeedID], Vector: score.DecodeVector(signal.Vector), ImageVector: imageVector,
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
	itemsSkippedBadTimestamp := 0
	for _, item := range items {
		if len(item.Vector) == 0 {
			itemsSkippedNoVector++
			slog.Warn("rescore skipped item without vector", "user", userID, "item_id", item.ItemID)
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, item.PublishedTS); err != nil {
			itemsSkippedBadTimestamp++
			slog.Warn("rescore skipped item with invalid published timestamp", "user", userID, "item_id", item.ItemID, "error", err)
			continue
		}
		itemsWithVectors = append(itemsWithVectors, item)
	}
	items = itemsWithVectors
	scores := make([]float64, len(items))
	for index := range items {
		published, _ := time.Parse(time.RFC3339Nano, items[index].PublishedTS)
		vector := score.DecodeVector(items[index].Vector)
		imageVector := []float32(nil)
		if score.CompatibleVersion(items[index].ImageModelVersion, e.ImageVersion) {
			imageVector = score.DecodeVector(items[index].ImageVector)
		}
		calculated := score.Calculate(vector, imageVector, model, items[index].FeedID, items[index].MediaKey != "", started.Sub(published).Hours())
		items[index].Score = calculated.Score
		scores[index] = calculated.Score
		items[index].Why = score.Why(calculated, vector, imageVector, items[index].FeedTitle, candidates)
	}
	model.SizeCutoffs = score.QuantileCutoffs(scores)
	tagScores := make(map[string][]float64)
	for index, item := range items {
		for _, tag := range feedTags[item.FeedID] {
			tagScores[tag] = append(tagScores[tag], scores[index])
		}
	}
	model.TagSizeCutoffs = nil
	for tag, values := range tagScores {
		if len(values) < 10 {
			continue
		}
		if model.TagSizeCutoffs == nil {
			model.TagSizeCutoffs = make(map[string]*domain.SizeCutoffs)
		}
		model.TagSizeCutoffs[tag] = score.QuantileCutoffs(values)
	}
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
	model.ComputedAt = domain.Timestamp(e.now())
	if err := e.Repository.PutModelIfUnchanged(ctx, model, computedAt); err != nil {
		var conflict *types.ConditionalCheckFailedException
		if !errors.As(err, &conflict) {
			return Result{}, err
		}
		fresh, err := e.Repository.Model(ctx, userID)
		if err != nil {
			return Result{}, err
		}
		computedAt = fresh.ComputedAt
		fresh.SizeCutoffs, fresh.TagSizeCutoffs = model.SizeCutoffs, model.TagSizeCutoffs
		fresh.ReplayTS, fresh.ReplayVersion = "", ""
		fresh.ComputedAt = domain.Timestamp(e.now())
		model = fresh
		if err := e.Repository.PutModelIfUnchanged(ctx, model, computedAt); err != nil {
			return Result{}, err
		}
	}
	drift := centroidDrift(old.LikedCentroid, model.LikedCentroid)
	return Result{
		Model:                    model,
		ItemsRescored:            len(items),
		ItemsSkippedNoVector:     itemsSkippedNoVector,
		ItemsSkippedBadTimestamp: itemsSkippedBadTimestamp,
		CentroidDrift:            drift,
		StoriesConsolidated:      storiesConsolidated,
		StoriesDeleted:           storiesDeleted,
		Duration:                 e.now().Sub(started),
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
