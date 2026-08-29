package vectorstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors/document"
	"github.com/aws/aws-sdk-go-v2/service/s3vectors/types"
)

type stubS3Vectors struct {
	put        *s3vectors.PutVectorsInput
	puts       []*s3vectors.PutVectorsInput
	query      *s3vectors.QueryVectorsInput
	list       *s3vectors.ListVectorsOutput
	deleted    []string
	gets       [][]string
	getVectors map[string][]float32
}

func (s *stubS3Vectors) PutVectors(_ context.Context, input *s3vectors.PutVectorsInput, _ ...func(*s3vectors.Options)) (*s3vectors.PutVectorsOutput, error) {
	s.put = input
	s.puts = append(s.puts, input)
	return &s3vectors.PutVectorsOutput{}, nil
}

func TestS3PutBatchChunksAtServiceLimit(t *testing.T) {
	stub := &stubS3Vectors{}
	store := NewS3(stub, "bucket", "items")
	records := make([]Record, putVectorsLimit*2+1)
	for index := range records {
		records[index] = Record{Key: fmt.Sprintf("item-%d", index), Data: []float32{1, 0}, Kind: KindLive, ExpiresTS: 42}
	}
	if err := store.PutBatch(context.Background(), records); err != nil {
		t.Fatal(err)
	}
	if len(stub.puts) != 3 || len(stub.puts[0].Vectors) != 500 || len(stub.puts[1].Vectors) != 500 || len(stub.puts[2].Vectors) != 1 {
		t.Fatalf("put batches = %#v", []int{len(stub.puts[0].Vectors), len(stub.puts[1].Vectors), len(stub.puts[2].Vectors)})
	}
	if err := store.PutBatch(context.Background(), []Record{{Key: "valid", Data: []float32{1}}, {Key: "missing-data"}}); err == nil {
		t.Fatal("invalid batch succeeded")
	}
	if len(stub.puts) != 3 {
		t.Fatalf("invalid batch made partial writes: %d calls", len(stub.puts))
	}
}

func TestS3PutBatchAlsoRespectsPayloadLimit(t *testing.T) {
	stub := &stubS3Vectors{}
	store := NewS3(stub, "bucket", "items")
	data := make([]float32, 4096)
	for index := range data {
		data[index] = 0.12345678
	}
	records := make([]Record, putVectorsLimit)
	for index := range records {
		records[index] = Record{Key: fmt.Sprintf("item-%d", index), Data: data, Kind: KindLive}
	}
	if err := store.PutBatch(context.Background(), records); err != nil {
		t.Fatal(err)
	}
	if len(stub.puts) < 2 {
		t.Fatalf("large payload used %d request, want multiple", len(stub.puts))
	}
	for _, input := range stub.puts {
		if len(input.Vectors) > putVectorsLimit {
			t.Fatalf("batch has %d vectors", len(input.Vectors))
		}
	}
}
func (s *stubS3Vectors) DeleteVectors(_ context.Context, input *s3vectors.DeleteVectorsInput, _ ...func(*s3vectors.Options)) (*s3vectors.DeleteVectorsOutput, error) {
	s.deleted = append(s.deleted, input.Keys...)
	return &s3vectors.DeleteVectorsOutput{}, nil
}
func (s *stubS3Vectors) GetVectors(_ context.Context, input *s3vectors.GetVectorsInput, _ ...func(*s3vectors.Options)) (*s3vectors.GetVectorsOutput, error) {
	s.gets = append(s.gets, append([]string(nil), input.Keys...))
	output := &s3vectors.GetVectorsOutput{}
	for _, key := range input.Keys {
		if vector, ok := s.getVectors[key]; ok {
			output.Vectors = append(output.Vectors, types.GetOutputVector{Key: aws.String(key), Data: &types.VectorDataMemberFloat32{Value: vector}})
		}
	}
	return output, nil
}
func (s *stubS3Vectors) QueryVectors(_ context.Context, input *s3vectors.QueryVectorsInput, _ ...func(*s3vectors.Options)) (*s3vectors.QueryVectorsOutput, error) {
	s.query = input
	return &s3vectors.QueryVectorsOutput{
		DistanceMetric: types.DistanceMetricCosine,
		Vectors:        []types.QueryOutputVector{{Key: aws.String("near"), Distance: aws.Float32(.08)}},
	}, nil
}
func (s *stubS3Vectors) ListVectors(context.Context, *s3vectors.ListVectorsInput, ...func(*s3vectors.Options)) (*s3vectors.ListVectorsOutput, error) {
	return s.list, nil
}

