package main

import (
	"context"
	"encoding/json"
	"github.com/nuntz/sema/internal/domain"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPatchCadencePinsAndAuto(t *testing.T) {
	for _, test := range []struct {
		body, connector     string
		wantStatus, wantPin int
	}{
		{`{"fetch_interval_h":null}`, domain.ConnectorRSS, 200, 0},
		{`{"fetch_interval_h":3}`, domain.ConnectorRSS, 200, 3},
		{`{"fetch_interval_h":1}`, domain.ConnectorRSS, 200, 1},
		{`{"fetch_interval_h":5}`, domain.ConnectorRSS, 400, 24},
		{`{"fetch_interval_h":0}`, domain.ConnectorRSS, 400, 24},
		{`{"fetch_interval_h":1}`, domain.ConnectorReddit, 400, 24},
		{`{"custom_title":"Retain my pin"}`, domain.ConnectorRSS, 200, 24},
	} {
		t.Run(test.body+test.connector, func(t *testing.T) {
			feed := domain.Feed{PK: "U#user", SK: "F#feed", FeedID: "feed", Connector: test.connector, FetchIntervalH: 24, HistoryStartedAt: domain.Timestamp(time.Now().Add(-8 * 24 * time.Hour))}
			db := &fakeAPIStore{feed: func(context.Context, string, string) (domain.Feed, error) { return feed, nil }, putFeed: func(_ context.Context, updated domain.Feed) error { feed = updated; return nil }}
			s := &server{store: db}
			got := s.patchFeed(context.Background(), "user", "feed", test.body)
			if got.StatusCode != test.wantStatus || feed.FetchIntervalH != test.wantPin {
				t.Fatalf("response=%+v feed=%+v", got, feed)
			}
			if got.StatusCode == http.StatusOK {
				var public domain.Feed
				if err := json.Unmarshal([]byte(got.Body), &public); err != nil {
					t.Fatal(err)
				}
				if (public.CadencePin == nil) != (test.wantPin == 0) || public.EffectiveCadenceH != domain.FeedIntervalHours(feed) {
					t.Fatalf("public=%+v", public)
				}
			}
		})
	}
}

func TestFeedListExcludesLinkItemsAndExposesRefusals(t *testing.T) {
	since := domain.Timestamp(time.Now().Add(-25 * time.Hour))
	db := &fakeAPIStore{feeds: func(context.Context, string) ([]domain.Feed, error) {
		return []domain.Feed{
			{FeedID: "mixed", EffectiveCadenceH: 6, RefusedSince: since, BodyOutcomes: strings.Repeat("0", 45) + strings.Repeat("1", 5), ItemCount: 60, ExtractionSample: 60, LinkItemCount: 50, ExtractionFailures: 2, ExtractionQualityTotal: 6},
			{FeedID: "links", ItemCount: 50, ExtractionSample: 50, LinkItemCount: 50},
		}, nil
	}}
	s := &server{store: db}
	feeds, err := s.cachedDetailedFeeds(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 2 || !feeds[0].LinkFeed || feeds[0].RefusedSince != since || feeds[0].Status != "broken" || feeds[0].CadencePin != nil || feeds[0].EffectiveCadenceH != 6 || feeds[0].ExtractionRate == nil || *feeds[0].ExtractionRate != 0.8 || feeds[1].ExtractionRate != nil {
		t.Fatalf("feeds=%+v", feeds)
	}
}

func TestSubscribeCadenceDefaultsAndPins(t *testing.T) {
	for _, cadence := range []string{"", `,"fetch_interval_h":null`, `,"fetch_interval_h":6`, `,"fetch_interval_h":5`} {
		var saved domain.Feed
		db := &fakeAPIStore{putFeed: func(_ context.Context, feed domain.Feed) error { saved = feed; return nil }}
		s := &server{store: db, queue: &apiQueue{}}
		got := s.addFeed(context.Background(), "user", `{"feed_url":"https://example.com/feed"`+cadence+`}`)
		if strings.Contains(cadence, ":5") {
			if got.StatusCode != 400 {
				t.Fatalf("invalid cadence=%+v", got)
			}
			continue
		}
		want := 0
		if strings.Contains(cadence, ":6") {
			want = 6
		}
		if got.StatusCode != 202 || saved.FetchIntervalH != want || saved.HistoryStartedAt == "" {
			t.Fatalf("response=%+v saved=%+v", got, saved)
		}
	}
}
