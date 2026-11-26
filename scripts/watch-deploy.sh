#!/usr/bin/env bash
# After pushing master: waits for the CI run of a commit and fails unless test, e2e, every
# e2e-stress-* leg and deploy all succeeded, so "pushed" is never mistaken for "deployed". A run
# still queued after QUEUE_LIMIT seconds fails too: GitHub can leave a push's run queued with no
# jobs, uncancellable, for hours.
# Usage: scripts/watch-deploy.sh [sha]; without a sha it watches HEAD.
# QUEUE_LIMIT (default 600) and TIMEOUT (default 1800) are in seconds.
set -euo pipefail

cd "$(dirname "$0")/.."
SHA="$(git rev-parse "${1:-HEAD}")"
QUEUE_LIMIT="${QUEUE_LIMIT:-600}"
TIMEOUT="${TIMEOUT:-1800}"
POLL="${POLL:-30}"
started="$(date +%s)"

# latest_run prints "id|status|conclusion|createdAt|startedAt" for the newest ci run of SHA on
# master, or nothing when there is none yet or GitHub didn't answer (both are retried). startedAt
# is the current attempt's start: a re-run keeps the run's createdAt but resets startedAt.
latest_run() {
  gh run list --workflow ci.yml --branch master --commit "$SHA" --limit 1 \
    --json databaseId,status,conclusion,createdAt,startedAt \
    -q '.[0] | select(.) | "\(.databaseId)|\(.status)|\(.conclusion)|\(.createdAt)|\(.startedAt)"' 2>/dev/null || true
}

while :; do
  now="$(date +%s)"
  if ((now - started > TIMEOUT)); then
    echo "No finished ci run for ${SHA:0:7} after ${TIMEOUT}s" >&2
    exit 1
  fi
  IFS="|" read -r id status conclusion created attempt_started <<<"$(latest_run)" || true
  if [[ -z "${id:-}" ]]; then
    sleep "$POLL"
    continue
  fi
  if [[ "$status" == completed ]]; then break; fi
  # Queued since the later of the two, so a re-run isn't timed from its first attempt.
  queued_since="$(date -d "$created" +%s)"
  attempt_since="$(date -d "${attempt_started:-$created}" +%s 2>/dev/null || echo 0)"
  ((attempt_since > queued_since)) && queued_since="$attempt_since"
  age=$((now - queued_since))
  if [[ "$status" == queued ]] && ((age > QUEUE_LIMIT)); then
    echo "ci run $id for ${SHA:0:7} has been queued for ${age}s without starting." >&2
    echo "It can't be cancelled; start a fresh run: gh workflow run ci.yml --ref master" >&2
    exit 1
  fi
  sleep "$POLL"
done

jobs=""
for _ in 1 2 3 4 5; do
  jobs="$(gh run view "$id" --json jobs -q '.jobs[] | "\(.name) \(.conclusion)"' 2>/dev/null)" && break
  sleep "$POLL"
done
echo "ci run $id for ${SHA:0:7}: $conclusion"
echo "$jobs"
# failure_lines prints why a job failed: its log's test failures, assertion errors and error
# annotations, without the log's colour codes. gh run view --log refuses while any other job still
# runs, so it reads the job's log from the API (#88). An e2e leg names its tests better through
# scripts/ci-e2e-report.sh, which it points at.
failure_lines() {
  local job_id
  job_id="$(gh run view "$id" --json jobs -q ".jobs[] | select(.name == \"$1\") | .databaseId" 2>/dev/null)" || true
  [[ -n "$job_id" ]] || return 0
  gh api --allow-escape-sequences "repos/{owner}/{repo}/actions/jobs/$job_id/logs" 2>/dev/null |
    sed -E 's/\x1b\[[0-9;]*[A-Za-z]//g; s/^[0-9T:.Z-]+ //' |
    grep -E 'FAIL |AssertionError|Error:|##\[error\]' | head -n 20 >&2 || true
  if [[ "$1" == e2e* ]]; then echo "Its failing tests: scripts/ci-e2e-report.sh $id" >&2; fi
}
succeeded() {
  if ! grep -qx "$1 success" <<<"$jobs"; then
    echo "$1 did not succeed, so ${SHA:0:7} is not deployed" >&2
    failure_lines "$1"
    exit 1
  fi
}
succeeded test
succeeded e2e
# One e2e-stress-<mode> job per SEMA_E2E_STRESS matrix leg, matched by name rather than listed. A
# run with none fails too. They come before deploy, which CI skips when one fails.
stress="$(grep -oE '^e2e-stress-[^ ]+' <<<"$jobs" || true)"
if [[ -z "$stress" ]]; then
  echo "no e2e-stress leg ran, so ${SHA:0:7} is not deployed" >&2
  exit 1
fi
while read -r job; do succeeded "$job"; done <<<"$stress"
succeeded deploy
echo "${SHA:0:7} is deployed."
