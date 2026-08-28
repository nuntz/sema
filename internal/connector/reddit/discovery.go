package reddit

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"

	"github.com/nuntz/sema/internal/connector"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/httpx"
)

type DiscoveryError struct {
	Kind       string
	StatusCode int
	Input      string
}

func (e *DiscoveryError) Error() string {
	if e.Kind == "unsupported" {
		return "Multireddits, user feeds, post URLs, and bare subreddit names aren't supported."
	}
	switch e.StatusCode {
	case http.StatusNotFound:
		return fmt.Sprintf("No subreddit called %s. Reddit returned 404 for its listing.", e.Input)
	case http.StatusForbidden:
		return fmt.Sprintf("%s is unavailable to Sema. Reddit returned 403 for its public RSS listing, and Sema has no Reddit login—so there is nothing to retry.", e.Input)
	case http.StatusTooManyRequests:
		return "Reddit is rate-limiting us. Try again in a few minutes."
	default:
		return fmt.Sprintf("Reddit returned HTTP %d for %s.", e.StatusCode, e.Input)
	}
}

type Discoverer struct{ client fetcher }

func NewDiscoverer(client *httpx.Client) *Discoverer { return &Discoverer{client: client} }

func (d *Discoverer) Discover(ctx context.Context, raw string) ([]domain.FeedCandidate, error) {
	input, err := ParseInput(raw)
	if err != nil {
		return nil, &DiscoveryError{Kind: "unsupported", Input: strings.TrimSpace(raw)}
	}
	feedURL := CanonicalURL(input.Subreddit, input.Sort)
	headers := make(http.Header)
	headers.Set("User-Agent", UserAgent)
	response, err := d.client.Get(ctx, feedURL, headers)
	if err != nil {
		return nil, fmt.Errorf("fetch Reddit listing: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		kind := "http"
		switch response.StatusCode {
		case http.StatusNotFound:
			kind = "not_found"
		case http.StatusForbidden:
			kind = "unavailable"
		case http.StatusTooManyRequests:
			kind = "rate_limited"
		}
		return nil, &DiscoveryError{Kind: kind, StatusCode: response.StatusCode, Input: Title(input.Subreddit)}
	}
	result, err := connector.ParseFeedResponse(response, domain.Feed{URL: feedURL})
	if err != nil {
		return nil, fmt.Errorf("parse Reddit listing: %w", err)
	}
	newest := ""
	for _, entry := range result.Entries {
		timestamp := domain.Timestamp(entry.Published)
		if timestamp > newest {
			newest = timestamp
		}
	}
	metadata := redditFeedMetadata{}
	_ = xml.Unmarshal(response.Body, &metadata)
	return []domain.FeedCandidate{{
		FeedURL: feedURL, Title: Title(input.Subreddit), Type: "atom", Connector: domain.ConnectorReddit,
		SiteURL: SiteURL(input.Subreddit), BadgeURL: strings.TrimSpace(metadata.Logo), Cadence: SortLabel(input.Sort),
		ItemCount: len(result.Entries), NewestItemTS: newest,
	}}, nil
}

type redditFeedMetadata struct {
	Logo string `xml:"logo"`
}
