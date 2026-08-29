package main

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/score"
)

type fakeItemVectors struct {
	items      []domain.Item
	vectorRows map[string][]byte
	writes     int
	mu         sync.Mutex
}

func (*fakeItemVectors) UserIDs(context.Context) ([]string, error) {
	return []string{"user"}, nil
}

func (f *fakeItemVectors) LiveItems(context.Context, string) ([]domain.Item, error) {
	return append([]domain.Item(nil), f.items...), nil
}

func (f *fakeItemVectors) LoadItemVectors(_ context.Context, _ string, items []domain.Item) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := range items {
		if vector, ok := f.vectorRows[items[index].ItemID]; ok {
			items[index].Vector = append([]byte(nil), vector...)
		}
	}
	return nil
}

func (f *fakeItemVectors) PutItemVectorIfAbsent(_ context.Context, _, itemID string, vector []byte, _ int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.vectorRows[itemID]; exists {
		return false, nil
	}
	f.vectorRows[itemID] = append([]byte(nil), vector...)
	f.writes++
	return true, nil
}

type fakeVectorSource struct {
	vectors  map[string][]float32
	requests [][]string
}

func (f *fakeVectorSource) GetBatch(_ context.Context, keys []string) (map[string][]float32, error) {
	f.requests = append(f.requests, append([]string(nil), keys...))
	result := make(map[string][]float32)
	for _, key := range keys {
		if vector, ok := f.vectors[key]; ok {
			result[key] = append([]float32(nil), vector...)
		}
	}
	return result, nil
}

func TestBackfillItemVectorsDryRunApplyAndIdempotence(t *testing.T) {
	inline := score.EncodeVector([]float32{1, 0})
	existing := score.EncodeVector([]float32{0, 1})
	repository := &fakeItemVectors{
		items: []domain.Item{
			{ItemID: "separated", TTL: 42},
			{ItemID: "inline", Vector: inline, TTL: 42},
			{ItemID: "external", TTL: 42},
			{ItemID: "unavailable", TTL: 42},
		},
		vectorRows: map[string][]byte{"separated": existing},
	}
	vectors := &fakeVectorSource{vectors: map[string][]float32{"external": {0.5, 0.5}}}

	result, err := run(context.Background(), repository, vectors, false, 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 4 || result.Missing != 3 || result.FromInline != 1 || result.FromS3 != 1 || result.Unavailable != 1 || result.Written != 0 {
		t.Fatalf("dry-run result = %#v", result)
	}
	if repository.writes != 0 || !reflect.DeepEqual(vectors.requests, [][]string{{"external", "unavailable"}}) {
		t.Fatalf("dry-run writes = %d, requests = %#v", repository.writes, vectors.requests)
	}

	result, err = run(context.Background(), repository, vectors, true, 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.Written != 2 || repository.writes != 2 {
		t.Fatalf("apply result = %#v, writes = %d", result, repository.writes)
	}
	if !reflect.DeepEqual(repository.vectorRows["inline"], inline) {
		t.Fatalf("inline vector = %#v", repository.vectorRows["inline"])
	}
	if !reflect.DeepEqual(repository.vectorRows["external"], score.EncodeVector([]float32{0.5, 0.5})) {
		t.Fatalf("external vector = %#v", repository.vectorRows["external"])
	}

	result, err = run(context.Background(), repository, vectors, true, 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.Missing != 1 || result.Unavailable != 1 || result.Written != 0 || repository.writes != 2 {
		t.Fatalf("second apply result = %#v, writes = %d", result, repository.writes)
	}
}

func TestBackfillItemVectorsRejectsInvalidConcurrency(t *testing.T) {
	_, err := run(context.Background(), &fakeItemVectors{}, &fakeVectorSource{}, false, 0)
	if err == nil {
		t.Fatal("zero concurrency succeeded")
	}
}
