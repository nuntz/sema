package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMaintenanceCommandsUseRequestedStack(t *testing.T) {
	cases := []struct {
		target string
		args   []string
		want   []string
	}{
		{"replay", []string{"STACK=prod"}, []string{"table=sema-prod", "queue=untz/sema/prod-itemsQueueUrl", "model=untz/sema/prod-modelVersion"}},
		{"redrive", []string{"STACK=prod"}, []string{"--source-arn untz/sema/prod-feedsDlqArn", "--destination-arn untz/sema/prod-feedsQueueArn", "--source-arn untz/sema/prod-itemsDlqArn", "--destination-arn untz/sema/prod-itemsQueueArn"}},
		{"backfill-youtube-connector", []string{"STACK=prod"}, []string{"table=sema-prod", "bucket=untz/sema/prod-contentBucket"}},
		// prod is the default stack, in the untz Pulumi Cloud organization.
		{"replay", nil, []string{"table=sema-prod", "queue=untz/sema/prod-itemsQueueUrl"}},
		{"rescore", nil, []string{"--function-name sema-prod-rescore"}},
		{"replay", []string{"PULUMI_ORG=other"}, []string{"table=sema-prod", "queue=other/sema/prod-itemsQueueUrl"}},
		// make deploy updates the Cloud stack, not whichever stack is selected.
		{"deploy", nil, []string{"pulumi up --stack untz/sema/prod", "--distribution-id untz/sema/prod-distributionId"}},
	}
	for _, tc := range cases {
		t.Run(strings.Join(append([]string{tc.target}, tc.args...), " "), func(t *testing.T) {
			dir := t.TempDir()
			// The active stack differs from the requested one. Stub every external
			// command so this exercises the real Make recipes without touching AWS.
			stubs := map[string]string{
				"pulumi": `#!/bin/sh
printf 'pulumi %s\n' "$*" >> "$REVIEW_COMMAND_LOG"
stack=selected
previous=
for arg in "$@"; do
	if [ "$previous" = "--stack" ]; then stack="$arg"; fi
	previous="$arg"
done
if [ "$1 $2" = "stack output" ]; then printf '%s-%s\n' "$stack" "$3"; fi
`,
				"go": `#!/bin/sh
printf 'table=%s queue=%s model=%s bucket=%s\n' "$TABLE_NAME" "$ITEMS_QUEUE_URL" "$MODEL_VERSION" "$CONTENT_BUCKET" >> "$REVIEW_COMMAND_LOG"
`,
				"aws": `#!/bin/sh
printf '%s\n' "$*" >> "$REVIEW_COMMAND_LOG"
`,
			}
			for name, script := range stubs {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			logPath := filepath.Join(dir, "commands")
			command := exec.Command("make", append([]string{tc.target, "MODEL_VERSION="}, tc.args...)...)
			command.Dir = "../.."
			command.Env = append(withoutEnv(os.Environ(), "STACK", "PULUMI_ORG", "PULUMI_STACK"), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "REVIEW_COMMAND_LOG="+logPath)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("make %s: %v\n%s", tc.target, err, output)
			}
			logged, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range tc.want {
				if !strings.Contains(string(logged), value) {
					t.Errorf("missing %q in commands:\n%s", value, logged)
				}
			}
		})
	}
}

// withoutEnv drops the named variables, so a developer's shell can't pick the stack.
func withoutEnv(environ []string, names ...string) []string {
	var kept []string
	for _, variable := range environ {
		name, _, _ := strings.Cut(variable, "=")
		if !slices.Contains(names, name) {
			kept = append(kept, variable)
		}
	}
	return kept
}
