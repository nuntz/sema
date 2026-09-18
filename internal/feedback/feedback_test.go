package feedback

import (
	"encoding/json"
	"os"
	"testing"
)

func TestFeedbackRules(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/feedback.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name      string
		Kept      bool
		Current   int
		Action    Action
		Requested int
		Value     int
		NextKept  bool
		Source    string
		Preserve  bool
		Rejected  bool
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := Apply(State{Kept: tc.Kept, Value: tc.Current}, tc.Action, tc.Requested)
			if (err != nil) != tc.Rejected {
				t.Fatalf("error = %v", err)
			}
			if err == nil && got != (Plan{Kept: tc.NextKept, Value: tc.Value, Source: tc.Source, Preserve: tc.Preserve}) {
				t.Fatalf("plan = %+v", got)
			}
		})
	}
}
