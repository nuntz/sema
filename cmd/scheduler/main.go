package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/observability"
	"github.com/nuntz/sema/internal/store"
)

const claimLease = 5 * time.Minute

type handler struct {
	store *store.Store
	queue *sqs.Client
	url   string
}

func (h *handler) run(ctx context.Context) error {
	started := time.Now()
	feeds, err := h.store.DueFeeds(ctx, started.UTC())
	if err != nil {
		return err
	}
	enqueued := 0
	for offset := 0; offset < len(feeds); offset += 10 {
		end := min(offset+10, len(feeds))
		claimed := make(chan domain.Feed, end-offset)
		var group sync.WaitGroup
		claimErrors := make(chan error, end-offset)
		for _, feed := range feeds[offset:end] {
			group.Add(1)
			go func(feed domain.Feed) {
				defer group.Done()
				ok, err := h.store.ClaimFeed(ctx, feed.PK[2:], feed.FeedID, started, started.Add(claimLease))
				if err != nil {
					claimErrors <- err
				} else if ok {
					claimed <- feed
				}
			}(feed)
		}
		group.Wait()
		close(claimed)
		close(claimErrors)
		var collected []error
		for err := range claimErrors {
			collected = append(collected, err)
		}
		if err := errors.Join(collected...); err != nil {
			return err
		}

		entries := make([]types.SendMessageBatchRequestEntry, 0, end-offset)
		for feed := range claimed {
			message, _ := json.Marshal(domain.FeedMessage{User: feed.PK[2:], FeedID: feed.FeedID})
			entries = append(entries, types.SendMessageBatchRequestEntry{Id: aws.String(fmt.Sprintf("feed-%d", offset+len(entries))), MessageBody: aws.String(string(message))})
		}
		if len(entries) == 0 {
			continue
		}
		response, err := h.queue.SendMessageBatch(ctx, &sqs.SendMessageBatchInput{QueueUrl: aws.String(h.url), Entries: entries})
		if err != nil {
			return err
		}
		if len(response.Failed) > 0 {
			return fmt.Errorf("failed to enqueue %d feeds: %s", len(response.Failed), aws.ToString(response.Failed[0].Message))
		}
		enqueued += len(entries)
	}
	slog.Info("scheduler complete", "feeds_enqueued", enqueued, "duration_ms", time.Since(started).Milliseconds())
	observability.Emit(map[string]float64{"FeedsEnqueued": float64(enqueued), "SchedulerDurationMs": float64(time.Since(started).Milliseconds())}, nil)
	return nil
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	repository, config, err := store.FromEnv(context.Background())
	if err != nil {
		panic(err)
	}
	queueURL := os.Getenv("FEEDS_QUEUE_URL")
	if queueURL == "" {
		panic("FEEDS_QUEUE_URL is required")
	}
	lambda.Start((&handler{store: repository, queue: sqs.NewFromConfig(config), url: queueURL}).run)
}
