package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3vectors"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/store"
	"github.com/nuntz/sema/internal/vectorstore"
)

type itemStore interface {
	UserIDs(context.Context) ([]string, error)
	LiveItems(context.Context, string) ([]domain.Item, error)
	ArchiveItems(context.Context, string) ([]domain.Item, error)
	LoadItemVectors(context.Context, string, []domain.Item) error
}

func main() {
	apply := flag.Bool("apply", false, "write current live and archive vectors")
	image := flag.Bool("image", false, "backfill stored image embeddings into IMAGE_VECTOR_INDEX")
	flag.Parse()
	ctx := context.Background()
	repository, config, err := store.FromEnv(ctx)
	if err != nil {
		panic(err)
	}
	bucket, index := strings.TrimSpace(os.Getenv("VECTOR_BUCKET")), strings.TrimSpace(os.Getenv("VECTOR_INDEX"))
	if *image {
		index = strings.TrimSpace(os.Getenv("IMAGE_VECTOR_INDEX"))
	}
	if bucket == "" || index == "" {
		panic("VECTOR_BUCKET and VECTOR_INDEX are required")
	}
	vectors := vectorstore.NewS3(s3vectors.NewFromConfig(config), bucket, index)
	if _, _, err := runChannel(ctx, repository, vectors, *apply, *image); err != nil {
		panic(err)
	}
}

func run(ctx context.Context, repository itemStore, vectors vectorstore.Store, apply bool) (int, int, error) {
	return runChannel(ctx, repository, vectors, apply, false)
}

func runChannel(ctx context.Context, repository itemStore, vectors vectorstore.Store, apply, image bool) (int, int, error) {
	users, err := repository.UserIDs(ctx)
	if err != nil {
		return 0, 0, err
	}
	liveCount, archiveCount := 0, 0
	for _, userID := range users {
		live, err := repository.LiveItems(ctx, userID)
		if err != nil {
			return liveCount, archiveCount, err
		}
		archive, err := repository.ArchiveItems(ctx, userID)
		if err != nil {
			return liveCount, archiveCount, err
		}
		if err := repository.LoadItemVectors(ctx, userID, live); err != nil {
			return liveCount, archiveCount, err
		}
		if err := repository.LoadItemVectors(ctx, userID, archive); err != nil {
			return liveCount, archiveCount, err
		}
		records := make([]vectorstore.Record, 0, len(live)+len(archive))
		positions := map[string]int{}
		for _, group := range []struct {
			items []domain.Item
			kind  vectorstore.Kind
		}{{live, vectorstore.KindLive}, {archive, vectorstore.KindArchive}} {
			for _, item := range group.items {
				item.PK = domain.UserPK(userID)
				record := vectorstore.FromItem(item, group.kind)
				if image {
					var ok bool
					record, ok = vectorstore.ImageRecordFromItem(item, group.kind)
					if !ok || item.MediaType == "video" || item.VideoID != "" {
						continue
					}
				}
				if len(record.Data) == 0 {
					continue
				}
				if group.kind == vectorstore.KindLive {
					liveCount++
				} else {
					archiveCount++
				}
				if position, exists := positions[record.Key]; exists {
					records[position] = record
				} else {
					positions[record.Key] = len(records)
					records = append(records, record)
				}
			}
		}
		if apply && len(records) > 0 {
			if err := vectors.PutBatch(ctx, records); err != nil {
				return liveCount, archiveCount, fmt.Errorf("put vectors for user %s: %w", userID, err)
			}
		}
	}
	mode := "dry-run"
	if apply {
		mode = "applied"
	}
	fmt.Fprintf(os.Stdout, "mode=%s users=%d live=%d archive=%d total=%d\n", mode, len(users), liveCount, archiveCount, liveCount+archiveCount)
	return liveCount, archiveCount, nil
}
