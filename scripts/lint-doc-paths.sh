#!/usr/bin/env bash
# Fails when a tracked Markdown file names, in backticks, a repo path that doesn't exist, so a doc
# can't keep pointing at a file a change deleted or renamed. It checks paths under the top-level
# source directories, and bare file names (`foo_test.go`) anywhere in the tree. Gitignored paths
# (web/.env.local) and Go symbols (internal/connector.Connector) are not files and pass.
set -euo pipefail

cd "$(dirname "$0")/.."
tracked="$(git ls-files)"
missing=0

while IFS= read -r doc; do
  # shellcheck disable=SC2016 # the backticks are Markdown code spans, not command substitution
  while IFS= read -r ref; do
    ref="${ref%%#*}"
    case "$ref" in
      '' | *[\<\>\*\{\}\$@:]*) continue ;;
      cmd/* | internal/* | web/* | infra/* | docs/* | scripts/* | .github/*)
        [[ -e "$ref" ]] && continue
        git check-ignore -q "$ref" && continue
        # A Go symbol: an existing package directory followed by .Identifier.
        [[ "$ref" =~ ^(.+)\.[A-Z][A-Za-z0-9_]*$ && -d "${BASH_REMATCH[1]}" ]] && continue
        ;;
      */*) continue ;;
      *.go | *.ts | *.tsx | *.sh | *.yml | *.md)
        grep -q -x -e "$ref" -e ".*/$ref" <<<"$tracked" && continue
        ;;
      *) continue ;;
    esac
    echo "$doc names \`$ref\`, which doesn't exist" >&2
    missing=1
  done < <(grep -o '`[^` ]*`' "$doc" | tr -d '`' | sort -u)
done < <(git ls-files '*.md')

if ((missing)); then
  exit 1
fi
echo "Docs name only existing paths."
