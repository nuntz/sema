package feedstatus

import (
	"sort"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

const (
	// failingErrorCount is how many fetches in a row must fail before a Feed
	// counts toward a failing Connector; one failure is ordinary flakiness.
	failingErrorCount = 2
	// failingRefusal is how long a Feed must be Refused before it counts.
	failingRefusal = 6 * time.Hour
)

// FailingConnectors returns, sorted, the Connectors that have at least one
// non-muted Feed and whose non-muted Feeds are all failing: each has failed
// its last two or more fetches, or has been Refused for at least six hours.
// It reads only each Feed's stored state, so a Connector whose Feeds have
// backed off to a daily fetch stays failing between fetches.
func FailingConnectors(feeds []domain.Feed, now time.Time) []string {
	working := map[string]bool{}
	for _, feed := range feeds {
		if feed.Muted {
			continue
		}
		connector := domain.FeedConnector(feed)
		working[connector] = working[connector] || !failing(feed, now)
	}
	result := []string{}
	for connector, ok := range working {
		if !ok {
			result = append(result, connector)
		}
	}
	sort.Strings(result)
	return result
}

func failing(feed domain.Feed, now time.Time) bool {
	if feed.ErrorCount >= failingErrorCount {
		return true
	}
	since, err := time.Parse(time.RFC3339Nano, feed.RefusedSince)
	return err == nil && now.Sub(since) >= failingRefusal
}