func TestS3VectorLifecycleAndExpiryFilter(t *testing.T) {
	stub := &stubS3Vectors{}
	store := NewS3(stub, "bucket", "items")
	record := Record{Key: "item", Data: []float32{1, 0}, Kind: KindLive, FeedID: "feed", PublishedTS: "then", ExpiresTS: 90, Title: "Title"}
	if err := store.Put(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	encoded, err := stub.put.Vectors[0].Metadata.MarshalSmithyDocument()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["kind"] != "live" || metadata["title"] != "Title" {
		t.Fatalf("put metadata = %#v", metadata)
	}
	if _, err := store.Query(context.Background(), []float32{1, 0}, 12, 100); err != nil {
		t.Fatal(err)
	}
	var filter map[string]any
	encoded, err = stub.query.Filter.MarshalSmithyDocument()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &filter); err != nil {
		t.Fatal(err)
	}
	if _, ok := filter["$or"]; !ok {
		t.Fatalf("query filter = %#v", filter)
	}

	stub.list = &s3vectors.ListVectorsOutput{Vectors: []types.ListOutputVector{
		{Key: aws.String("expired"), Metadata: document.NewLazyDocument(map[string]any{"kind": "live", "expires_ts": int64(90)})},
		{Key: aws.String("boundary"), Metadata: document.NewLazyDocument(map[string]any{"kind": "live", "expires_ts": int64(100)})},
		{Key: aws.String("future"), Metadata: document.NewLazyDocument(map[string]any{"kind": "live", "expires_ts": int64(110)})},
		{Key: aws.String("archive"), Metadata: document.NewLazyDocument(map[string]any{"kind": "archive"})},
	}}
	deleted, size, err := store.Cleanup(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 || size != 4 || !reflect.DeepEqual(stub.deleted, []string{"expired"}) {
		t.Fatalf("cleanup = deleted %d size %d keys %#v", deleted, size, stub.deleted)
	}
}

func TestS3GetBatchChunksDeduplicatesAndOmitsMissing(t *testing.T) {
	stub := &stubS3Vectors{getVectors: map[string][]float32{
		"item-0": {1, 0}, "item-100": {0, 1}, "item-200": {1, 1},
	}}
	store := NewS3(stub, "bucket", "items")
	keys := make([]string, 201)
	for index := range keys {
		keys[index] = fmt.Sprintf("item-%d", index)
	}
	keys = append(keys, "item-0", "")
	vectors, err := store.GetBatch(context.Background(), keys)
	if err != nil {
		t.Fatal(err)
	}
	if len(stub.gets) != 3 || len(stub.gets[0]) != 100 || len(stub.gets[1]) != 100 || len(stub.gets[2]) != 1 {
		t.Fatalf("get batches = %#v", stub.gets)
	}
	if len(vectors) != 3 || !reflect.DeepEqual(vectors["item-100"], []float32{0, 1}) {
		t.Fatalf("vectors = %#v", vectors)
	}
	vector, err := store.Get(context.Background(), "item-0")
	if err != nil || !reflect.DeepEqual(vector, []float32{1, 0}) {
		t.Fatalf("single get = %#v, %v", vector, err)
	}
	if _, err := store.Get(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing get error = %v", err)
	}
}
