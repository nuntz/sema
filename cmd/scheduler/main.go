package main

import (
	"context"
	"encoding/json"
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

const dueMargin = 5 * time.Minute

type handler struct {
	store *store.Store
	queue *sqs.Client
	url   string
}

func (h *handler) run(ctx context.Context) error {
	started := time.Now()
	feeds, err := h.store.DueFeeds(ctx, started.UTC().Add(dueMargin))
	if err != nil {
		return err
	}
	for offset := 0; offset < len(feeds); offset += 10 {
		end := min(offset+10, len(feeds))
		entries := make([]types.SendMessageBatchRequestEntry, 0, end-offset)
		for i, feed := range feeds[offset:end] {
			message, _ := json.Marshal(domain.FeedMessage{User: feed.PK[2:], FeedID: feed.FeedID})
			entries = append(entries, types.SendMessageBatchRequestEntry{Id: aws.String(fmt.Sprintf("feed-%d", offset+i)), MessageBody: aws.String(string(message))})
		}
		response, err := h.queue.SendMessageBatch(ctx, &sqs.SendMessageBatchInput{QueueUrl: aws.String(h.url), Entries: entries})
		if err != nil {
			return err
		}
		if len(response.Failed) > 0 {
			return fmt.Errorf("failed to enqueue %d feeds: %s", len(response.Failed), aws.ToString(response.Failed[0].Message))
		}
		var group sync.WaitGroup
		errors := make(chan error, end-offset)
		for _, feed := range feeds[offset:end] {
			group.Add(1)
			go func(feed domain.Feed) {
				defer group.Done()
				if err := h.store.ScheduleFeed(ctx, feed.PK[2:], feed.FeedID, started.Add(time.Hour)); err != nil {
					errors <- err
				}
			}(feed)
		}
		group.Wait()
		close(errors)
		if err := <-errors; err != nil {
			return err
		}
	}
	slog.Info("scheduler complete", "feeds_enqueued", len(feeds), "duration_ms", time.Since(started).Milliseconds())
	observability.Emit(map[string]float64{"FeedsEnqueued": float64(len(feeds)), "SchedulerDurationMs": float64(time.Since(started).Milliseconds())}, nil)
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
