package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/nuntz/sema/internal/observability"
	rankingrescore "github.com/nuntz/sema/internal/rescore"
	"github.com/nuntz/sema/internal/store"
	storycluster "github.com/nuntz/sema/internal/story"
)

type request struct {
	User     string `json:"user,omitempty"`
	OnDemand bool   `json:"on_demand,omitempty"`
}

type response struct {
	Users                int `json:"users"`
	ItemsRescored        int `json:"items_rescored"`
	ItemsSkippedNoVector int `json:"items_skipped_no_vector"`
	Skipped              int `json:"skipped"`
	StoriesConsolidated  int `json:"stories_consolidated"`
	StoriesDeleted       int `json:"stories_deleted"`
}

type handler struct {
	store  *store.Store
	engine *rankingrescore.Engine
}

func (h *handler) run(ctx context.Context, input request) (response, error) {
	users := []string{input.User}
	if input.User == "" {
		var err error
		users, err = h.store.UserIDs(ctx)
		if err != nil {
			return response{}, err
		}
	}
	output := response{}
	for _, userID := range users {
		result, err := h.engine.RunUser(ctx, userID, input.OnDemand)
		if errors.Is(err, rankingrescore.ErrReplayActive) {
			output.Skipped++
			slog.Info("rescore skipped during replay", "user", userID)
			continue
		}
		if err != nil {
			return output, err
		}
		output.Users++
		output.ItemsRescored += result.ItemsRescored
		output.ItemsSkippedNoVector += result.ItemsSkippedNoVector
		output.StoriesConsolidated += result.StoriesConsolidated
		output.StoriesDeleted += result.StoriesDeleted
		observability.Emit(map[string]float64{
			"RescoreDurationMs":           float64(result.Duration.Milliseconds()),
			"ItemsRescored":               float64(result.ItemsRescored),
			"RescoreItemsSkippedNoVector": float64(result.ItemsSkippedNoVector),
			"CentroidDrift":               result.CentroidDrift,
			"StoriesConsolidated":         float64(result.StoriesConsolidated),
			"StoriesDeleted":              float64(result.StoriesDeleted),
		}, map[string]string{"User": userID})
	}
	return output, nil
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	repository, _, err := store.FromEnv(context.Background())
	if err != nil {
		panic(err)
	}
	version := strings.TrimSpace(os.Getenv("MODEL_VERSION"))
	if version == "" {
		version = "amazon.titan-embed-text-v2:0"
	}
	engine := &rankingrescore.Engine{Repository: repository, Version: version, StoryConfig: storycluster.FromEnv()}
	lambda.Start((&handler{store: repository, engine: engine}).run)
}
