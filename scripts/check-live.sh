#!/usr/bin/env bash
# After a deploy: the site root serves the SPA, and the API refuses a request without a session,
# which proves CloudFront still routes /api through API Gateway to the API Lambda.
# Usage: scripts/check-live.sh [url]; without a url it reads the selected stack's `url` output.
set -euo pipefail

cd "$(dirname "$0")/.."
SITE_URL="${1:-$(cd infra && pulumi stack output url)}"
SITE_URL="${SITE_URL%/}"

# expect_status PATH STATUS [PATTERN] retries for about a minute while CloudFront picks up the deploy.
expect_status() {
  local path="$1" expected="$2" pattern="${3:-}" body status=""
  body="$(mktemp)"
  for attempt in 1 2 3 4 5; do
    status="$(curl -sS -o "$body" -w '%{http_code}' "$SITE_URL$path" || true)"
    if [[ "$status" == "$expected" ]] && { [[ -z "$pattern" ]] || grep -q "$pattern" "$body"; }; then
      echo "$path → $status"
      rm -f "$body"
      return 0
    fi
    sleep $((attempt * 5))
  done
  rm -f "$body"
  if [[ "$status" == "$expected" ]]; then
    echo "$SITE_URL$path answered $status, but its body lacks $pattern" >&2
  else
    echo "$SITE_URL$path answered ${status:-nothing}, expected $expected" >&2
  fi
  return 1
}

expect_status / 200 '<div id="root"></div>'
expect_status /api/items 401
