package vectorstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors"
)

func TestS3VectorsDevIntegration(t *testing.T) {
	if os.Getenv("S3_VECTORS_INTEGRATION") != "1" {
		t.Skip("set S3_VECTORS_INTEGRATION=1 to run against the dev vector index")
	}
	bucket, index := os.Getenv("VECTOR_BUCKET"), os.Getenv("VECTOR_INDEX")
	if bucket == "" || index == "" {
		t.Fatal("VECTOR_BUCKET and VECTOR_INDEX are required")
	}
	if !strings.HasSuffix(bucket, "-dev") {
		t.Fatalf("integration test is restricted to a dev bucket, got %q", bucket)
	}
	dimensions := 1024
	if value := os.Getenv("VECTOR_DIMENSIONS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			t.Fatalf("invalid VECTOR_DIMENSIONS %q", value)
		}
		dimensions = parsed
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	config, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	store := NewS3(s3vectors.NewFromConfig(config), bucket, index)
	key := fmt.Sprintf("drop5-integration-%d", time.Now().UnixNano())
	vector := make([]float32, dimensions)
	vector[0] = 1
	if err := store.Put(ctx, Record{
		Key: key, Data: vector, Kind: KindLive, FeedID: "integration",
		PublishedTS: time.Now().UTC().Format(time.RFC3339), ExpiresTS: time.Now().Add(time.Hour).Unix(), Title: "integration test",
	}); err != nil {
		t.Fatal(err)
	}
	deleted := false
	defer func() {
		if !deleted {
			_ = store.Delete(context.Background(), key)
		}
	}()

	if err := eventually(ctx, func() (bool, error) {
		got, err := store.Get(ctx, key)
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return err == nil && len(got) == dimensions && got[0] == 1, err
	}); err != nil {
		t.Fatalf("get vector: %v", err)
	}
	if err := eventually(ctx, func() (bool, error) {
		matches, err := store.Query(ctx, vector, 10, time.Now().Unix())
		if err != nil {
			return false, err
		}
		for _, match := range matches {
			if match.Key == key {
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		t.Fatalf("query vector: %v", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	deleted = true
}

func eventually(ctx context.Context, check func() (bool, error)) error {
	for {
		ok, err := check()
		if ok {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}
