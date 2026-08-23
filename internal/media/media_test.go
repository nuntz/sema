package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/nuntz/sema/internal/httpx"
)

type fakeClient struct {
	response httpx.Response
	headers  http.Header
}

func (c *fakeClient) Get(_ context.Context, _ string, headers http.Header) (httpx.Response, error) {
	c.headers = headers.Clone()
	return c.response, nil
}

func TestFetchLeadRequestsAndAcceptsImages(t *testing.T) {
	var body bytes.Buffer
	if err := jpeg.Encode(&body, image.NewRGBA(image.Rect(0, 0, 300, 200)), nil); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{response: httpx.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
		Body:       body.Bytes(),
	}}

	lead, err := (&Processor{client: client}).FetchLead(context.Background(), []string{"https://example.com/lead.jpg"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := client.headers.Get("Accept"), "image/webp,image/jpeg,image/png"; got != want {
		t.Fatalf("Accept = %q, want %q", got, want)
	}
	if lead.Width != 300 || lead.Height != 200 {
		t.Fatalf("lead dimensions = %dx%d, want 300x200", lead.Width, lead.Height)
	}
}

func TestAdvertisedImageFormatsAreDecodable(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var jpegBody, pngBody bytes.Buffer
	if err := jpeg.Encode(&jpegBody, source, nil); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&pngBody, source); err != nil {
		t.Fatal(err)
	}
	webpBody, err := base64.StdEncoding.DecodeString("UklGRiQAAABXRUJQVlA4IBgAAAAwAQCdASoBAAEAAgA0JaQAA3AA/vuUAAA=")
	if err != nil {
		t.Fatal(err)
	}

	fixtures := map[string][]byte{
		"image/jpeg": jpegBody.Bytes(),
		"image/png":  pngBody.Bytes(),
		"image/webp": webpBody,
	}
	for _, mediaType := range strings.Split(imageAccept, ",") {
		body, ok := fixtures[mediaType]
		if !ok {
			t.Errorf("advertised image format %q has no decoder fixture", mediaType)
			continue
		}
		if _, _, err := image.DecodeConfig(bytes.NewReader(body)); err != nil {
			t.Errorf("advertised image format %q is not decodable: %v", mediaType, err)
		}
		delete(fixtures, mediaType)
	}
	for mediaType := range fixtures {
		t.Errorf("decoder fixture %q is not advertised", mediaType)
	}
}

func TestFetchLeadRejectsNonImageContentType(t *testing.T) {
	client := &fakeClient{response: httpx.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:       []byte("<html></html>"),
	}}

	_, err := (&Processor{client: client}).FetchLead(context.Background(), []string{"https://example.com/lead.jpg"})
	var leadErr *LeadError
	if !errors.As(err, &leadErr) {
		t.Fatalf("error = %v, want *LeadError", err)
	}
	if leadErr.URL != "https://example.com/lead.jpg" {
		t.Fatalf("URL = %q", leadErr.URL)
	}
	if leadErr.ContentType != "text/html; charset=utf-8" {
		t.Fatalf("ContentType = %q", leadErr.ContentType)
	}
}

func TestCandidatesFallsBackToFeedContentImage(t *testing.T) {
	pageURL, _ := url.Parse("https://www.reddit.com/comments/one")
	feedURL, _ := url.Parse("https://www.reddit.com/")
	pageHTML := []byte(`<html><head><meta property="og:image" content="https://cdn.example.com/page.jpg"></head></html>`)
	feedHTML := []byte(`<table><tr><td><img src="/preview.jpeg?width=640"></td></tr></table>`)

	candidates := Candidates(nil, pageHTML, nil, feedHTML, "", pageURL, feedURL)
	want := []string{"https://cdn.example.com/page.jpg", "https://www.reddit.com/preview.jpeg?width=640"}
	if !slices.Equal(candidates, want) {
		t.Fatalf("candidates = %#v, want %#v", candidates, want)
	}
}
