package main

import (
	"testing"

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
