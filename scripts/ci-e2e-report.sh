#!/usr/bin/env bash
# Reads Playwright's JSON reports (web/playwright.config.ts) and prints, per report, the pass
# counts, each failing test with its error, and the slowest tests by share of their timeout, the
# share web/e2e/test.ts fails past half (#65). CI uploads one report per e2e leg and run attempt.
# Under each failing or flaky test it prints where its trace.zip is: a run's results artifact to
# download, or a local path. The trace's DOM snapshots show the page before and after each step,
# so a CI-only failure shows what moved, such as a height that changed after the test read it (#83).
# Usage: scripts/ci-e2e-report.sh <run-id|report.json> [slowest count, default 5]
#   A run ID downloads that CI run's reports with gh; a path reads a local report, such as
#   /tmp/sema-playwright-report.json after `make e2e`.
# Exit codes: 0 when every report has no failures, 1 when any has one, 2 for a usage error or a
# run with no reports (one from before the reports were uploaded, or still in progress).
set -euo pipefail

cd "$(dirname "$0")/.."
if [[ $# -lt 1 || $# -gt 2 ]]; then
  sed -n '2,12p' "$0" >&2
  exit 2
fi
source="$1"
slowest="${2:-5}"

dir="$(mktemp -d)"
trap 'rm -rf "$dir"' EXIT
if [[ -f "$source" ]]; then
  mkdir "$dir/local"
  cp "$source" "$dir/local/report.json"
else
  if ! gh run download "$source" --pattern 'playwright-report*' --dir "$dir" >/dev/null; then
    echo "ci-e2e-report: no Playwright reports in run $source" >&2
    exit 2
  fi
fi

shopt -s nullglob
reports=("$dir"/*/*.json)
if [[ ${#reports[@]} -eq 0 ]]; then
  echo "ci-e2e-report: no Playwright reports in run $source" >&2
  exit 2
fi

status=0
for report in "${reports[@]}"; do
  name="$(basename "$(dirname "$report")")"
  echo "== $name"
  # A run's reports sit beside results artifacts of the same leg and attempt, which hold the traces.
  run=""
  if [[ ! -f "$source" ]]; then run="$source"; fi
  results="${name/playwright-report/playwright-results}"
  # Specs nest in a file's suite and its describe blocks; each spec has one test per project, and
  # its last result is the one that counted.
  jq -r --argjson slowest "$slowest" --arg run "$run" --arg results "$results" '
    def specs(path): (.specs[]? | . as $spec | .tests[] | {
        title: (path + [$spec.title] | join(" › ")),
        where: "\($spec.file):\($spec.line)",
        status,
        timeout,
        result: .results[-1]
      }),
      (.suites[]? | specs(path + [.title]));
    [.suites[] | specs([])] as $tests
    | .stats as $stats
    | "\($stats.expected) passed, \($stats.unexpected) failed, \($stats.flaky) flaky, \($stats.skipped) skipped",
      ($tests[] | select(.status == "unexpected" or .status == "flaky")
        | "\n\(.status | ascii_upcase) \(.where) › \(.title)",
          (.result.errors[]?.message // "" | split("\n")[:12][] | "    " + .),
          (.result.attachments[]? | select(.name == "trace") | .path
            | if $run == "" then "    trace: \(.)"
              else "    trace: \(sub("^/tmp/sema-playwright-results[^/]*/"; "")) in gh run download \($run) -n \($results)"
              end)),
      "\nslowest:",
      ($tests | map(select(.result.duration != null and .timeout > 0 and .status != "skipped"))
        | sort_by(-(.result.duration / .timeout))[:$slowest][]
        | "  \(100 * .result.duration / .timeout | . * 10 | round / 10)%  \(.result.duration / 100 | round / 10)s of \(.timeout / 1000)s  \(.where) › \(.title)")
  ' "$report" | sed 's/\x1b\[[0-9;]*[A-Za-z]//g'
  if [[ "$(jq '.stats.unexpected' "$report")" != 0 ]]; then status=1; fi
  echo
done
exit "$status"
