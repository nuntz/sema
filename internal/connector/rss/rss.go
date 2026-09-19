package rss

import (
	"context"
	"golang.org/x/net/html"
	"net/http"
	"net/url"
	"strings"

	"github.com/nuntz/sema/internal/connector"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/httpx"
)

type Connector struct {
	client fetcher
}

type fetcher interface {
	Get(context.Context, string, http.Header) (httpx.Response, error)
}

func New(client *httpx.Client) *Connector {
	return &Connector{client: client}
}

func (c *Connector) Fetch(ctx context.Context, feed domain.Feed) (domain.FetchResult, error) {
	response, err := c.client.Get(ctx, feed.URL, connector.ConditionalHeaders(feed))
	if err != nil {
		return domain.FetchResult{}, err
	}
	result, err := connector.ParseFeedResponse(response, feed)
	if err == nil && domain.IsBlueskyFeed(feed) {
		for i := range result.Entries {
			entry := &result.Entries[i]
			entry.LinkItem = !hasExternalLink(*entry)
		}
	}
	return result, err
}

func hasExternalLink(entry domain.Entry) bool {
	external := func(raw string) bool {
		u, err := url.Parse(raw)
		return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && !strings.EqualFold(u.Hostname(), "bsky.app")
	}
	if external(entry.URL) {
		return true
	}
	for _, raw := range []string{entry.ContentRaw, entry.SummaryRaw} {
		tokenizer := html.NewTokenizer(strings.NewReader(raw))
		for {
			token := tokenizer.Next()
			if token == html.ErrorToken {
				break
			}
			if token != html.StartTagToken {
				continue
			}
			tag := tokenizer.Token()
			if tag.Data != "a" {
				continue
			}
			for _, attr := range tag.Attr {
				if attr.Key == "href" && external(attr.Val) {
					return true
				}
			}
		}
	}
	return false
}
