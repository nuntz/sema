package observability

import (
	"encoding/json"
	"io"
	"os"
	"sort"
	"sync"
	"time"
)

// ExtractedMetrics lists the metric names CloudWatch extracts from log events.
// Every extracted name becomes a billed custom metric, and the free tier covers
// ten of them, so the list is capped at MaxExtractedMetrics. Alarms depend on
// FeedsEnqueued, SummariesGenerated and StoryAssignmentFailed; the remaining
// entries chart pipeline health on the dashboard. Every other metric name, and
// every dimension value, still lands in the log line as a plain field for Logs
// Insights queries.
var ExtractedMetrics = map[string]bool{
	"FeedsEnqueued":         true,
	"SummariesGenerated":    true,
	"StoryAssignmentFailed": true,
	"ItemsWritten":          true,
	"FeedsFailed":           true,
	"ExtractionFailed":      true,
	"MediaFailed":           true,
	"ItemWorkerDurationMs":  true,
	"BedrockLatencyMs":      true,
	"StoryCreated":          true,
}

// MaxExtractedMetrics is the CloudWatch free tier allowance for custom metrics.
const MaxExtractedMetrics = 10

var (
	outputMu sync.Mutex
	output   io.Writer = os.Stdout
)

// Emit writes one structured JSON log event carrying every metric and dimension.
// Metrics named in ExtractedMetrics also get a CloudWatch Embedded Metric Format
// header so they become real metrics; they are always extracted without
// dimensions, because each distinct dimension value would be another billed
// custom metric. Dimensions stay in the event as fields for Logs Insights.
func Emit(metrics map[string]float64, dimensions map[string]string) {
	event := Event(metrics, dimensions)
	outputMu.Lock()
	defer outputMu.Unlock()
	_ = json.NewEncoder(output).Encode(event)
}

// Event builds the log event Emit writes.
func Event(metrics map[string]float64, dimensions map[string]string) map[string]any {
	event := make(map[string]any, len(metrics)+len(dimensions)+1)
	for name, value := range metrics {
		event[name] = value
	}
	for name, value := range dimensions {
		event[name] = value
	}
	extracted := make([]string, 0, len(metrics))
	for name := range metrics {
		if ExtractedMetrics[name] {
			extracted = append(extracted, name)
		}
	}
	if len(extracted) == 0 {
		return event
	}
	sort.Strings(extracted)
	definitions := make([]map[string]string, 0, len(extracted))
	for _, name := range extracted {
		definitions = append(definitions, map[string]string{"Name": name, "Unit": unit(name)})
	}
	event["_aws"] = map[string]any{
		"Timestamp":         time.Now().UnixMilli(),
		"CloudWatchMetrics": []any{map[string]any{"Namespace": "Sema", "Dimensions": [][]string{{}}, "Metrics": definitions}},
	}
	return event
}

func unit(name string) string {
	if len(name) >= 2 && name[len(name)-2:] == "Ms" {
		return "Milliseconds"
	}
	return "Count"
}
