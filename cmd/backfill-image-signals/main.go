package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/embed"
	"github.com/nuntz/sema/internal/embed/titanimage"
	"github.com/nuntz/sema/internal/score"
	"github.com/nuntz/sema/internal/store"
	"github.com/nuntz/sema/internal/vectorstore"
)

type imageSignalStore interface {
	UserIDs(context.Context) ([]string, error)
	Signals(context.Context, string) ([]domain.Signal, error)
	Behaviours(context.Context, string) ([]domain.Behaviour, error)
	LiveItems(context.Context, string) ([]domain.Item, error)
	ArchiveItems(context.Context, string) ([]domain.Item, error)
	Content(context.Context, string) ([]byte, string, error)
	UpdateSignalImageEmbedding(context.Context, string, string, []byte, string) error
	UpdateBehaviourImageEmbedding(context.Context, string, string, []byte, string) error
	SetItemImageVector(context.Context, string, string, []byte, string) error
}

type imageVectorWriter interface {
	Put(context.Context, vectorstore.Record) error
}

type report struct {
	Scanned        int
	Eligible       int
	Embedded       int
	SkippedVideo   int
	SkippedNoMedia int
	Failed         int
}

type target struct {
	signal    bool
	behaviour bool
}

type indexedItem struct {
	item domain.Item
	kind vectorstore.Kind
	live bool
}

func main() {
	apply := flag.Bool("apply", false, "embed images and update signal, behaviour, item-vector, and image-index rows")
	flag.Parse()
	ctx := context.Background()
	repository, awsConfig, err := store.FromEnv(ctx)
	if err != nil {
		panic(err)
	}
	bucket := strings.TrimSpace(os.Getenv("VECTOR_BUCKET"))
	index := strings.TrimSpace(os.Getenv("IMAGE_VECTOR_INDEX"))
	if bucket == "" || index == "" {
		panic("VECTOR_BUCKET and IMAGE_VECTOR_INDEX are required")
	}
	version := strings.TrimSpace(os.Getenv("IMAGE_MODEL_VERSION"))
	if version == "" {
		version = titanimage.ModelID
	}
	runtime := bedrockruntime.NewFromConfig(awsConfig)
	vectors := vectorstore.NewS3(s3vectors.NewFromConfig(awsConfig), bucket, index)
	result, err := run(ctx, repository, titanimage.NewWithModel(runtime, version), vectors, version, *apply)
	if err != nil {
		panic(err)
	}
	mode := "dry-run"
	if *apply {
		mode = "applied"
	}
	fmt.Fprintf(os.Stdout, "mode=%s scanned=%d eligible=%d embedded=%d skipped-video=%d skipped-no-media=%d failed=%d\n", mode, result.Scanned, result.Eligible, result.Embedded, result.SkippedVideo, result.SkippedNoMedia, result.Failed)
}

