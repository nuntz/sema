package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"net/http"
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
	if got := client.headers.Get("Accept"); got != imageAccept {
		t.Fatalf("Accept = %q, want %q", got, imageAccept)
	}
	if lead.Width != 300 || lead.Height != 200 {
		t.Fatalf("lead dimensions = %dx%d, want 300x200", lead.Width, lead.Height)
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
