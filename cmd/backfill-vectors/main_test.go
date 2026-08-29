package main

import (
	"context"
	"testing"

	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/vectorstore"
)

type backfillItems struct {
	live, archive []domain.Item
}

func (*backfillItems) UserIDs(context.Context) ([]string, error) { return []string{"user"}, nil }
func (s *backfillItems) LiveItems(context.Context, string) ([]domain.Item, error) {
	return s.live, nil
}
func (s *backfillItems) ArchiveItems(context.Context, string) ([]domain.Item, error) {
	return s.archive, nil
}
func (*backfillItems) LoadItemVectors(context.Context, string, []domain.Item) error { return nil }

type backfillVectors struct {
	putCalls   int
	batchCalls int
	records    []vectorstore.Record
}

func (s *backfillVectors) Put(context.Context, vectorstore.Record) error {
	s.putCalls++
	return nil
}
func (s *backfillVectors) PutBatch(_ context.Context, records []vectorstore.Record) error {
	s.batchCalls++
	s.records = append(s.records, records...)
	return nil
}
func (*backfillVectors) Delete(context.Context, string) error           { return nil }
func (*backfillVectors) Get(context.Context, string) ([]float32, error) { return nil, nil }
func (*backfillVectors) Query(context.Context, []float32, int, int64) ([]vectorstore.Match, error) {
	return nil, nil
}
func (*backfillVectors) Cleanup(context.Context, int64) (int, int, error) { return 0, 0, nil }

func TestRunBatchesAllVectorsForUser(t *testing.T) {
	repository := &backfillItems{
		live:    []domain.Item{{ItemID: "one", Vector: []byte{0, 0, 128, 63}}, {ItemID: "two", Vector: []byte{0, 0, 128, 63}}},
		archive: []domain.Item{{ItemID: "kept", Vector: []byte{0, 0, 128, 63}}},
	}
	vectors := &backfillVectors{}
	live, archive, err := run(context.Background(), repository, vectors, true)
	if err != nil || live != 2 || archive != 1 {
		t.Fatalf("run = %d, %d, %v", live, archive, err)
	}
	if vectors.putCalls != 0 || vectors.batchCalls != 1 || len(vectors.records) != 3 {
		t.Fatalf("writes = puts %d batches %d records %#v", vectors.putCalls, vectors.batchCalls, vectors.records)
	}
	if vectors.records[0].Kind != vectorstore.KindLive || vectors.records[2].Kind != vectorstore.KindArchive {
		t.Fatalf("record kinds = %#v", vectors.records)
	}
}
