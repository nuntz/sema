#!/usr/bin/env bash
# After pushing master: waits for the CI run of a commit and fails unless test, e2e and deploy all
# succeeded, so "pushed" is never mistaken for "deployed". A run still queued after QUEUE_LIMIT
# seconds fails too: GitHub can leave a push's run queued with no jobs, uncancellable, for hours.
# Usage: scripts/watch-deploy.sh [sha]; without a sha it watches HEAD.
# QUEUE_LIMIT (default 600) and TIMEOUT (default 1800) are in seconds.
set -euo pipefail

cd "$(dirname "$0")/.."
SHA="$(git rev-parse "${1:-HEAD}")"
QUEUE_LIMIT="${QUEUE_LIMIT:-600}"
TIMEOUT="${TIMEOUT:-1800}"
POLL="${POLL:-30}"
started="$(date +%s)"

# latest_run prints "id|status|conclusion|createdAt" for the newest ci run of SHA on master, or
# nothing when there is none yet or GitHub didn't answer (both are retried).
latest_run() {
  gh run list --workflow ci.yml --branch master --commit "$SHA" --limit 1 \
    --json databaseId,status,conclusion,createdAt \
    -q '.[0] | select(.) | "\(.databaseId)|\(.status)|\(.conclusion)|\(.createdAt)"' 2>/dev/null || true
}

while :; do
  now="$(date +%s)"
  if ((now - started > TIMEOUT)); then
    echo "No finished ci run for ${SHA:0:7} after ${TIMEOUT}s" >&2
    exit 1
  fi
  IFS="|" read -r id status conclusion created <<<"$(latest_run)" || true
  if [[ -z "${id:-}" ]]; then
    sleep "$POLL"
    continue
  fi
  if [[ "$status" == completed ]]; then break; fi
  age=$((now - $(date -d "$created" +%s)))
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
for job in test e2e deploy; do
  if ! grep -qx "$job success" <<<"$jobs"; then
    echo "$job did not succeed, so ${SHA:0:7} is not deployed" >&2
    exit 1
  fi
done
echo "${SHA:0:7} is deployed."
