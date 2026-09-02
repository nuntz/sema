package main

import (
	"context"
	"errors"
	"image"
	"slices"
	"strings"
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

type fakeBodyImageFetcher struct {
	calls []string
}

func (f *fakeBodyImageFetcher) FetchBodyImage(_ context.Context, sourceURL string) (media.Image, error) {
	f.calls = append(f.calls, sourceURL)
	if strings.Contains(sourceURL, "failed") {
		return media.Image{}, errors.New("publisher unavailable")
	}
	return media.Image{Bytes: []byte("webp"), ContentType: "image/webp", Extension: ".webp", SourceURL: sourceURL}, nil
}

type recordingBodyImageWriter struct {
	objects map[string][]byte
}

func (w *recordingBodyImageWriter) PutContent(_ context.Context, key, contentType string, body []byte) error {
	if contentType != "image/webp" {
		return errors.New("unexpected content type")
	}
	w.objects[key] = slices.Clone(body)
	return nil
}

func (*recordingBodyImageWriter) ContentURL(key string) string {
	return "https://content.example/" + key
}

func TestCacheBodyImagesStoresPublisherImagesAndLeavesMediaCardsAndFailuresUntouched(t *testing.T) {
	raw := `<img src="https://publisher.example/first.png" srcset="https://publisher.example/first-large.png 2x">` +
		`<a class="media-card"><span class="media-card-thumbnail"><img src="https://video.example/poster.jpg" srcset="https://video.example/poster-large.jpg 2x"></span></a>` +
		`<img src="https://publisher.example/second.jpg">` +
		`<img src="https://publisher.example/failed.png" srcset="https://publisher.example/failed-large.png 2x">`
	fetcher := &fakeBodyImageFetcher{}
	writer := &recordingBodyImageWriter{objects: make(map[string][]byte)}

	resolved, succeeded, failed, failures := cacheBodyImages(context.Background(), fetcher, writer, "user", "item", raw)
	if succeeded != 2 || failed != 1 || len(failures) != 1 {
		t.Fatalf("counts = %d succeeded, %d failed, failures %v", succeeded, failed, failures)
	}
	wantCalls := []string{"https://publisher.example/first.png", "https://publisher.example/second.jpg", "https://publisher.example/failed.png"}
	if !slices.Equal(fetcher.calls, wantCalls) {
		t.Fatalf("fetches = %#v, want %#v", fetcher.calls, wantCalls)
	}
	for _, key := range []string{"media/user/item/body-0.webp", "media/user/item/body-1.webp"} {
		if string(writer.objects[key]) != "webp" {
			t.Errorf("stored %q = %q", key, writer.objects[key])
		}
		if !strings.Contains(resolved, `src="https://content.example/`+key+`"`) {
			t.Errorf("body does not reference %q: %s", key, resolved)
		}
	}
	if len(writer.objects) != 2 {
		t.Fatalf("stored objects = %#v", writer.objects)
	}
	for _, unchanged := range []string{
		`<img src="https://video.example/poster.jpg" srcset="https://video.example/poster-large.jpg 2x"/>`,
		`<img src="https://publisher.example/failed.png" srcset="https://publisher.example/failed-large.png 2x"/>`,
	} {
		if !strings.Contains(resolved, unchanged) {
			t.Errorf("body does not preserve %q: %s", unchanged, resolved)
		}
	}
	if strings.Contains(resolved, "first-large.png") {
		t.Fatalf("cached body image retained srcset: %s", resolved)
	}
}
