package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors"
	"github.com/nuntz/sema/internal/observability"
	"github.com/nuntz/sema/internal/vectorstore"
)

type handler struct{ vectors vectorstore.Store }

func (h *handler) run(ctx context.Context) error {
	deleted, size, err := h.vectors.Cleanup(ctx, time.Now().Unix())
	if err != nil {
		return err
	}
	observability.Emit(map[string]float64{"VectorIndexSize": float64(size - deleted), "VectorsDeleted": float64(deleted)}, nil)
	return nil
}

func main() {
	ctx := context.Background()
	awsConfig, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		panic(err)
	}
	bucket, index := strings.TrimSpace(os.Getenv("VECTOR_BUCKET")), strings.TrimSpace(os.Getenv("VECTOR_INDEX"))
	if bucket == "" || index == "" {
		panic("VECTOR_BUCKET and VECTOR_INDEX are required")
	}
	lambda.Start((&handler{vectors: vectorstore.NewS3(s3vectors.NewFromConfig(awsConfig), bucket, index)}).run)
}
