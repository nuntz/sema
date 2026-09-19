package domain

import (
	"net/url"
	"strings"
)

func IsBlueskyFeed(feed Feed) bool {
	u, err := url.Parse(feed.URL)
	return err == nil && strings.EqualFold(u.Hostname(), "bsky.app")
}

func UsesBodyHistory(feed Feed) bool {
	return FeedConnector(feed) == ConnectorRSS && !IsBlueskyFeed(feed)
}

func IsLinkFeed(feed Feed) bool {
	return UsesBodyHistory(feed) && len(feed.BodyOutcomes) >= 50 && strings.Count(feed.BodyOutcomes[len(feed.BodyOutcomes)-50:], "0") >= 45
}

func AppendBodyOutcome(history string, hasBody bool) string {
	outcome := "0"
	if hasBody {
		outcome = "1"
	}
	history += outcome
	if len(history) > 50 {
		history = history[len(history)-50:]
	}
	return history
}