func run(ctx context.Context, repository imageSignalStore, embedder embed.ImageEmbedder, vectors imageVectorWriter, version string, apply bool) (report, error) {
	users, err := repository.UserIDs(ctx)
	if err != nil {
		return report{}, err
	}
	result := report{}
	for _, userID := range users {
		signals, err := repository.Signals(ctx, userID)
		if err != nil {
			return result, fmt.Errorf("load signals for %s: %w", userID, err)
		}
		behaviours, err := repository.Behaviours(ctx, userID)
		if err != nil {
			return result, fmt.Errorf("load behaviours for %s: %w", userID, err)
		}
		targets := make(map[string]target)
		for _, signal := range signals {
			result.Scanned++
			if len(signal.ImageVector) == 0 {
				entry := targets[signal.ItemID]
				entry.signal = true
				targets[signal.ItemID] = entry
			}
		}
		for _, behaviour := range behaviours {
			result.Scanned++
			if len(behaviour.ImageVector) == 0 {
				entry := targets[behaviour.ItemID]
				entry.behaviour = true
				targets[behaviour.ItemID] = entry
			}
		}
		liveItems, err := repository.LiveItems(ctx, userID)
		if err != nil {
			return result, fmt.Errorf("load live items for %s: %w", userID, err)
		}
		archiveItems, err := repository.ArchiveItems(ctx, userID)
		if err != nil {
			return result, fmt.Errorf("load archive items for %s: %w", userID, err)
		}
		items := indexItems(liveItems, archiveItems)
		for itemID, entry := range targets {
			resolved, ok := items[itemID]
			if !ok {
				result.Failed++
				slog.WarnContext(ctx, "image signal backfill item unavailable", "user", userID, "item_id", itemID, "error", store.ErrNotFound)
				continue
			}
			item, kind, live := resolved.item, resolved.kind, resolved.live
			item.PK = domain.UserPK(userID)
			if item.MediaType == "video" || item.VideoID != "" {
				result.SkippedVideo++
				continue
			}
			if item.MediaKey == "" {
				result.SkippedNoMedia++
				continue
			}
			result.Eligible++
			if !apply {
				continue
			}
			key := selectedVariantKey(item.MediaVariants, item.MediaKey)
			jpeg, _, readErr := repository.Content(ctx, key)
			if readErr != nil {
				result.Failed++
				slog.WarnContext(ctx, "image signal backfill read failed", "user", userID, "item_id", itemID, "key", key, "error", readErr)
				continue
			}
			embedded, embedErr := embedder.EmbedImage(ctx, jpeg)
			if embedErr != nil {
				result.Failed++
				slog.WarnContext(ctx, "image signal backfill embedding failed", "user", userID, "item_id", itemID, "error", embedErr)
				continue
			}
			encoded := score.EncodeVector(score.Normalize(embedded))
			item.ImageVector, item.ImageModelVersion = encoded, version
			record, ok := vectorstore.ImageRecordFromItem(item, kind)
			if !ok {
				result.Failed++
				continue
			}
			if err := vectors.Put(ctx, record); err != nil {
				result.Failed++
				slog.WarnContext(ctx, "image signal backfill vector write failed", "user", userID, "item_id", itemID, "error", err)
				continue
			}
			if entry.signal {
				if err := repository.UpdateSignalImageEmbedding(ctx, userID, itemID, encoded, version); err != nil {
					result.Failed++
					continue
				}
			}
			if entry.behaviour {
				if err := repository.UpdateBehaviourImageEmbedding(ctx, userID, itemID, encoded, version); err != nil {
					result.Failed++
					continue
				}
			}
			if live {
				if err := repository.SetItemImageVector(ctx, userID, itemID, encoded, version); err != nil {
					result.Failed++
					continue
				}
			}
			result.Embedded++
		}
	}
	return result, nil
}

func indexItems(liveItems, archiveItems []domain.Item) map[string]indexedItem {
	items := make(map[string]indexedItem, len(liveItems)+len(archiveItems))
	for _, item := range archiveItems {
		if item.ItemID == "" {
			continue
		}
		if _, exists := items[item.ItemID]; !exists {
			items[item.ItemID] = indexedItem{item: item, kind: vectorstore.KindArchive}
		}
	}
	for _, item := range liveItems {
		if item.ItemID != "" {
			items[item.ItemID] = indexedItem{item: item, kind: vectorstore.KindLive, live: true}
		}
	}
	return items
}

func selectedVariantKey(variants []domain.MediaVariant, fallback string) string {
	if len(variants) == 0 {
		return fallback
	}
	selected := variants[0]
	withinLimit := false
	for _, variant := range variants {
		if variant.Width <= 768 && (!withinLimit || variant.Width > selected.Width) {
			selected = variant
			withinLimit = true
		} else if !withinLimit && variant.Width < selected.Width {
			selected = variant
		}
	}
	if selected.Key == "" {
		return fallback
	}
	return selected.Key
}
