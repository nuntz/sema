package discovery

import (
	"context"
	"testing"

	"github.com/nuntz/sema/internal/domain"
)

type stubDiscoverer struct{ candidate domain.FeedCandidate }

func (s stubDiscoverer) Discover(context.Context, string) ([]domain.FeedCandidate, error) {
	return []domain.FeedCandidate{s.candidate}, nil
}

func TestYouTubeDiscoveryRolloutGateReturnsNoLegacyRSSCandidate(t *testing.T) {
	candidates, err := (&Discoverer{routes: []route{{matches: func(string) bool { return true }, enabled: false}}}).Discover(context.Background(), "https://youtube.com/@sema")
	if err != nil || len(candidates) != 0 {
		t.Fatalf("candidates = %#v, err = %v", candidates, err)
	}
}

func TestOrderedRoutesRunBeforeRSSFallback(t *testing.T) {
	discoverer := &Discoverer{
		rss: stubDiscoverer{candidate: domain.FeedCandidate{Connector: domain.ConnectorRSS}},
		routes: []route{{
			matches: func(raw string) bool { return raw == "r/example" }, enabled: true,
			discover: stubDiscoverer{candidate: domain.FeedCandidate{Connector: domain.ConnectorReddit}},
		}},
	}
	for _, test := range []struct{ input, connector string }{{"r/example", domain.ConnectorReddit}, {"example.com", domain.ConnectorRSS}} {
		candidates, err := discoverer.Discover(context.Background(), test.input)
		if err != nil || len(candidates) != 1 || candidates[0].Connector != test.connector {
			t.Fatalf("Discover(%q) = %#v, %v", test.input, candidates, err)
		}
	}
}
