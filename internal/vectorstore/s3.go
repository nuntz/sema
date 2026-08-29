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

const (
	putVectorsLimit           = 500
	putVectorsPayloadLimit    = 20 << 20
	putVectorsRequestOverhead = 1024
)

func NewS3(client s3VectorsAPI, bucket, index string) *S3 {
	return &S3{client: client, bucket: bucket, index: index}
}

func (s *S3) Put(ctx context.Context, record Record) error {
	return s.PutBatch(ctx, []Record{record})
}

func (s *S3) PutBatch(ctx context.Context, records []Record) error {
	vectors := make([]types.PutInputVector, len(records))
	sizes := make([]int, len(records))
	for index, record := range records {
		vector, size, err := putInputVector(record)
		if err != nil {
			return fmt.Errorf("vector %d: %w", index, err)
		}
		if size+putVectorsRequestOverhead > putVectorsPayloadLimit {
			return fmt.Errorf("vector %d exceeds PutVectors payload limit", index)
		}
		vectors[index] = vector
		sizes[index] = size
	}
	for offset := 0; offset < len(vectors); {
		end, payloadSize := offset, putVectorsRequestOverhead
		for end < len(vectors) && end-offset < putVectorsLimit && payloadSize+sizes[end]+1 <= putVectorsPayloadLimit {
			payloadSize += sizes[end] + 1
			end++
		}
		if _, err := s.client.PutVectors(ctx, &s3vectors.PutVectorsInput{
			VectorBucketName: aws.String(s.bucket), IndexName: aws.String(s.index), Vectors: vectors[offset:end],
		}); err != nil {
			return err
		}
		offset = end
	}
	return nil
}

func putInputVector(record Record) (types.PutInputVector, int, error) {
	if record.Key == "" || len(record.Data) == 0 {
		return types.PutInputVector{}, 0, fmt.Errorf("vector key and data are required")
	}
	metadata := map[string]any{
		"kind": string(record.Kind), "feed_id": record.FeedID,
		"published_ts": record.PublishedTS, "title": record.Title,
	}
	if record.Kind == KindLive {
		metadata["expires_ts"] = record.ExpiresTS
	}
	encoded, err := json.Marshal(map[string]any{
		"key": record.Key, "data": map[string]any{"float32": record.Data}, "metadata": metadata,
	})
	if err != nil {
		return types.PutInputVector{}, 0, fmt.Errorf("encode vector payload: %w", err)
	}
	return types.PutInputVector{
		Key: aws.String(record.Key), Data: &types.VectorDataMemberFloat32{Value: record.Data}, Metadata: document.NewLazyDocument(metadata),
	}, len(encoded), nil
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
