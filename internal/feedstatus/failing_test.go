package feedstatus

import (
	"reflect"
	"testing"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

func TestFailingConnectors(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	refused := func(age time.Duration) string { return domain.Timestamp(now.Add(-age)) }
	cases := []struct {
		name  string
		feeds []domain.Feed
		want  []string
	}{
		{"no feeds", nil, []string{}},
		{"every feed failing twice in a row", []domain.Feed{
			{Connector: domain.ConnectorYouTube, ErrorCount: 2},
			{Connector: domain.ConnectorYouTube, ErrorCount: 3},
		}, []string{domain.ConnectorYouTube}},
		{"one feed healthy", []domain.Feed{
			{Connector: domain.ConnectorYouTube, ErrorCount: 2},
			{Connector: domain.ConnectorYouTube},
		}, []string{}},
		{"a single failed fetch", []domain.Feed{{Connector: domain.ConnectorRSS, ErrorCount: 1}}, []string{}},
		{"muted feeds are ignored", []domain.Feed{
			{Connector: domain.ConnectorRSS, ErrorCount: 4},
			{Connector: domain.ConnectorRSS, Muted: true},
		}, []string{domain.ConnectorRSS}},
		{"only muted feeds", []domain.Feed{
			{Connector: domain.ConnectorRSS, ErrorCount: 4, Muted: true},
		}, []string{}},
		{"refused for seven hours", []domain.Feed{{Connector: domain.ConnectorYouTube, RefusedSince: refused(7 * time.Hour)}}, []string{domain.ConnectorYouTube}},
		{"refused for one hour", []domain.Feed{{Connector: domain.ConnectorYouTube, RefusedSince: refused(time.Hour)}}, []string{}},
		{"unparseable refusal time", []domain.Feed{{Connector: domain.ConnectorYouTube, RefusedSince: "garbage"}}, []string{}},
		{"refused and erroring feeds together", []domain.Feed{
			{Connector: domain.ConnectorYouTube, RefusedSince: refused(7 * time.Hour)},
			{Connector: domain.ConnectorYouTube, ErrorCount: 2},
		}, []string{domain.ConnectorYouTube}},
		{"only the dead connector is reported", []domain.Feed{
			{Connector: domain.ConnectorRSS, ErrorCount: 5},
			{Connector: domain.ConnectorRSS},
			{Connector: domain.ConnectorYouTube, ErrorCount: 2},
			{Connector: domain.ConnectorYouTube, RefusedSince: refused(8 * time.Hour)},
		}, []string{domain.ConnectorYouTube}},
		{"feeds without a connector are rss", []domain.Feed{
			{ErrorCount: 2},
			{Connector: domain.ConnectorRSS, ErrorCount: 3},
		}, []string{domain.ConnectorRSS}},
		{"several dead connectors sort by name", []domain.Feed{
			{Connector: domain.ConnectorYouTube, ErrorCount: 2},
			{Connector: domain.ConnectorRSS, ErrorCount: 2},
		}, []string{domain.ConnectorRSS, domain.ConnectorYouTube}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := FailingConnectors(test.feeds, now); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("FailingConnectors = %#v, want %#v", got, test.want)
			}
		})
	}
}

// A Connector dead for days has backed every Feed off to one fetch a day, so
// nothing has been fetched for hours; its stored state alone keeps it failing.
func TestFailingConnectorsStaysFailingAfterDailyBackoff(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	feed := domain.Feed{Connector: domain.ConnectorRSS}
	start := now.Add(-4 * 24 * time.Hour)
	for fetchedAt := start; ; {
		feed, _ = AfterFetch(feed, domain.FetchResult{}, errorString("connection refused"), fetchedAt)
		next, err := time.Parse(domain.TimeLayout, feed.NextFetchAt)
		if err != nil {
			t.Fatal(err)
		}
		if next.After(now) {
			break
		}
		fetchedAt = next
	}
	last, _ := time.Parse(domain.TimeLayout, feed.LastFetchAt)
	if feed.ErrorCount < 5 || now.Sub(last) < 3*time.Hour {
		t.Fatalf("feed = error count %d, last fetch %s; want a feed backed off to daily fetches", feed.ErrorCount, feed.LastFetchAt)
	}
	if got := FailingConnectors([]domain.Feed{feed, feed}, now); !reflect.DeepEqual(got, []string{domain.ConnectorRSS}) {
		t.Fatalf("FailingConnectors = %#v, want rss still failing", got)
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }
