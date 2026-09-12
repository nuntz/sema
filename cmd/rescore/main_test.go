package main

import (
	"context"
	"errors"
	rankingrescore "github.com/nuntz/sema/internal/rescore"
	"testing"
)

type fakeUsers struct{}

func (fakeUsers) UserIDs(context.Context) ([]string, error) { return []string{"bad", "good"}, nil }

type fakeEngine struct {
	calls   []string
	failure error
}

func (f *fakeEngine) RunUser(_ context.Context, user string, _ bool) (rankingrescore.Result, error) {
	f.calls = append(f.calls, user)
	if user == "bad" {
		return rankingrescore.Result{}, f.failure
	}
	return rankingrescore.Result{ItemsRescored: 2}, nil
}
func TestRunContinuesAfterUserFailure(t *testing.T) {
	failure := errors.New("failed")
	for _, test := range []struct {
		name, user                    string
		failure                       error
		users, failed, skipped, calls int
		wantErr                       bool
	}{
		{"scheduled", "", failure, 1, 1, 0, 2, false},
		{"on demand", "bad", failure, 0, 1, 0, 1, true},
		{"replay", "", rankingrescore.ErrReplayActive, 1, 0, 1, 2, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := &fakeEngine{failure: test.failure}
			output, err := (&handler{store: fakeUsers{}, engine: engine}).run(context.Background(), request{User: test.user})
			if (err != nil) != test.wantErr || output.Users != test.users || output.Failed != test.failed || output.Skipped != test.skipped || len(engine.calls) != test.calls {
				t.Fatalf("output = %#v, calls = %v, error = %v", output, engine.calls, err)
			}
		})
	}
}
