package main

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

type fakeMediaVariantStore struct {
	live         []domain.Item
	archive      []domain.Item
	objects      map[string][]byte
	contentReads int
	writes       []string
	updates      int
}

func (*fakeMediaVariantStore) UserIDs(context.Context) ([]string, error) {
	return []string{"user"}, nil
}
func (f *fakeMediaVariantStore) LiveItems(context.Context, string) ([]domain.Item, error) {
	return append([]domain.Item(nil), f.live...), nil
}
func (f *fakeMediaVariantStore) ArchiveItems(context.Context, string) ([]domain.Item, error) {
	return append([]domain.Item(nil), f.archive...), nil
}
func (f *fakeMediaVariantStore) Content(_ context.Context, key string) ([]byte, string, error) {
	f.contentReads++
	return f.objects[key], "image/jpeg", nil
}
func (f *fakeMediaVariantStore) PutContent(_ context.Context, key, _ string, body []byte) error {
	f.writes = append(f.writes, key)
	f.objects[key] = append([]byte(nil), body...)
	return nil
}
func (f *fakeMediaVariantStore) UpdateMediaVariants(_ context.Context, item domain.Item, variants []domain.MediaVariant, width, height int) error {
	f.updates++
	for index := range f.live {
		if f.live[index].PK == item.PK && f.live[index].SK == item.SK {
			f.live[index].MediaVariants = variants
			f.live[index].MediaW, f.live[index].MediaH = width, height
		}
	}
	for index := range f.archive {
		if f.archive[index].PK == item.PK && f.archive[index].SK == item.SK {
			f.archive[index].MediaVariants = variants
			f.archive[index].MediaW, f.archive[index].MediaH = width, height
		}
	}
	return nil
}

func TestBackfillMediaVariantsIsIdempotentAndCapsPortraits(t *testing.T) {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1600, 2400)), &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	item := domain.Item{PK: "U#user", SK: "I#item", ItemID: "item", MediaKey: "media/user/item/lead.jpg", MediaW: 1600, MediaH: 2400}
	repository := &fakeMediaVariantStore{live: []domain.Item{item}, objects: map[string][]byte{item.MediaKey: encoded.Bytes()}}

	total, affected, err := run(context.Background(), repository, true, 0)
	if err != nil || total != 1 || affected != 1 {
		t.Fatalf("first run = total %d affected %d err %v", total, affected, err)
	}
	if repository.contentReads != 1 || repository.updates != 1 || len(repository.writes) != 3 {
		t.Fatalf("first writes = %#v reads %d updates %d", repository.writes, repository.contentReads, repository.updates)
	}
	updated := repository.live[0]
	if updated.MediaW != 853 || updated.MediaH != 1280 || len(updated.MediaVariants) != 3 {
		t.Fatalf("updated item = %#v", updated)
	}
	if repository.writes[2] != item.MediaKey {
		t.Fatalf("largest key = %q, want %q", repository.writes[2], item.MediaKey)
	}

	total, affected, err = run(context.Background(), repository, true, time.Millisecond)
	if err != nil || total != 1 || affected != 0 || repository.contentReads != 1 || repository.updates != 1 || len(repository.writes) != 3 {
		t.Fatalf("second run = total %d affected %d reads %d writes %d updates %d err %v", total, affected, repository.contentReads, len(repository.writes), repository.updates, err)
	}
}

func TestBackfillMediaVariantsDryRunDoesNotReadContent(t *testing.T) {
	item := domain.Item{PK: "U#user", SK: "A#item", ItemID: "item", MediaKey: "archive/user/item/lead.webp"}
	repository := &fakeMediaVariantStore{archive: []domain.Item{item}, objects: map[string][]byte{}}
	total, affected, err := run(context.Background(), repository, false, 50*time.Millisecond)
	if err != nil || total != 1 || affected != 1 || repository.contentReads != 0 || len(repository.writes) != 0 || repository.updates != 0 {
		t.Fatalf("dry run = total %d affected %d reads %d writes %d updates %d err %v", total, affected, repository.contentReads, len(repository.writes), repository.updates, err)
	}
}
