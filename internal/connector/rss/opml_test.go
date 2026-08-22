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
