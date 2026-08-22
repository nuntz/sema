package rss

import (
	"strings"
	"testing"
)

func TestParseOPML(t *testing.T) {
	raw := `<?xml version="1.0"?><opml version="2.0"><body><outline text="Tech"><outline title="Example" type="rss" xmlUrl="https://example.com/feed.xml"/><outline text="Duplicate" xmlUrl="https://example.com/feed.xml"/></outline></body></opml>`
	feeds, err := ParseOPML(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 || feeds[0].Title != "Example" {
		t.Fatalf("feeds = %#v", feeds)
	}
}

func TestUnsupportedReason(t *testing.T) {
	for _, feedURL := range []string{
		"https://www.youtube.com/feeds/videos.xml?channel_id=UC123",
		"https://youtube.com/feeds/videos.xml?playlist_id=PL123",
		"https://M.YOUTUBE.COM/feeds/videos.xml/?user=example",
	} {
		if reason := UnsupportedReason(feedURL); reason != YouTubeUnsupportedReason {
			t.Errorf("UnsupportedReason(%q) = %q", feedURL, reason)
		}
	}
	for _, feedURL := range []string{
		"https://www.youtube.com/watch?v=123",
		"https://notyoutube.com/feeds/videos.xml",
		"https://example.com/feeds/videos.xml",
	} {
		if reason := UnsupportedReason(feedURL); reason != "" {
			t.Errorf("UnsupportedReason(%q) = %q, want empty", feedURL, reason)
		}
	}
}
