package rss

import (
	"context"
	"net/http"

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
	return connector.ParseFeedResponse(response, feed)
}
