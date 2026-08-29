package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/aws/aws-sdk-go-v2/service/s3vectors"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
	"github.com/nuntz/sema/internal/store"
	"github.com/nuntz/sema/internal/vectorstore"
)

const defaultWriteConcurrency = 16

type itemVectorStore interface {
	UserIDs(context.Context) ([]string, error)
	LiveItems(context.Context, string) ([]domain.Item, error)
	LoadItemVectors(context.Context, string, []domain.Item) error
	PutItemVectorIfAbsent(context.Context, string, string, []byte, int64) (bool, error)
}

type vectorSource interface {
	GetBatch(context.Context, []string) (map[string][]float32, error)
}

type backfillResult struct {
	Users       int
	Total       int
	Missing     int
	FromInline  int
	FromS3      int
	Unavailable int
	Written     int
}

type recoveredVector struct {
	userID string
	itemID string
	vector []byte
	ttl    int64
}

func main() {
	apply := flag.Bool("apply", false, "write missing DynamoDB item vector rows")
	concurrency := flag.Int("concurrency", defaultWriteConcurrency, "maximum concurrent DynamoDB writes")
	flag.Parse()
	ctx := context.Background()
	repository, config, err := store.FromEnv(ctx)
	if err != nil {
		panic(err)
	}
	bucket, index := strings.TrimSpace(os.Getenv("VECTOR_BUCKET")), strings.TrimSpace(os.Getenv("VECTOR_INDEX"))
	if bucket == "" || index == "" {
		panic("VECTOR_BUCKET and VECTOR_INDEX are required")
	}
	vectors := vectorstore.NewS3(s3vectors.NewFromConfig(config), bucket, index)
	if _, err := run(ctx, repository, vectors, *apply, *concurrency); err != nil {
		panic(err)
	}
}

func run(ctx context.Context, repository itemVectorStore, vectors vectorSource, apply bool, concurrency int) (backfillResult, error) {
	if concurrency < 1 {
		return backfillResult{}, errors.New("concurrency must be at least 1")
	}
	users, err := repository.UserIDs(ctx)
	if err != nil {
		return backfillResult{}, err
	}
	result := backfillResult{Users: len(users)}
	recovered := []recoveredVector{}
	for _, userID := range users {
		items, err := repository.LiveItems(ctx, userID)
		if err != nil {
			return result, fmt.Errorf("list live items for %s: %w", userID, err)
		}
		result.Total += len(items)
		separated := append([]domain.Item(nil), items...)
		for index := range separated {
			separated[index].Vector = nil
		}
		if err := repository.LoadItemVectors(ctx, userID, separated); err != nil {
			return result, fmt.Errorf("load item vector rows for %s: %w", userID, err)
		}

		needsS3 := make([]domain.Item, 0)
		seen := make(map[string]bool, len(items))
		for index, item := range items {
			if seen[item.ItemID] || len(separated[index].Vector) > 0 {
				continue
			}
			seen[item.ItemID] = true
			result.Missing++
			if len(item.Vector) > 0 {
				result.FromInline++
				recovered = append(recovered, recoveredVector{userID: userID, itemID: item.ItemID, vector: append([]byte(nil), item.Vector...), ttl: item.TTL})
				continue
			}
			needsS3 = append(needsS3, item)
		}
		keys := make([]string, len(needsS3))
		for index, item := range needsS3 {
			keys[index] = item.ItemID
		}
		external, err := vectors.GetBatch(ctx, keys)
		if err != nil {
			return result, fmt.Errorf("load S3 vectors for %s: %w", userID, err)
		}
		for _, item := range needsS3 {
			vector := score.EncodeVector(external[item.ItemID])
			if len(vector) == 0 {
				result.Unavailable++
				slog.WarnContext(ctx, "item vector unavailable", "user", userID, "item_id", item.ItemID)
				continue
			}
			result.FromS3++
			recovered = append(recovered, recoveredVector{userID: userID, itemID: item.ItemID, vector: vector, ttl: item.TTL})
		}
	}
	if apply {
		result.Written, err = writeRecovered(ctx, repository, recovered, concurrency)
		if err != nil {
			return result, err
		}
	}
	mode := "dry-run"
	if apply {
		mode = "applied"
	}
	fmt.Fprintf(os.Stdout, "mode=%s users=%d total=%d missing=%d recoverable=%d inline=%d s3=%d unavailable=%d written=%d\n",
		mode, result.Users, result.Total, result.Missing, result.FromInline+result.FromS3, result.FromInline, result.FromS3, result.Unavailable, result.Written)
	return result, nil
}

func writeRecovered(ctx context.Context, repository itemVectorStore, recovered []recoveredVector, concurrency int) (int, error) {
	if len(recovered) == 0 {
		return 0, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan recoveredVector)
	var workers sync.WaitGroup
	var written atomic.Int64
	var firstErr error
	var firstErrOnce sync.Once
	for range min(concurrency, len(recovered)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range jobs {
				created, err := repository.PutItemVectorIfAbsent(ctx, item.userID, item.itemID, item.vector, item.ttl)
				if err != nil {
					firstErrOnce.Do(func() {
						firstErr = fmt.Errorf("write %s/%s: %w", item.userID, item.itemID, err)
						cancel()
					})
					return
				}
				if created {
					written.Add(1)
				}
			}
		}()
	}

sendItems:
	for _, item := range recovered {
		select {
		case jobs <- item:
		case <-ctx.Done():
			break sendItems
		}
	}
	close(jobs)
	workers.Wait()
	if firstErr != nil {
		return int(written.Load()), firstErr
	}
	return int(written.Load()), ctx.Err()
}
