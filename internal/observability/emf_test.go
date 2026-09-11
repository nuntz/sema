package observability

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestExtractedMetricsStayWithinFreeTier(t *testing.T) {
	if len(ExtractedMetrics) > MaxExtractedMetrics {
		t.Fatalf("ExtractedMetrics has %d entries, want at most %d (CloudWatch free tier)", len(ExtractedMetrics), MaxExtractedMetrics)
	}
	for _, name := range []string{"FeedsEnqueued", "SummariesGenerated", "StoryAssignmentFailed"} {
		if !ExtractedMetrics[name] {
			t.Errorf("ExtractedMetrics is missing %s, which an alarm depends on", name)
		}
	}
}

func TestEventExtractsAllowlistedMetricsWithoutDimensions(t *testing.T) {
	event := Event(map[string]float64{"ExtractionFailed": 1, "BodyImageFailed": 2}, map[string]string{"FeedID": "feed-1"})

	if event["ExtractionFailed"] != 1.0 || event["BodyImageFailed"] != 2.0 || event["FeedID"] != "feed-1" {
		t.Fatalf("event fields = %#v, want every metric and dimension kept as a field", event)
	}
	aws, ok := event["_aws"].(map[string]any)
	if !ok {
		t.Fatalf("event has no _aws header: %#v", event)
	}
	directive := aws["CloudWatchMetrics"].([]any)[0].(map[string]any)
	if dims := directive["Dimensions"].([][]string); len(dims) != 1 || len(dims[0]) != 0 {
		t.Errorf("Dimensions = %#v, want a single empty dimension set", dims)
	}
	definitions := directive["Metrics"].([]map[string]string)
	if len(definitions) != 1 || definitions[0]["Name"] != "ExtractionFailed" || definitions[0]["Unit"] != "Count" {
		t.Errorf("Metrics = %#v, want only ExtractionFailed", definitions)
	}
}

func TestEventSkipsHeaderWhenNothingIsExtracted(t *testing.T) {
	event := Event(map[string]float64{"APIRequests": 1, "APIRequestDurationMs": 12}, map[string]string{"Route": "GET /items", "Status": "200"})
	if _, ok := event["_aws"]; ok {
		t.Fatalf("event = %#v, want no _aws header for unlisted metrics", event)
	}
	if event["APIRequests"] != 1.0 || event["Route"] != "GET /items" {
		t.Fatalf("event = %#v, want metrics and dimensions kept as fields", event)
	}
}

func TestEmitWritesOneJSONLine(t *testing.T) {
	var buffer bytes.Buffer
	output = &buffer
	t.Cleanup(func() { output = os.Stdout })

	Emit(map[string]float64{"BedrockLatencyMs": 40}, nil)

	var decoded map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &decoded); err != nil {
		t.Fatalf("decode %q: %v", buffer.String(), err)
	}
	definitions := decoded["_aws"].(map[string]any)["CloudWatchMetrics"].([]any)[0].(map[string]any)["Metrics"].([]any)
	if unit := definitions[0].(map[string]any)["Unit"]; unit != "Milliseconds" {
		t.Errorf("Unit = %v, want Milliseconds", unit)
	}
}
