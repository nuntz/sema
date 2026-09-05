package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"time"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
	"github.com/nuntz/sema/internal/store"
	storycluster "github.com/nuntz/sema/internal/story"
)

type storyStore interface {
	UserIDs(context.Context) ([]string, error)
	LiveItems(context.Context, string) ([]domain.Item, error)
	LoadItemVectors(context.Context, string, []domain.Item) error
	Story(context.Context, string, string) (domain.Story, error)
	PutStory(context.Context, domain.Story) error
	SetItemStory(context.Context, domain.Item, string) error
}

type cluster struct {
	StoryID string
	Members []domain.Item
}

func main() {
	apply := flag.Bool("apply", false, "write story rows and item story IDs")
	flag.Parse()
	repository, _, err := store.FromEnv(context.Background())
	if err != nil {
		panic(err)
	}
	if err := run(context.Background(), repository, storycluster.FromEnv(), *apply); err != nil {
		panic(err)
	}
}

func run(ctx context.Context, repository storyStore, config storycluster.Config, apply bool) error {
	users, err := repository.UserIDs(ctx)
	if err != nil {
		return err
	}
	mode := "dry-run"
	if apply {
		mode = "applied"
	}
	for _, userID := range users {
		items, err := repository.LiveItems(ctx, userID)
		if err != nil {
			return fmt.Errorf("list live items for %s: %w", userID, err)
		}
		if err := repository.LoadItemVectors(ctx, userID, items); err != nil {
			return fmt.Errorf("load vectors for %s: %w", userID, err)
		}
		clusters := buildClusters(items, config)
		members := 0
		for _, group := range clusters {
			members += len(group.Members)
			if !apply {
				continue
			}
			createdAt := domain.Timestamp(time.Now())
			if existing, storyErr := repository.Story(ctx, userID, group.StoryID); storyErr == nil {
				if existing.CreatedAt != "" {
					createdAt = existing.CreatedAt
				}
			} else if !errors.Is(storyErr, store.ErrNotFound) {
				return fmt.Errorf("load story %s/%s: %w", userID, group.StoryID, storyErr)
			}
			memberIDs := make([]string, 0, len(group.Members))
			var ttl int64
			for _, item := range group.Members {
				memberIDs = append(memberIDs, item.ItemID)
				ttl = max(ttl, item.TTL)
			}
			now := domain.Timestamp(time.Now())
			row := domain.Story{
				PK: domain.UserPK(userID), SK: domain.StorySK(group.StoryID), StoryID: group.StoryID,
				MemberIDs: memberIDs, CreatedAt: createdAt, UpdatedAt: now, TTL: ttl,
			}
			if err := repository.PutStory(ctx, row); err != nil {
				return fmt.Errorf("put story %s/%s: %w", userID, group.StoryID, err)
			}
			for _, item := range group.Members {
				if item.StoryID == group.StoryID {
					continue
				}
				if err := repository.SetItemStory(ctx, item, group.StoryID); err != nil {
					return fmt.Errorf("set story for %s/%s: %w", userID, item.ItemID, err)
				}
			}
		}
		fmt.Fprintf(os.Stdout, "mode=%s user=%s live=%d stories=%d members=%d\n", mode, userID, len(items), len(clusters), members)
	}
	return nil
}

func buildClusters(items []domain.Item, config storycluster.Config) []cluster {
	assigned := make(map[string]string, len(items))
	processed := make([]domain.Item, 0, len(items))
	existing := append([]domain.Item(nil), items...)
	sort.SliceStable(existing, func(i, j int) bool {
		iAssigned, jAssigned := existing[i].StoryID != "", existing[j].StoryID != ""
		if iAssigned != jAssigned {
			return iAssigned
		}
		if existing[i].PublishedTS != existing[j].PublishedTS {
			return existing[i].PublishedTS < existing[j].PublishedTS
		}
		return existing[i].ItemID < existing[j].ItemID
	})
	for _, item := range existing {
		if len(item.Vector) == 0 {
			continue
		}
		if item.StoryID != "" {
			assigned[item.ItemID] = item.StoryID
			processed = append(processed, item)
			continue
		}
		candidates := qualifyingCandidates(item, processed, config, assigned)
		if len(candidates) == 0 {
			processed = append(processed, item)
			continue
		}
		storyID, found := storycluster.Choose(candidates)
		if !found {
			sort.SliceStable(candidates, func(i, j int) bool {
				if candidates[i].Similarity != candidates[j].Similarity {
					return candidates[i].Similarity > candidates[j].Similarity
				}
				return candidates[i].Item.ItemID < candidates[j].Item.ItemID
			})
			storyID = candidates[0].Item.ItemID
			assigned[candidates[0].Item.ItemID] = storyID
			for index := range processed {
				if processed[index].ItemID == candidates[0].Item.ItemID {
					processed[index].StoryID = storyID
					break
				}
			}
		}
		assigned[item.ItemID] = storyID
		item.StoryID = storyID
		processed = append(processed, item)
	}
	byStory := make(map[string][]domain.Item)
	byID := make(map[string]domain.Item, len(items))
	for _, item := range items {
		byID[item.ItemID] = item
	}
	for itemID, storyID := range assigned {
		item := byID[itemID]
		item.StoryID = storyID
		byStory[storyID] = append(byStory[storyID], item)
	}
	clusters := make([]cluster, 0, len(byStory))
	for storyID, members := range byStory {
		if len(members) < 2 {
			continue
		}
		sort.Slice(members, func(i, j int) bool { return members[i].ItemID < members[j].ItemID })
		clusters = append(clusters, cluster{StoryID: storyID, Members: members})
	}
	sort.Slice(clusters, func(i, j int) bool { return clusters[i].StoryID < clusters[j].StoryID })
	return clusters
}

func qualifyingCandidates(item domain.Item, processed []domain.Item, config storycluster.Config, assigned map[string]string) []storycluster.Candidate {
	vector := score.DecodeVector(item.Vector)
	candidates := make([]storycluster.Candidate, 0)
	for _, other := range processed {
		otherVector := score.DecodeVector(other.Vector)
		if len(otherVector) == 0 || len(otherVector) != len(vector) {
			continue
		}
		similarity := int(math.Round(score.Dot(vector, otherVector) * 100))
		other.StoryID = assigned[other.ItemID]
		if storycluster.Qualifies(other, item, similarity, config) {
			candidates = append(candidates, storycluster.Candidate{Item: other, Similarity: similarity})
		}
	}
	return candidates
}
