package main

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestPublishedWindowParsing(t *testing.T) {
	query := map[string]string{"published_from": "2026-09-01T00:00:00Z", "published_before": "2026-09-03T00:00:00Z", "tonight_before": "2026-09-08T00:00:00-07:00", "unkept": "true", "ascending": "true"}
	filter, err := parseItemFilter(query)
	if err != nil || !filter.Unkept || !filter.Ascending || filter.TonightBefore.UTC().Hour() != 7 || filter.Published.Before.Sub(filter.Published.From) != 48*time.Hour {
		t.Fatalf("%+v %v", filter, err)
	}
}
func TestPublishedWindowInvalidParameters(t *testing.T) {
	for _, query := range []map[string]string{{"published_from": "2026-09-01T00:00:00Z"}, {"published_before": "2026-09-01T00:00:00Z"}, {"published_from": "invalid", "published_before": "later"}, {"published_from": "2026-09-02T00:00:00Z", "published_before": "2026-09-01T00:00:00Z"}, {"published_from": "2026-09-01T00:00:00Z", "published_before": "2026-09-01T00:00:00Z"}, {"tonight_before": "tonight"}, {"unkept": "yes"}, {"ascending": "yes"}} {
		s := &server{}
		if got := s.getItems(context.Background(), "user", query); got.StatusCode != http.StatusBadRequest {
			t.Fatalf("items: %+v", got)
		}
		if got := s.getFeedItemCounts(context.Background(), "user", query); got.StatusCode != http.StatusBadRequest {
			t.Fatalf("counts: %+v", got)
		}
	}
}
