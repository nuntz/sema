package send

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

var sentAt = time.Date(2026, 9, 26, 18, 4, 11, 0, time.UTC)

func compactJSON(t *testing.T, raw string) string {
	t.Helper()
	var out bytes.Buffer
	if err := json.Compact(&out, []byte(raw)); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestItemPayloadRedditLinkPostSendsTheExternalLinkAndTheThread(t *testing.T) {
	item := domain.Item{
		ItemID: "item-1", FeedID: "feed-1", Connector: "reddit", PostType: "link",
		URL: "https://www.reddit.com/r/maps/comments/abc/", ExternalURL: "https://example.com/article",
		Title: "A map", Summary: "Short summary.", Author: "cartographer",
		PublishedTS:    "2026-09-25T09:00:00.000000000Z",
		MediaSourceURL: "https://example.com/hero.jpg", MediaW: 1600, MediaH: 900,
		ArchiveSK: "A#item-1",
	}
	feed := domain.Feed{FeedID: "feed-1", Connector: "reddit", URL: "https://www.reddit.com/r/maps/.rss", SiteURL: "https://www.reddit.com/r/maps/", Title: "r/maps", Tags: []string{"maps", "geo"}}

	got, err := ItemPayload(item, feed, "receiver-user", sentAt, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := compactJSON(t, `{
		"version": 1,
		"event": "item.send",
		"sent_at": "2026-09-26T18:04:11Z",
		"user_id": "receiver-user",
		"item": {
			"id": "item-1",
			"url": "https://example.com/article",
			"discussion_url": "https://www.reddit.com/r/maps/comments/abc/",
			"title": "A map",
			"summary": "Short summary.",
			"author": "cartographer",
			"published_at": "2026-09-25T09:00:00Z",
			"feed": {"connector": "reddit", "title": "r/maps", "url": "https://www.reddit.com/r/maps/"},
			"images": [{"url": "https://example.com/hero.jpg", "role": "hero"}],
			"kept": true,
			"tags": ["geo", "maps"]
		}
	}`)
	if string(got) != want {
		t.Fatalf("payload =\n%s\nwant\n%s", got, want)
	}
}

func TestItemPayloadOmitsAbsentOptionalFields(t *testing.T) {
	item := domain.Item{ItemID: "item-2", FeedID: "feed-2", Connector: "rss", URL: "https://example.com/post", Title: "Post", PublishedTS: "not a time", MediaSourceURL: "/relative.jpg"}
	feed := domain.Feed{FeedID: "feed-2", URL: "https://example.com/feed.xml"}

	got, err := ItemPayload(item, feed, "receiver-user", sentAt, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := compactJSON(t, `{
		"version": 1, "event": "item.send", "sent_at": "2026-09-26T18:04:11Z", "user_id": "receiver-user",
		"item": {
			"id": "item-2", "url": "https://example.com/post", "title": "Post",
			"feed": {"connector": "rss", "url": "https://example.com/feed.xml"},
			"images": [], "kept": false, "tags": []
		}
	}`)
	if string(got) != want {
		t.Fatalf("payload =\n%s\nwant\n%s", got, want)
	}
}

func TestPingPayloadCarriesOnlyTheEnvelope(t *testing.T) {
	got, err := PingPayload("receiver-user", sentAt)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":1,"event":"ping","sent_at":"2026-09-26T18:04:11Z","user_id":"receiver-user"}`
	if string(got) != want {
		t.Fatalf("payload = %s, want %s", got, want)
	}
}

func TestItemPayloadTruncatesTextToFitTheSizeCap(t *testing.T) {
	title := strings.Repeat("é", 70_000)
	item := domain.Item{ItemID: "item-3", URL: "https://example.com/long", Title: title, Summary: strings.Repeat("s", 400)}

	got, err := ItemPayload(item, domain.Feed{}, "receiver-user", sentAt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 64*1024 {
		t.Fatalf("payload is %d bytes, want at most 65536", len(got))
	}
	var decoded struct {
		Item struct {
			ID, URL, Title string
		}
	}
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Item.ID != "item-3" || decoded.Item.URL != "https://example.com/long" {
		t.Fatalf("identity fields changed: %+v", decoded.Item)
	}
	if decoded.Item.Title == "" || !strings.HasPrefix(title, decoded.Item.Title) {
		t.Fatalf("title was not truncated to a prefix: %d runes", len([]rune(decoded.Item.Title)))
	}
}

func TestItemPayloadAppendsSemasSignedCopyAfterTheOrigin(t *testing.T) {
	item := domain.Item{ItemID: "item-4", URL: "https://example.com/post", MediaSourceURL: "https://example.com/hero.jpg", MediaW: 2400, MediaH: 1600}
	copyImage := &CopyImage{URL: "https://sema.example/media/user/item-4/lead-1280.jpg?Signature=abc", Width: 1280, Height: 853, ExpiresAt: sentAt.Add(time.Hour)}

	got, err := ItemPayload(item, domain.Feed{}, "receiver-user", sentAt, copyImage)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Item struct {
			Images json.RawMessage `json:"images"`
		} `json:"item"`
	}
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	want := compactJSON(t, `[
		{"url": "https://example.com/hero.jpg", "role": "hero"},
		{"url": "https://sema.example/media/user/item-4/lead-1280.jpg?Signature=abc", "width": 1280, "height": 853, "role": "copy", "expires_at": "2026-09-26T19:04:11Z"}
	]`)
	if string(decoded.Item.Images) != want {
		t.Fatalf("images = %s, want %s", decoded.Item.Images, want)
	}
}

func TestItemPayloadSendsOnlyTheCopyWhenNoOriginIsStored(t *testing.T) {
	item := domain.Item{ItemID: "item-5", URL: "https://example.com/post"}
	copyImage := &CopyImage{URL: "https://sema.example/archive/user/item-5/lead.jpg?Signature=abc", ExpiresAt: sentAt.Add(time.Hour)}

	got, err := ItemPayload(item, domain.Feed{}, "receiver-user", sentAt, copyImage)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"images":[{"url":"https://sema.example/archive/user/item-5/lead.jpg?Signature=abc","role":"copy","expires_at":"2026-09-26T19:04:11Z"}]`) {
		t.Fatalf("payload = %s", got)
	}
}

func TestItemPayloadDropsOptionalFieldsBeforeExceedingTheCap(t *testing.T) {
	item := domain.Item{ItemID: "item-6", URL: "https://example.com/a", Title: "Title", Author: strings.Repeat("a", 80_000)}
	feed := domain.Feed{FeedID: "feed", Tags: []string{strings.Repeat("t", 70_000)}}

	got, err := ItemPayload(item, feed, "receiver-user", sentAt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > MaxPayloadBytes || !strings.Contains(string(got), `"id":"item-6","url":"https://example.com/a"`) {
		t.Fatalf("payload is %d bytes: %.200s", len(got), got)
	}
}

func TestItemPayloadRefusesAnItemThatCannotFit(t *testing.T) {
	item := domain.Item{ItemID: "item-7", URL: "https://example.com/" + strings.Repeat("u", 70_000)}

	if got, err := ItemPayload(item, domain.Feed{}, "receiver-user", sentAt, nil); err == nil {
		t.Fatalf("payload of %d bytes built without error", len(got))
	}
}
