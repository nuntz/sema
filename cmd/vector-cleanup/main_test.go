package main

import (
	"context"
	"testing"

	"github.com/nuntz/sema/internal/vectorstore"
)

type cleanupStore struct {
	vectorstore.Store
	deleted int
	size    int
	calls   int
}

func (s *cleanupStore) Cleanup(context.Context, int64) (int, int, error) {
	s.calls++
	return s.deleted, s.size, nil
}

func TestCleanupRunsBothVectorIndexes(t *testing.T) {
	textStore := &cleanupStore{deleted: 2, size: 10}
	imageStore := &cleanupStore{deleted: 3, size: 9}
	events := []map[string]float64{}
	h := &handler{vectors: textStore, imageVectors: imageStore, emit: func(metrics map[string]float64, _ map[string]string) {
		events = append(events, metrics)
	}}
	if err := h.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if textStore.calls != 1 || imageStore.calls != 1 || len(events) != 2 {
		t.Fatalf("cleanup calls/events = %d/%d/%#v", textStore.calls, imageStore.calls, events)
	}
	if events[0]["VectorIndexSize"] != 8 || events[0]["VectorsDeleted"] != 2 || events[1]["ImageVectorIndexSize"] != 6 || events[1]["ImageVectorsDeleted"] != 3 {
		t.Fatalf("metrics = %#v", events)
	}
}
