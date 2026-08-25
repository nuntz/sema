package main

import (
	"context"
	"image"
	"slices"
	"testing"

	"github.com/nuntz/sema/internal/media"
)

type recordingContentWriter struct{ keys []string }

func (w *recordingContentWriter) PutContent(_ context.Context, key, _ string, _ []byte) error {
	w.keys = append(w.keys, key)
	return nil
}

func TestStoreLeadWritesResponsiveKeysAndManifest(t *testing.T) {
	lead, err := media.EncodeLead(image.NewRGBA(image.Rect(0, 0, 1600, 1000)))
	if err != nil {
		t.Fatal(err)
	}
	writer := &recordingContentWriter{}
	base := "media/user/item/lead.jpg"
	variants, err := storeLead(context.Background(), writer, base, lead)
	if err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"media/user/item/lead-384.jpg", "media/user/item/lead-768.jpg", base}
	if !slices.Equal(writer.keys, wantKeys) {
		t.Fatalf("stored keys = %#v, want %#v", writer.keys, wantKeys)
	}
	if len(variants) != 3 || variants[0].Key != wantKeys[0] || variants[0].Width != 384 || variants[1].Key != wantKeys[1] || variants[1].Width != 768 || variants[2].Key != base || variants[2].Width != 1280 {
		t.Fatalf("manifest = %#v", variants)
	}
}
