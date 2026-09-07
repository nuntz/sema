package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaintenanceCommandsUseRequestedStack(t *testing.T) {
	for _, target := range []string{"replay", "redrive", "backfill-youtube-connector"} {
		t.Run(target, func(t *testing.T) {
			dir := t.TempDir()
			// The active stack differs from STACK. Stub every external command
			// so this exercises the real Make recipes without touching AWS.
			stubs := map[string]string{
				"pulumi": `#!/bin/sh
stack=dev
if [ "$4" = "--stack" ]; then stack="$5"; fi
printf '%s-%s\n' "$stack" "$3"
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
			command := exec.Command("make", target, "STACK=prod", "MODEL_VERSION=")
			command.Dir = "../.."
			command.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "REVIEW_COMMAND_LOG="+logPath)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("make %s: %v\n%s", target, err, output)
			}
			logged, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string][]string{
				"replay":                     {"table=sema-prod", "queue=prod-itemsQueueUrl", "model=prod-modelVersion"},
				"redrive":                    {"--source-arn prod-feedsDlqArn", "--destination-arn prod-feedsQueueArn", "--source-arn prod-itemsDlqArn", "--destination-arn prod-itemsQueueArn"},
				"backfill-youtube-connector": {"table=sema-prod", "bucket=prod-contentBucket"},
			}
			for _, value := range want[target] {
				if !strings.Contains(string(logged), value) {
					t.Errorf("missing %q in command: %s", value, logged)
				}
			}
		})
	}
}
