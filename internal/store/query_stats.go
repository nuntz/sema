package store

import "context"

// QueryStats counts the DynamoDB work behind one list request so the API can
// report page cost alongside latency.
type QueryStats struct {
	Queries int
	Scanned int
}

type queryStatsKey struct{}

// WithQueryStats returns a context whose item list calls accumulate into the
// returned stats.
func WithQueryStats(ctx context.Context) (context.Context, *QueryStats) {
	stats := &QueryStats{}
	return context.WithValue(ctx, queryStatsKey{}, stats), stats
}

func recordQuery(ctx context.Context, scanned int32) {
	if stats, ok := ctx.Value(queryStatsKey{}).(*QueryStats); ok {
		stats.Queries++
		stats.Scanned += int(scanned)
	}
}
