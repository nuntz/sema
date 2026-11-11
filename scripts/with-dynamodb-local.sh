#!/usr/bin/env bash
# Runs a command with DynamoDB Local behind DYNAMODB_ENDPOINT, so the endpoint-gated Go tests
# (internal/dynamotest) run instead of skipping. CI's test job provides the endpoint as a service;
# locally this reuses whatever already answers on the port, else starts a pinned container and
# leaves it running for the next run (stop it with `podman rm -f sema-dynamodb-local`).
# SEMA_REQUIRE_DYNAMODB turns a missing endpoint into a test failure rather than a skip.
set -euo pipefail

# Keep in step with the dynamodb service in .github/workflows/ci.yml.
IMAGE=docker.io/amazon/dynamodb-local:3.3.2
NAME=sema-dynamodb-local
PORT="${SEMA_DYNAMODB_PORT:-8000}"

if [[ -z "${DYNAMODB_ENDPOINT:-}" ]]; then
  endpoint="http://localhost:$PORT"
  if ! curl -s -o /dev/null -m 2 "$endpoint"; then
    RUNTIME="$(command -v podman || command -v docker || true)"
    if [[ -z "$RUNTIME" ]]; then
      echo "The DynamoDB Local tests need podman or docker, or DYNAMODB_ENDPOINT" >&2
      exit 1
    fi
    "$RUNTIME" rm -f "$NAME" >/dev/null 2>&1 || true
    "$RUNTIME" run -d --name "$NAME" -p "127.0.0.1:$PORT:8000" "$IMAGE" >/dev/null
    for _ in $(seq 50); do
      curl -s -o /dev/null -m 1 "$endpoint" && break
      sleep 0.2
    done
    if ! curl -s -o /dev/null -m 2 "$endpoint"; then
      echo "DynamoDB Local did not start on $endpoint" >&2
      exit 1
    fi
  fi
  export DYNAMODB_ENDPOINT="$endpoint"
fi
export SEMA_REQUIRE_DYNAMODB=1
exec "$@"
