#!/usr/bin/env bash
# Lints the GitHub workflows (actionlint, which also shellchecks their `run:` blocks) and every
# shell script, in pinned containers so CI and a local run use the same versions.
set -euo pipefail

cd "$(dirname "$0")/.."
RUNTIME="$(command -v podman || command -v docker || true)"
if [[ -z "$RUNTIME" ]]; then
  echo "Linting the workflows and shell scripts needs podman or docker" >&2
  exit 1
fi

"$RUNTIME" run --rm -v "$PWD:/repo:Z" -w /repo docker.io/rhysd/actionlint:1.7.12 -color
"$RUNTIME" run --rm -v "$PWD:/repo:Z" -w /repo docker.io/koalaman/shellcheck:v0.11.0 -x scripts/*.sh
# A floating runner or image can change under a green commit: ubuntu-latest moving to 26.04 broke
# Playwright's Chromium install and blocked every deploy. Name a version instead.
if grep -nE 'runs-on:.*-latest|image:.*:latest\b|image: [^:]+$' .github/workflows/*.yml; then
  echo "Pin each runner and service image to a version, not latest" >&2
  exit 1
fi
echo "Workflows and shell scripts are clean."
