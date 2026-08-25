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

type mappedClient struct{ responses map[string]httpx.Response }

func (c *mappedClient) Get(_ context.Context, rawURL string, _ http.Header) (httpx.Response, error) {
	return c.responses[rawURL], nil
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

func TestEncodeLeadFitsBothDimensionsAndNeverUpscales(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		want          [][2]int
	}{
		{name: "landscape", width: 2400, height: 1600, want: [][2]int{{384, 256}, {768, 512}, {1280, 853}}},
		{name: "portrait", width: 1600, height: 2400, want: [][2]int{{256, 384}, {512, 768}, {853, 1280}}},
		{name: "small source skips larger boxes", width: 500, height: 333, want: [][2]int{{384, 255}, {500, 333}}},
		{name: "source within smallest box", width: 300, height: 200, want: [][2]int{{300, 200}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lead, err := EncodeLead(image.NewRGBA(image.Rect(0, 0, test.width, test.height)))
			if err != nil {
				t.Fatal(err)
			}
			if len(lead.Variants) != len(test.want) {
				t.Fatalf("variants = %#v, want dimensions %#v", lead.Variants, test.want)
			}
			for index, want := range test.want {
				variant := lead.Variants[index]
				if variant.Width != want[0] || variant.Height != want[1] {
					t.Errorf("variant %d = %dx%d, want %dx%d", index, variant.Width, variant.Height, want[0], want[1])
				}
				configuration, format, err := image.DecodeConfig(bytes.NewReader(variant.Bytes))
				if err != nil || format != "jpeg" || configuration.Width != want[0] || configuration.Height != want[1] {
					t.Errorf("encoded variant %d = %#v, %q, %v", index, configuration, format, err)
				}
			}
			largest := test.want[len(test.want)-1]
			if lead.Width != largest[0] || lead.Height != largest[1] || lead.Width > 1280 || lead.Height > 1280 {
				t.Errorf("largest = %dx%d, want %dx%d within cap", lead.Width, lead.Height, largest[0], largest[1])
			}
		})
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

func TestFetchVideoLeadFallsBackAndCropsFourByThreeThumbnail(t *testing.T) {
	var body bytes.Buffer
	if err := jpeg.Encode(&body, image.NewRGBA(image.Rect(0, 0, 480, 360)), nil); err != nil {
		t.Fatal(err)
	}
	maxres := "https://i.ytimg.com/vi/video/maxresdefault.jpg"
	hq := "https://i.ytimg.com/vi/video/hqdefault.jpg"
	client := &mappedClient{responses: map[string]httpx.Response{
		maxres: {StatusCode: http.StatusNotFound, Header: http.Header{"Content-Type": []string{"text/html"}}},
		hq:     {StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: body.Bytes()},
	}}
	lead, err := (&Processor{client: client}).FetchVideoLead(context.Background(), []string{maxres, hq})
	if err != nil {
		t.Fatal(err)
	}
	if lead.SourceURL != hq || lead.Width != 480 || lead.Height != 270 {
		t.Fatalf("lead = %#v", lead)
	}
}

func TestAvatarIsCentreCroppedToNinetySixPixels(t *testing.T) {
	var body bytes.Buffer
	if err := png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 200, 100))); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{response: httpx.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: body.Bytes()}}
	avatar, err := (&Processor{client: client}).Avatar(context.Background(), "https://example.com/avatar.png")
	if err != nil {
		t.Fatal(err)
	}
	if avatar.Width != 96 || avatar.Height != 96 || avatar.ContentType != "image/png" {
		t.Fatalf("avatar = %#v", avatar)
	}
}
