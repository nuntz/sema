package main

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/nuntz/sema/internal/discovery"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/httpx"
)

const redditEndedText = "Reddit ended its public feeds, so Sema can’t follow subreddits."

// redditInputs are the ways a user can enter a subreddit, or another Reddit
// address, when adding a Feed.
var redditInputs = []string{
	"r/castles",
	"/r/castles",
	"reddit.com/r/castles",
	"https://www.reddit.com/r/castles/",
	"https://old.reddit.com/r/castles",
	"https://new.reddit.com/r/castles/",
	"https://www.reddit.com/r/castles/.rss",
	"https://www.reddit.com/r/castles/top.rss?t=day",
	"https://old.reddit.com/r/castles/new.rss",
	"https://www.reddit.com/r/castles/comments/abc123/a_castle/",
	"https://www.reddit.com/user/someone/",
}

// recordingFetcher stands in for the network and records every request.
type recordingFetcher struct {
	mu   sync.Mutex
	urls []string
}

func (f *recordingFetcher) Get(_ context.Context, rawURL string, _ http.Header) (httpx.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.urls = append(f.urls, rawURL)
	return httpx.Response{StatusCode: http.StatusOK, Body: []byte(`<rss version="2.0"><channel><title>x</title></channel></rss>`)}, nil
}

func assertRedditEnded(t *testing.T, got int, body string) {
	t.Helper()
	var payload struct {
		Error string `json:"error"`
		Kind  string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("body = %s: %v", body, err)
	}
	if got != http.StatusUnprocessableEntity || payload.Kind != "reddit_ended" || payload.Error != redditEndedText {
		t.Fatalf("status = %d, body = %s", got, body)
	}
}

func TestDiscoveryRefusesRedditWithoutContactingIt(t *testing.T) {
	for _, input := range redditInputs {
		t.Run(input, func(t *testing.T) {
			network := &recordingFetcher{}
			server := &server{store: &fakeAPIStore{}, discover: discovery.New(network, true)}
			body, _ := json.Marshal(map[string]string{"url": input})
			got := server.discoverFeeds(context.Background(), string(body))
			assertRedditEnded(t, got.StatusCode, got.Body)
			if len(network.urls) != 0 {
				t.Fatalf("discovery requested %v", network.urls)
			}
		})
	}
}

func TestImportListsSubredditsAsUnsupportedAndImportsTheRest(t *testing.T) {
	saved := []string{}
	state := &fakeSnapshotStore{putFeed: func(_ context.Context, feed domain.Feed) error {
		saved = append(saved, feed.URL)
		return nil
	}}
	server := &server{store: &fakeAPIStore{}, snapshotStore: state, queue: &apiQueue{}}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "subscriptions.opml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = part.Write([]byte(`<?xml version="1.0"?><opml version="2.0"><body>
		<outline text="Example" xmlUrl="https://example.com/feed.xml"/>
		<outline text="Castles" xmlUrl="https://www.reddit.com/r/castles/top.rss?t=day"/>
		<outline text="Old pics" xmlUrl="https://old.reddit.com/r/pics/.rss"/>
	</body></opml>`))
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	got := server.importFeeds(context.Background(), server.requestSnapshot("user"), events.APIGatewayV2HTTPRequest{
		Body: body.String(), Headers: map[string]string{"content-type": writer.FormDataContentType()},
	})
	var result importFeedsResult
	if err := json.Unmarshal([]byte(got.Body), &result); err != nil || got.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s: %v", got.StatusCode, got.Body, err)
	}
	want := []unsupportedFeed{
		{Title: "Castles", URL: "https://www.reddit.com/r/castles/top.rss?t=day", Reason: redditEndedText},
		{Title: "Old pics", URL: "https://old.reddit.com/r/pics/.rss", Reason: redditEndedText},
	}
	if result.Imported != 1 || !slices.Equal(result.Unsupported, want) {
		t.Fatalf("result = %+v", result)
	}
	if !slices.Equal(saved, []string{"https://example.com/feed.xml"}) {
		t.Fatalf("saved = %v", saved)
	}
}
