package discovery

import (
	"context"

	"github.com/nuntz/sema/internal/connector/reddit"
	"github.com/nuntz/sema/internal/connector/rss"
	"github.com/nuntz/sema/internal/connector/youtube"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/httpx"
)

type candidateDiscoverer interface {
	Discover(context.Context, string) ([]domain.FeedCandidate, error)
}

type route struct {
	matches  func(string) bool
	discover candidateDiscoverer
	enabled  bool
}

type Discoverer struct {
	rss    candidateDiscoverer
	routes []route
}

func New(client *httpx.Client, youtubeEnabled bool) *Discoverer {
	return &Discoverer{
		rss: rss.NewDiscoverer(client),
		routes: []route{
			{matches: reddit.Matches, discover: reddit.NewDiscoverer(client), enabled: true},
			{matches: youtube.IsYouTubeInput, discover: youtube.NewDiscoverer(client), enabled: youtubeEnabled},
		},
	}
}

func (d *Discoverer) Discover(ctx context.Context, rawURL string) ([]domain.FeedCandidate, error) {
	for _, candidate := range d.routes {
		if !candidate.matches(rawURL) {
			continue
		}
		if !candidate.enabled {
			return []domain.FeedCandidate{}, nil
		}
		return candidate.discover.Discover(ctx, rawURL)
	}
	return d.rss.Discover(ctx, rawURL)
}
