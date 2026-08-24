package discovery

import (
	"context"
	"testing"
)

func TestYouTubeDiscoveryRolloutGateReturnsNoLegacyRSSCandidate(t *testing.T) {
	candidates, err := (&Discoverer{enabled: false}).Discover(context.Background(), "https://youtube.com/@sema")
	if err != nil || len(candidates) != 0 {
		t.Fatalf("candidates = %#v, err = %v", candidates, err)
	}
}
