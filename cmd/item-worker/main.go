package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors"
	bedrockembed "github.com/nuntz/sema/internal/embed/bedrock"
	"github.com/nuntz/sema/internal/embed/titanimage"
	"github.com/nuntz/sema/internal/httpx"
	"github.com/nuntz/sema/internal/ingest"
	"github.com/nuntz/sema/internal/media"
	"github.com/nuntz/sema/internal/score"
	"github.com/nuntz/sema/internal/store"
	storycluster "github.com/nuntz/sema/internal/story"
	"github.com/nuntz/sema/internal/summarize"
	bedrocksummary "github.com/nuntz/sema/internal/summarize/bedrock"
	"github.com/nuntz/sema/internal/vectorstore"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	repository, config, err := store.FromEnv(context.Background())
	if err != nil {
		panic(err)
	}
	articleHTTP := httpx.New(15*time.Second, 5<<20)
	processor := media.New(httpx.New(10*time.Second, media.MaxDownloadBytes))
	modelVersion := strings.TrimSpace(os.Getenv("MODEL_VERSION"))
	if modelVersion == "" {
		modelVersion = "amazon.titan-embed-text-v2:0"
	}
	scoringVersion := strings.TrimSpace(os.Getenv("SCORING_VERSION"))
	if scoringVersion == "" {
		scoringVersion = score.VersionV2
	}
	summaryModel := strings.TrimSpace(os.Getenv("SUMMARIZE_MODEL"))
	if summaryModel == "" {
		summaryModel = bedrocksummary.ModelID
	}
	runtime := bedrockruntime.NewFromConfig(config)
	vectorBucket, vectorIndex := strings.TrimSpace(os.Getenv("VECTOR_BUCKET")), strings.TrimSpace(os.Getenv("VECTOR_INDEX"))
	if vectorBucket == "" || vectorIndex == "" {
		panic("VECTOR_BUCKET and VECTOR_INDEX are required")
	}
	embedder := bedrockembed.NewWithModel(runtime, modelVersion)
	summarizer := summarize.New(bedrocksummary.NewWithModel(runtime, summaryModel))
	imageModelVersion := strings.TrimSpace(os.Getenv("IMAGE_MODEL_VERSION"))
	if imageModelVersion == "" {
		imageModelVersion = titanimage.ModelID
	}
	imageVectorIndex := strings.TrimSpace(os.Getenv("IMAGE_VECTOR_INDEX"))
	vectorClient := s3vectors.NewFromConfig(config)
	h := &ingest.Processor{
		Store: repository, HTTP: articleHTTP, Media: processor, Embedder: embedder, Summarizer: summarizer,
		Extraction:   ingest.ArticleExtractor{},
		ModelVersion: modelVersion, ScoringVersion: scoringVersion,
		Vectors: vectorstore.NewS3(vectorClient, vectorBucket, vectorIndex), StoryConfig: storycluster.FromEnv(),
	}
	if imageVectorIndex == "" {
		slog.Info("image embedding channel disabled", "reason", "IMAGE_VECTOR_INDEX is empty")
	} else {
		h.ImageEmbedder = titanimage.NewWithModel(runtime, imageModelVersion)
		h.ImageModelVersion = imageModelVersion
		h.ImageVectors = vectorstore.NewS3(vectorClient, vectorBucket, imageVectorIndex)
	}
	h.Scoring = ingest.Ranking{
		Signals: repository, Models: score.NewCache(repository, 5*time.Minute, modelVersion, h.ImageModelVersion),
		Version: scoringVersion, TextVersion: modelVersion, ImageVersion: h.ImageModelVersion,
	}
	lambda.Start(func(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
		batch := ingest.Batch{Records: make([]ingest.Message, len(event.Records))}
		for i, record := range event.Records {
			batch.Records[i] = ingest.Message{ID: record.MessageId, Body: record.Body}
		}
		result, err := h.Run(ctx, batch)
		response := events.SQSEventResponse{}
		for _, failure := range result.Failures {
			response.BatchItemFailures = append(response.BatchItemFailures, events.SQSBatchItemFailure{ItemIdentifier: failure.ID})
		}
		return response, err
	})
}
