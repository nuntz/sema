package discovery

import (
	"context"

	"github.com/nuntz/sema/internal/connector/rss"
	"github.com/nuntz/sema/internal/connector/youtube"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/httpx"
)

type Discoverer struct {
	rss     *rss.Discoverer
	youtube *youtube.Discoverer
	enabled bool
}

func New(client *httpx.Client, youtubeEnabled bool) *Discoverer {
	return &Discoverer{rss: rss.NewDiscoverer(client), youtube: youtube.NewDiscoverer(client), enabled: youtubeEnabled}
}

func (d *Discoverer) Discover(ctx context.Context, rawURL string) ([]domain.FeedCandidate, error) {
	if youtube.IsYouTubeInput(rawURL) {
		if !d.enabled {
			return []domain.FeedCandidate{}, nil
		}
		return d.youtube.Discover(ctx, rawURL)
	}
	return d.rss.Discover(ctx, rawURL)
}
