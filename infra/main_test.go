package main

import (
	"encoding/json"
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
			Properties struct {
				Title   string  `json:"title"`
				Metrics [][]any `json:"metrics"`
			} `json:"properties"`
		} `json:"widgets"`
	}
	if err := json.Unmarshal([]byte(body), &dashboard); err != nil {
		t.Fatalf("decode dashboard body: %v", err)
	}
	if len(dashboard.Widgets) == 0 {
		t.Fatal("dashboard has no widgets")
	}

	metrics := 0
	metricsByTitle := map[string][][]any{}
	for index, widget := range dashboard.Widgets {
		title := strings.TrimSpace(widget.Properties.Title)
		if title == "" {
			t.Errorf("widget %d has no title", index)
		}
		metrics += len(widget.Properties.Metrics)
		metricsByTitle[title] = widget.Properties.Metrics
	}
	if metrics >= 50 {
		t.Errorf("dashboard charts %d metrics, want fewer than 50", metrics)
	}
	if metrics != 49 {
		t.Errorf("dashboard charts %d metrics, want 49", metrics)
	}
	storyMetrics := metricsByTitle["Story assignment"]
	if len(storyMetrics) != 1 {
		t.Fatalf("Story assignment metrics = %#v, want one SEARCH expression", storyMetrics)
	}
	storyExpression, ok := storyMetrics[0][0].(map[string]any)
	if !ok || storyExpression["expression"] != `SEARCH('{Sema} ("StoryCreated" OR "StoryJoined" OR "StoryAssignmentFailed")', 'Sum', 300)` {
		t.Fatalf("Story assignment metric = %#v", storyMetrics[0])
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
