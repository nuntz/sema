package vectorstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors/document"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors/types"
)

type s3VectorsAPI interface {
	PutVectors(context.Context, *s3vectors.PutVectorsInput, ...func(*s3vectors.Options)) (*s3vectors.PutVectorsOutput, error)
	DeleteVectors(context.Context, *s3vectors.DeleteVectorsInput, ...func(*s3vectors.Options)) (*s3vectors.DeleteVectorsOutput, error)
	GetVectors(context.Context, *s3vectors.GetVectorsInput, ...func(*s3vectors.Options)) (*s3vectors.GetVectorsOutput, error)
	QueryVectors(context.Context, *s3vectors.QueryVectorsInput, ...func(*s3vectors.Options)) (*s3vectors.QueryVectorsOutput, error)
	ListVectors(context.Context, *s3vectors.ListVectorsInput, ...func(*s3vectors.Options)) (*s3vectors.ListVectorsOutput, error)
}

type S3 struct {
	client s3VectorsAPI
	bucket string
	index  string
}

func NewS3(client s3VectorsAPI, bucket, index string) *S3 {
	return &S3{client: client, bucket: bucket, index: index}
}

func (s *S3) Put(ctx context.Context, record Record) error {
	if record.Key == "" || len(record.Data) == 0 {
		return fmt.Errorf("vector key and data are required")
	}
	metadata := map[string]any{
		"kind": string(record.Kind), "feed_id": record.FeedID,
		"published_ts": record.PublishedTS, "title": record.Title,
	}
	if record.Kind == KindLive {
		metadata["expires_ts"] = record.ExpiresTS
	}
	_, err := s.client.PutVectors(ctx, &s3vectors.PutVectorsInput{
		VectorBucketName: aws.String(s.bucket), IndexName: aws.String(s.index),
		Vectors: []types.PutInputVector{{
			Key: aws.String(record.Key), Data: &types.VectorDataMemberFloat32{Value: record.Data}, Metadata: document.NewLazyDocument(metadata),
		}},
	})
	return err
}

func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteVectors(ctx, &s3vectors.DeleteVectorsInput{
		VectorBucketName: aws.String(s.bucket), IndexName: aws.String(s.index), Keys: []string{key},
	})
	return err
}

func (s *S3) Get(ctx context.Context, key string) ([]float32, error) {
	output, err := s.client.GetVectors(ctx, &s3vectors.GetVectorsInput{
		VectorBucketName: aws.String(s.bucket), IndexName: aws.String(s.index), Keys: []string{key}, ReturnData: true,
	})
	if err != nil {
		return nil, err
	}
	if len(output.Vectors) == 0 {
		return nil, ErrNotFound
	}
	data, ok := output.Vectors[0].Data.(*types.VectorDataMemberFloat32)
	if !ok || len(data.Value) == 0 {
		return nil, ErrNotFound
	}
	return data.Value, nil
}

func (s *S3) Query(ctx context.Context, vector []float32, limit int, now int64) ([]Match, error) {
	if limit < 1 {
		return []Match{}, nil
	}
	filter := map[string]any{"$or": []any{
		map[string]any{"kind": map[string]any{"$eq": string(KindArchive)}},
		map[string]any{"expires_ts": map[string]any{"$gt": now}},
	}}
	output, err := s.client.QueryVectors(ctx, &s3vectors.QueryVectorsInput{
		VectorBucketName: aws.String(s.bucket), IndexName: aws.String(s.index), TopK: aws.Int32(int32(limit)),
		QueryVector: &types.VectorDataMemberFloat32{Value: vector}, Filter: document.NewLazyDocument(filter), ReturnDistance: true,
	})
	if err != nil {
		return nil, err
	}
	matches := make([]Match, 0, len(output.Vectors))
	for _, candidate := range output.Vectors {
		if candidate.Key == nil || candidate.Distance == nil {
			continue
		}
		matches = append(matches, Match{Key: *candidate.Key, Similarity: Similarity(*candidate.Distance)})
	}
	return matches, nil
}

type vectorMetadata struct {
	Kind      string `json:"kind"`
	ExpiresTS int64  `json:"expires_ts"`
}

func (s *S3) Cleanup(ctx context.Context, now int64) (int, int, error) {
	deleted, size := 0, 0
	var token *string
	for {
		output, err := s.client.ListVectors(ctx, &s3vectors.ListVectorsInput{
			VectorBucketName: aws.String(s.bucket), IndexName: aws.String(s.index), NextToken: token,
			MaxResults: aws.Int32(500), ReturnMetadata: true,
		})
		if err != nil {
			return deleted, size, err
		}
		size += len(output.Vectors)
		keys := make([]string, 0)
		for _, candidate := range output.Vectors {
			if candidate.Key == nil || candidate.Metadata == nil {
				continue
			}
			var metadata vectorMetadata
			encoded, err := candidate.Metadata.MarshalSmithyDocument()
			if err != nil {
				return deleted, size, err
			}
			if err := json.Unmarshal(encoded, &metadata); err != nil {
				return deleted, size, err
			}
			if metadata.Kind == string(KindLive) && metadata.ExpiresTS < now {
				keys = append(keys, *candidate.Key)
			}
		}
		if len(keys) > 0 {
			if _, err := s.client.DeleteVectors(ctx, &s3vectors.DeleteVectorsInput{
				VectorBucketName: aws.String(s.bucket), IndexName: aws.String(s.index), Keys: keys,
			}); err != nil {
				return deleted, size, err
			}
			deleted += len(keys)
		}
		token = output.NextToken
		if token == nil || *token == "" {
			return deleted, size, nil
		}
	}
}
