package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	awslambda "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func TestSchedulerSilentAlarmMetric(t *testing.T) {
	actions := pulumi.Array{pulumi.String("arn:aws:sns:us-east-1:123456789012:alerts")}
	args := schedulerSilentAlarmArgs(actions)

	assertPulumiString(t, "namespace", args.Namespace, "Sema")
	assertPulumiString(t, "metric name", args.MetricName, "FeedsEnqueued")
	assertPulumiString(t, "statistic", args.Statistic, "Sum")
	assertPulumiString(t, "comparison operator", args.ComparisonOperator, "LessThanThreshold")
	assertPulumiString(t, "missing data", args.TreatMissingData, "breaching")
	assertPulumiInt(t, "period", args.Period, 3600)
	assertPulumiInt(t, "evaluation periods", args.EvaluationPeriods, 4)
	assertPulumiInt(t, "datapoints to alarm", args.DatapointsToAlarm, 4)
	if threshold, ok := args.Threshold.(pulumi.Float64); !ok || float64(threshold) != 1 {
		t.Fatalf("threshold = %#v, want 1", args.Threshold)
	}
	if got, ok := args.AlarmActions.(pulumi.Array); !ok || len(got) != 1 || got[0] != actions[0] {
		t.Fatalf("alarm actions = %#v, want %#v", args.AlarmActions, actions)
	}
}

func TestStoryAssignmentFailedAlarmMetric(t *testing.T) {
	actions := pulumi.Array{pulumi.String("arn:aws:sns:us-east-1:123456789012:alerts")}
	args := storyAssignmentFailedAlarmArgs(actions)

	assertPulumiString(t, "namespace", args.Namespace, "Sema")
	assertPulumiString(t, "metric name", args.MetricName, "StoryAssignmentFailed")
	assertPulumiString(t, "statistic", args.Statistic, "Sum")
	assertPulumiString(t, "comparison operator", args.ComparisonOperator, "GreaterThanThreshold")
	assertPulumiString(t, "missing data", args.TreatMissingData, "notBreaching")
	assertPulumiInt(t, "period", args.Period, 900)
	assertPulumiInt(t, "evaluation periods", args.EvaluationPeriods, 1)
	if threshold, ok := args.Threshold.(pulumi.Float64); !ok || float64(threshold) != 20 {
		t.Fatalf("threshold = %#v, want 20", args.Threshold)
	}
	if args.Dimensions != nil {
		t.Fatalf("dimensions = %#v, want none", args.Dimensions)
	}
	if got, ok := args.AlarmActions.(pulumi.Array); !ok || len(got) != 1 || got[0] != actions[0] {
		t.Fatalf("alarm actions = %#v, want %#v", args.AlarmActions, actions)
	}
}

