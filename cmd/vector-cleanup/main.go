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

type handler struct {
	vectors      vectorstore.Store
	imageVectors vectorstore.Store
	emit         func(map[string]float64, map[string]string)
}

func (h *handler) run(ctx context.Context) error {
	deleted, size, err := h.vectors.Cleanup(ctx, time.Now().Unix())
	if err != nil {
		return err
	}
	emit := h.emit
	if emit == nil {
		emit = observability.Emit
	}
	emit(map[string]float64{"VectorIndexSize": float64(size - deleted), "VectorsDeleted": float64(deleted)}, nil)
	if h.imageVectors != nil {
		imageDeleted, imageSize, imageErr := h.imageVectors.Cleanup(ctx, time.Now().Unix())
		if imageErr != nil {
			return imageErr
		}
		emit(map[string]float64{"ImageVectorIndexSize": float64(imageSize - imageDeleted), "ImageVectorsDeleted": float64(imageDeleted)}, nil)
	}
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
	client := s3vectors.NewFromConfig(awsConfig)
	h := &handler{vectors: vectorstore.NewS3(client, bucket, index)}
	if imageIndex := strings.TrimSpace(os.Getenv("IMAGE_VECTOR_INDEX")); imageIndex != "" {
		h.imageVectors = vectorstore.NewS3(client, bucket, imageIndex)
	}
	lambda.Start(h.run)
}
