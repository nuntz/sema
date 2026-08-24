package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/embed"
	bedrockembed "github.com/nuntz/sema/internal/embed/bedrock"
	"github.com/nuntz/sema/internal/score"
	"github.com/nuntz/sema/internal/store"
)

type replay struct {
	store        *store.Store
	queue        *sqs.Client
	embedder     embed.Embedder
	queueURL     string
	version      string
	forceExtract bool
	forceSummary bool
	reindex      bool
}

func (r *replay) run(ctx context.Context) error {
	users, err := r.store.UserIDs(ctx)
	if err != nil {
		return err
	}
	totalItems, totalSignals := 0, 0
	for _, userID := range users {
		if r.reindex {
			items, err := r.store.LiveItems(ctx, userID)
			if err != nil {
				return err
			}
			for _, item := range items {
				if item.SearchText == domain.DeriveSearchText(item.Title, item.Summary) {
					continue
				}
				if err := r.store.UpdateSearchText(ctx, item); err != nil {
					return err
				}
				totalItems++
			}
			continue
		}
		if err := r.store.StartReplay(ctx, userID, r.version, time.Now().UTC()); err != nil {
			return err
		}
		signals, err := r.store.Signals(ctx, userID)
		if err != nil {
			return err
		}
		for _, signal := range signals {
			vector, embedErr := r.embedder.Embed(ctx, capRunes(strings.TrimSpace(signal.Title), 2048))
			if embedErr != nil {
				return fmt.Errorf("re-embed signal %s: %w", signal.ItemID, embedErr)
			}
			if err := r.store.UpdateSignalEmbedding(ctx, userID, signal.ItemID, score.EncodeVector(score.Normalize(vector)), r.version); err != nil {
				return err
			}
			totalSignals++
		}
		behaviours, err := r.store.Behaviours(ctx, userID)
		if err != nil {
			return err
		}
		for _, behaviour := range behaviours {
			vector, embedErr := r.embedder.Embed(ctx, capRunes(strings.TrimSpace(behaviour.Title), 2048))
			if embedErr != nil {
				return fmt.Errorf("re-embed behaviour %s: %w", behaviour.ItemID, embedErr)
			}
			if err := r.store.UpdateBehaviourEmbedding(ctx, userID, behaviour.ItemID, score.EncodeVector(score.Normalize(vector)), r.version); err != nil {
				return err
			}
			totalSignals++
		}
		items, err := r.store.LiveItems(ctx, userID)
		if err != nil {
			return err
		}
		messages := replayMessages(userID, items, r.forceExtract, r.forceSummary)
		if err := r.enqueue(ctx, messages); err != nil {
			return err
		}
		totalItems += len(messages)
	}
	if r.reindex {
		slog.Info("live search text reindexed", "users", len(users), "items_updated", totalItems)
		return nil
	}
	slog.Info("replay queued", "users", len(users), "signals_reembedded", totalSignals, "items_queued", totalItems, "model_version", r.version, "force_extract", r.forceExtract, "force_summary", r.forceSummary)
	return nil
}

func replayMessages(userID string, items []domain.Item, forceExtract, forceSummary bool) []domain.ItemMessage {
	messages := make([]domain.ItemMessage, 0, len(items))
	for _, item := range items {
		messages = append(messages, domain.ItemMessage{
			User: userID, FeedID: item.FeedID, ItemID: item.ItemID, URL: item.URL,
			Title: item.Title, Author: item.Author, PublishedTS: item.PublishedTS, DisplayDate: item.DisplayDate, Reprocess: true,
			ForceExtract: forceExtract, ForceSummary: forceSummary,
		})
	}
	return messages
}

func (r *replay) enqueue(ctx context.Context, messages []domain.ItemMessage) error {
	for offset := 0; offset < len(messages); offset += 10 {
		end := min(offset+10, len(messages))
		entries := make([]types.SendMessageBatchRequestEntry, 0, end-offset)
		for index, message := range messages[offset:end] {
			body, err := json.Marshal(message)
			if err != nil {
				return err
			}
			entries = append(entries, types.SendMessageBatchRequestEntry{
				Id: aws.String(fmt.Sprintf("replay-%d", offset+index)), MessageBody: aws.String(string(body)),
			})
		}
		output, err := r.queue.SendMessageBatch(ctx, &sqs.SendMessageBatchInput{QueueUrl: aws.String(r.queueURL), Entries: entries})
		if err != nil {
			return err
		}
		if output == nil {
			return fmt.Errorf("enqueue replay items: empty SQS response")
		}
		if len(output.Failed) > 0 {
			return fmt.Errorf("failed to enqueue %d replay items: %s", len(output.Failed), aws.ToString(output.Failed[0].Message))
		}
	}
	return nil
}

func capRunes(value string, count int) string {
	runes := []rune(value)
	if len(runes) <= count {
		return value
	}
	return string(runes[:count])
}

func main() {
	forceExtract := flag.Bool("force-extract", false, "re-run extraction and media for every live item")
	forceSummary := flag.Bool("force-summary", false, "re-run summary generation for every eligible live item")
	reindex := flag.Bool("reindex", false, "rewrite live derived attributes without extraction, embedding, or scoring")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx := context.Background()
	repository, config, err := store.FromEnv(ctx)
	if err != nil {
		panic(err)
	}
	queueURL := strings.TrimSpace(os.Getenv("ITEMS_QUEUE_URL"))
	if queueURL == "" && !*reindex {
		panic("ITEMS_QUEUE_URL is required")
	}
	version := strings.TrimSpace(os.Getenv("MODEL_VERSION"))
	if version == "" {
		version = "amazon.titan-embed-text-v2:0"
	}
	command := &replay{
		store: repository, queue: sqs.NewFromConfig(config), embedder: bedrockembed.NewWithModel(bedrockruntime.NewFromConfig(config), version),
		queueURL: queueURL, version: version, forceExtract: *forceExtract, forceSummary: *forceSummary, reindex: *reindex,
	}
	if err := command.run(ctx); err != nil {
		panic(err)
	}
}