func TestWebCacheControl(t *testing.T) {
	tests := map[string]string{
		"index.html":           "no-cache",
		"sw.js":                "no-cache",
		"version.json":         "no-cache",
		"manifest.webmanifest": "public,max-age=300,must-revalidate",
		"icon-192.png":         "public,max-age=86400",
		"icon-512.png":         "public,max-age=86400",
		"apple-touch-icon.png": "public,max-age=86400",
		"sema-mark.svg":        "public,max-age=86400",
		"sema-mark-small.svg":  "public,max-age=86400",
		"favicon.ico":          "public,max-age=86400",
		"favicon.svg":          "public,max-age=86400",
		"favicon-32.png":       "public,max-age=86400",
		"favicon-16.png":       "public,max-age=86400",
		"assets/app-abc.js":    "public,max-age=31536000,immutable",
	}
	for path, want := range tests {
		if got := webCacheControl(path); got != want {
			t.Errorf("webCacheControl(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestQueueEventSourceMappingCapsConcurrency(t *testing.T) {
	args := queueEventSourceMappingArgs(pulumi.String("queue"), pulumi.String("function"), 5)
	assertPulumiInt(t, "batch size", args.BatchSize, 5)
	scaling, ok := args.ScalingConfig.(*awslambda.EventSourceMappingScalingConfigArgs)
	if !ok {
		t.Fatalf("scaling config = %#v", args.ScalingConfig)
	}
	assertPulumiInt(t, "maximum concurrency", scaling.MaximumConcurrency, 10)
}

func TestBoundedIntPreservesExplicitZero(t *testing.T) {
	tests := []struct {
		raw     string
		want    int
		wantErr bool
	}{
		{raw: "", want: defaultImageSearchFloor},
		{raw: "0", want: 0},
		{raw: "100", want: 100},
		{raw: "101", wantErr: true},
		{raw: "nope", wantErr: true},
	}
	for _, test := range tests {
		got, err := boundedInt(test.raw, defaultImageSearchFloor, 0, 100)
		if (err != nil) != test.wantErr || (!test.wantErr && got != test.want) {
			t.Errorf("boundedInt(%q) = %d, %v; want %d, error %v", test.raw, got, err, test.want, test.wantErr)
		}
	}
}

func assertPulumiString(t *testing.T, name string, input pulumi.StringPtrInput, want string) {
	t.Helper()
	got, ok := input.(pulumi.String)
	if !ok || string(got) != want {
		t.Fatalf("%s = %#v, want %q", name, input, want)
	}
}

func assertPulumiInt(t *testing.T, name string, input pulumi.IntPtrInput, want int) {
	t.Helper()
	got, ok := input.(pulumi.Int)
	if !ok || int(got) != want {
		t.Fatalf("%s = %#v, want %d", name, input, want)
	}
}

func TestDashboardBodyStaysWithinMetricBudget(t *testing.T) {
	body, err := dashboardBody(dashboardResources{
		stack:  "dev",
		region: "us-east-1",
		functions: dashboardFunctions{
			scheduler: "sema-dev-scheduler", feedWorker: "sema-dev-feed-worker", itemWorker: "sema-dev-item-worker",
			api: "sema-dev-api", rescore: "sema-dev-rescore", vectorCleanup: "sema-dev-vector-cleanup",
		},
		feedsQueue: "feeds-queue", feedsDLQ: "feeds-dlq", itemsQueue: "items-queue", itemsDLQ: "items-dlq",
		table: "sema-dev", apiID: "abc123", distributionID: "E123456789",
		alarmArns: []string{"arn:1", "arn:2", "arn:3", "arn:4", "arn:5", "arn:6", "arn:7"},
	})
	if err != nil {
		t.Fatalf("dashboardBody: %v", err)
	}

	var dashboard struct {
		Widgets []struct {
			Type       string `json:"type"`
			Properties struct {
				Title   string  `json:"title"`
				Metrics [][]any `json:"metrics"`
				Query   string  `json:"query"`
			} `json:"properties"`
		} `json:"widgets"`
	}
	if err := json.Unmarshal([]byte(body), &dashboard); err != nil {
		t.Fatalf("decode dashboard body: %v", err)
	}
	if len(dashboard.Widgets) == 0 {
		t.Fatal("dashboard has no widgets")
	}

	// Mirrors observability.ExtractedMetrics: the only Sema metric names that exist
	// as custom metrics. Anything else on the dashboard would chart an empty series.
	extracted := map[string]bool{
		"FeedsEnqueued": true, "SummariesGenerated": true, "StoryAssignmentFailed": true, "ItemsWritten": true, "FeedsFailed": true,
		"ExtractionFailed": true, "MediaFailed": true, "ItemWorkerDurationMs": true, "BedrockLatencyMs": true, "StoryCreated": true,
	}
	metrics := 0
	metricsByTitle := map[string][][]any{}
	logWidgets := 0
	for index, widget := range dashboard.Widgets {
		title := strings.TrimSpace(widget.Properties.Title)
		if title == "" {
			t.Errorf("widget %d has no title", index)
		}
		metrics += len(widget.Properties.Metrics)
		metricsByTitle[title] = widget.Properties.Metrics
		for _, metric := range widget.Properties.Metrics {
			if len(metric) >= 2 && metric[0] == "Sema" && !extracted[metric[1].(string)] {
				t.Errorf("widget %q charts Sema/%v, which is not an extracted metric", title, metric[1])
			}
			if expression, ok := metric[0].(map[string]any); ok && strings.Contains(fmt.Sprint(expression["expression"]), "{Sema") {
				t.Errorf("widget %q searches Sema metrics: %v; dimensioned Sema metrics are no longer extracted", title, expression["expression"])
			}
		}
		if widget.Type == "log" {
			logWidgets++
			if !strings.HasPrefix(widget.Properties.Query, "SOURCE '/aws/lambda/sema-dev-item-worker' | ") || !strings.Contains(widget.Properties.Query, "by FeedID") {
				t.Errorf("log widget %q query = %q, want item worker log group grouped by FeedID", title, widget.Properties.Query)
			}
		}
	}
	if metrics >= 50 {
		t.Errorf("dashboard charts %d metrics, want fewer than 50", metrics)
	}
	if metrics != 40 {
		t.Errorf("dashboard charts %d metrics, want 40", metrics)
	}
	if logWidgets != 1 {
		t.Errorf("dashboard has %d log widgets, want 1 for failures by feed", logWidgets)
	}
	storyMetrics := metricsByTitle["Story assignment"]
	if len(storyMetrics) != 2 || storyMetrics[0][1] != "StoryCreated" || storyMetrics[1][1] != "StoryAssignmentFailed" {
		t.Fatalf("Story assignment metrics = %#v, want StoryCreated and StoryAssignmentFailed", storyMetrics)
	}
	foundTransactionConflict := false
	for _, metric := range metricsByTitle["DynamoDB errors"] {
		if len(metric) >= 4 && metric[0] == "AWS/DynamoDB" && metric[1] == "TransactionConflict" && metric[2] == "TableName" && metric[3] == "sema-dev" {
			foundTransactionConflict = true
			break
		}
	}
	if !foundTransactionConflict {
		t.Fatalf("DynamoDB errors metrics = %#v, want TransactionConflict", metricsByTitle["DynamoDB errors"])
	}
	t.Logf("dashboard charts %d metrics across %d widgets", metrics, len(dashboard.Widgets))
}

func TestOnlyItemsQueueHasBatchingWindow(t *testing.T) {
	items := itemQueueEventSourceMappingArgs(pulumi.String("items"), pulumi.String("worker"))
	assertPulumiInt(t, "items batch size", items.BatchSize, 5)
	assertPulumiInt(t, "items batching window", items.MaximumBatchingWindowInSeconds, 15)
	feeds := queueEventSourceMappingArgs(pulumi.String("feeds"), pulumi.String("worker"), 10)
	if feeds.MaximumBatchingWindowInSeconds != nil {
		t.Fatalf("feeds batching window = %#v, want unset", feeds.MaximumBatchingWindowInSeconds)
	}
	if itemQueueVisibilitySeconds <= itemWorkerTimeoutSeconds+itemBatchWindowSeconds {
		t.Fatal("items visibility must exceed Lambda timeout plus batching window")
	}
	responses, ok := items.FunctionResponseTypes.(pulumi.StringArray)
	if !ok || len(responses) != 1 || responses[0] != pulumi.String("ReportBatchItemFailures") {
		t.Fatalf("partial failure responses = %#v", items.FunctionResponseTypes)
	}
}
