package main

import "context"

type requestFieldsKey struct{}

// withRequestFields gives handlers somewhere to describe the request they
// served; handle emits those fields on the one per-request event, as Logs
// Insights fields rather than metrics.
func withRequestFields(ctx context.Context) (context.Context, map[string]string) {
	fields := map[string]string{}
	return context.WithValue(ctx, requestFieldsKey{}, fields), fields
}

// annotateRequest adds a field to the current request's event. Outside a
// request served by handle it does nothing. The fields map is unsynchronised,
// so call it only from the request's own goroutine. Field names must not
// collide with the request metrics (APIRequests, APIRequestDurationMs), and
// the saved queries in infra/main.go depend on them, so rename them together.
func annotateRequest(ctx context.Context, name, value string) {
	if fields, ok := ctx.Value(requestFieldsKey{}).(map[string]string); ok && value != "" {
		fields[name] = value
	}
}
