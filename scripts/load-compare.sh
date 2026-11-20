#!/usr/bin/env bash
# Load evidence (CODING_STANDARDS.md → Flaky tests): runs the named Playwright tests many times at
# once on the working tree, then again with a patch applied, and prints both pass counts. A
# change that passes one run at a time can still fail when every core is busy, as two #63
# attempts did (41 of 48 against master's 0). The patch is always reverted, even on Ctrl-C.
# Paste its output into the PR.
# Usage: scripts/load-compare.sh <patch|-> <spec> [playwright args, such as -g "title"]
#   <patch> is a `git diff` from the repo root ("-" reads it from stdin), usually the change
#   reversed: `git diff -R master -- web/src > base.patch` compares against master's code.
#   <spec> is relative to web/ or the repo root. SEMA_E2E_STRESS and SEMA_E2E_PORT pass through.
# LOAD_REPEAT (default 48) and LOAD_WORKERS (default nproc) set the load.
# Exit codes: 0 when the working tree passed every run, 1 when any failed, 2 for a usage error, a
# patch that doesn't apply, no test run, or an interrupt.
set -euo pipefail

cd "$(dirname "$0")/.."
if [[ $# -lt 2 ]]; then
  sed -n '2,13p' "$0" >&2
  exit 2
fi
patch_arg="$1"
spec="${2#web/}"
shift 2
repeat="${LOAD_REPEAT:-48}"
workers="${LOAD_WORKERS:-$(nproc)}"

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
  echo "load-compare: the patch doesn't apply to the working tree" >&2
  exit 2
fi

# run prints "<passed> <failed>" for one loaded run of the named tests.
run() {
  local status=0 summary passed failed
  (cd web && bunx playwright test "$spec" --retries=0 --reporter=line \
    --repeat-each="$repeat" --workers="$workers" "$@") >"$output" 2>&1 || status=$?
  # The line reporter redraws progress with ANSI escapes; strip them so the summary greps cleanly.
  summary="$(sed 's/\x1b\[[0-9;]*[A-Za-z]//g' "$output")"
  if grep -qE '^ *[0-9]+ interrupted' <<<"$summary"; then
    echo "load-compare: interrupted" >&2
    exit 2
  fi
  passed="$(grep -oE '^ *[0-9]+ passed' <<<"$summary" | grep -oE '[0-9]+' || echo 0)"
  failed="$(grep -oE '^ *[0-9]+ failed' <<<"$summary" | grep -oE '[0-9]+' || echo 0)"
  if ((passed + failed == 0)); then
    echo "$summary" | tail -20 >&2
    echo "load-compare: no test ran; check the spec path and -g title" >&2
    exit 2
  fi
  if ((status != 0 && failed == 0)); then
    echo "$summary" | tail -20 >&2
    echo "load-compare: playwright failed without a test failing" >&2
    exit 2
  fi
  echo "$passed $failed"
}

echo "load-compare: $repeat runs each, $workers workers${SEMA_E2E_STRESS:+, SEMA_E2E_STRESS=$SEMA_E2E_STRESS}"
# run's exit 2 leaves only its command substitution, so each call passes its status on.
counts="$(run "$@")" || exit
read -r tree_passed tree_failed <<<"$counts"
echo "  working tree: $tree_passed passed, $tree_failed failed"
git apply "$patch"
applied=1
counts="$(run "$@")" || exit
read -r base_passed base_failed <<<"$counts"
echo "  with $(git apply --numstat "$patch" | awk '{print $3}' | paste -sd ' ') patched: $base_passed passed, $base_failed failed"
((tree_failed == 0))
