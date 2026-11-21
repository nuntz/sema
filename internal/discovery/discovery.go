package discovery

import (
	"context"
	"net/http"

	"github.com/nuntz/sema/internal/connector/reddit"
	"github.com/nuntz/sema/internal/connector/rss"
	"github.com/nuntz/sema/internal/connector/youtube"
	"github.com/nuntz/sema/internal/domain"
	"github.com/nuntz/sema/internal/httpx"
)

// Fetcher is the HTTP client discovery reads candidate pages and feeds with.
type Fetcher interface {
	Get(context.Context, string, http.Header) (httpx.Response, error)
}

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

func New(client Fetcher, youtubeEnabled bool) *Discoverer {
	return &Discoverer{
		rss: rss.NewDiscoverer(client),
		routes: []route{
			// Reddit ended its public feeds (ADR 0009): refuse its addresses
			// without a request, and never fall through to RSS discovery.
			{matches: reddit.Matches, discover: refuseReddit{}, enabled: true},
			{matches: youtube.IsYouTubeInput, discover: youtube.NewDiscoverer(client), enabled: youtubeEnabled},
		},
	}
}

type refuseReddit struct{}

func (refuseReddit) Discover(context.Context, string) ([]domain.FeedCandidate, error) {
	return nil, reddit.ErrEnded
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
