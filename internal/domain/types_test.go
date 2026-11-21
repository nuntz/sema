package domain

import (
	"testing"
	"time"
)

func TestTimestampSortsChronologically(t *testing.T) {
	earlier := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	later := earlier.Add(500 * time.Millisecond)
	if Timestamp(earlier) >= Timestamp(later) {
		t.Fatalf("timestamps are not lexically sortable: %s >= %s", Timestamp(earlier), Timestamp(later))
	}
	if ItemSK(earlier, "z") >= ItemSK(later, "a") {
		t.Fatal("item sort keys are not chronological")
	}
}

func TestFeedConnectorDefaultsLegacyRowsToRSS(t *testing.T) {
	if got := FeedConnector(Feed{}); got != ConnectorRSS {
		t.Fatalf("legacy connector = %q, want %q", got, ConnectorRSS)
	}
	if got := FeedConnector(Feed{Connector: ConnectorYouTube}); got != ConnectorYouTube {
		t.Fatalf("YouTube connector = %q, want %q", got, ConnectorYouTube)
	}
}

func TestPresentWithFeedLendsTheFeedsConnectorTitleAndFavicon(t *testing.T) {
	stored := Item{FeedID: "f", Connector: ConnectorYouTube, FeedTitle: "Stored title", FaviconKey: "favicons/stored.png"}
	cases := []struct {
		name string
		feed Feed
		want Item
	}{
		{
			name: "custom title wins over fetched title",
			feed: Feed{FeedID: "f", Connector: ConnectorReddit, Title: "r/castles", CustomTitle: "Castles", FaviconKey: "favicons/reddit.png"},
			want: Item{FeedID: "f", Connector: ConnectorReddit, FeedTitle: "Castles", FaviconKey: "favicons/reddit.png"},
		},
		{
			name: "fetched title when there is no custom title",
			feed: Feed{FeedID: "f", Connector: ConnectorReddit, Title: "r/castles"},
			want: Item{FeedID: "f", Connector: ConnectorReddit, FeedTitle: "r/castles", FaviconKey: "favicons/stored.png"},
		},
		{
			name: "a legacy Feed without title or favicon keeps the Item's own and reads as RSS",
			feed: Feed{FeedID: "f"},
			want: Item{FeedID: "f", Connector: ConnectorRSS, FeedTitle: "Stored title", FaviconKey: "favicons/stored.png"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PresentWithFeed(stored, tc.feed)
			if got.FeedID != tc.want.FeedID || got.Connector != tc.want.Connector || got.FeedTitle != tc.want.FeedTitle || got.FaviconKey != tc.want.FaviconKey {
				t.Fatalf("PresentWithFeed = %+v, want %+v", got, tc.want)
			}
		})
	}
}
