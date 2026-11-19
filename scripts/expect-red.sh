#!/usr/bin/env bash
# Injected-bug evidence (CODING_STANDARDS.md → Flaky tests): applies a patch that plants a bug,
# runs only the named Playwright tests, and succeeds only if at least one ran and failed. The patch
# is always reverted, even on Ctrl-C. Paste its output into the PR.
# Usage: scripts/expect-red.sh <patch|-> <spec> [playwright args, such as -g "title"]
#   <patch> is a `git diff` from the repo root ("-" reads it from stdin); <spec> is relative to
#   web/ or the repo root. Make one by editing the app, `git diff -- web/src > bug.patch`, then reverting.
# Exit codes: 0 red as expected, 1 still green, 2 for a usage error, a patch that doesn't apply,
# no test run, or an interrupt.
set -euo pipefail

cd "$(dirname "$0")/.."
if [[ $# -lt 2 ]]; then
  sed -n '2,9p' "$0" >&2
  exit 2
fi
patch_arg="$1"
spec="${2#web/}"
shift 2

patch="$(mktemp)"
output="$(mktemp)"
applied=0
cleanup() {
  if [[ "$applied" == 1 ]]; then git apply -R "$patch"; fi
  rm -f "$patch" "$output"
}
trap cleanup EXIT
if [[ "$patch_arg" == "-" ]]; then cat >"$patch"; else cp "$patch_arg" "$patch"; fi

if ! git apply --check "$patch"; then
  echo "expect-red: the patch doesn't apply to the working tree" >&2
  exit 2
fi
git apply "$patch"
applied=1
echo "expect-red: planted $(git apply --numstat "$patch" | awk '{print $3}' | paste -sd ' ')"

status=0
(cd web && bunx playwright test "$spec" --retries=0 --reporter=line "$@") >"$output" 2>&1 ||
  status=$?
# The line reporter redraws progress with ANSI escapes; strip them so the summary greps cleanly.
summary="$(sed 's/\x1b\[[0-9;]*[A-Za-z]//g' "$output")"

if grep -qE '^ *[0-9]+ interrupted' <<<"$summary"; then
  echo "expect-red: interrupted; the patch is reverted" >&2
  exit 2
fi
if grep -q "No tests found" <<<"$summary" || ! grep -qE '^ *[0-9]+ (passed|failed)' <<<"$summary"; then
  echo "$summary" | tail -20 >&2
  echo "expect-red: no test ran; check the spec path and -g title" >&2
  exit 2
fi
if [[ "$status" == 0 ]]; then
  grep -E '^ *[0-9]+ passed' <<<"$summary" >&2
  echo "expect-red: STILL GREEN: these tests don't catch the planted bug" >&2
  exit 1
fi
# Show where each test failed, so a reader can check it failed on the planted bug, not on a crash.
grep -E '^ +[0-9]+\) |Error:|Expected|Received|^ +> [0-9]+ \||^ *[0-9]+ (passed|failed|flaky)' \
  <<<"$summary" || true
echo "expect-red: red as expected"
