#!/usr/bin/env bash
# Lands the bottom PR of a stack: squash-merges it into master, retargets the PR stacked on it to
# master, rebases every PR above onto its new parent, and only then deletes the merged branch
# (deleting a base branch first makes GitHub close the PR stacked on it). Each rebased branch must
# carry the same patch as before, or the script stops before pushing it.
#
# Dry run by default: prints the plan. Pass --apply to merge and push.
#   bash scripts/land-stacked-pr.sh <pr-number> [--apply]
set -euo pipefail

cd "$(dirname "$0")/.."
pr="${1:?usage: land-stacked-pr.sh <pr-number> [--apply]}"
apply=false
[[ "${2:-}" == "--apply" ]] && apply=true

read -r base head_branch head_sha state < <(gh pr view "$pr" --json baseRefName,headRefName,headRefOid,state \
  --jq '"\(.baseRefName) \(.headRefName) \(.headRefOid) \(.state)"')
if [[ "$state" != OPEN || "$base" != master ]]; then
  echo "#$pr must be open and target master (it is $state, base $base)" >&2
  exit 1
fi
if ! gh pr checks "$pr" >/dev/null; then
  echo "#$pr has failing or pending checks: gh pr checks $pr" >&2
  exit 1
fi

# Walk the stack upward: each PR whose base is the branch below it.
stack_prs=()
stack_branches=()
below="$head_branch"
while :; do
  mapfile -t above < <(gh pr list --state open --base "$below" --json number,headRefName --jq '.[] | "\(.number) \(.headRefName)"')
  ((${#above[@]} == 0)) && break
  if ((${#above[@]} > 1)); then
    echo "More than one open PR is stacked on $below; land a linear stack" >&2
    exit 1
  fi
  read -r number branch <<<"${above[0]}"
  stack_prs+=("$number")
  stack_branches+=("$branch")
  below="$branch"
done

echo "Squash-merge #$pr ($head_branch at ${head_sha:0:7}) into master."
for i in "${!stack_prs[@]}"; do
  if ((i == 0)); then
    echo "Retarget #${stack_prs[i]} to master, and rebase ${stack_branches[i]} onto master."
  else
    echo "Rebase ${stack_branches[i]} (#${stack_prs[i]}) onto ${stack_branches[i - 1]}."
  fi
done
echo "Delete $head_branch."
if ! $apply; then
  echo "Dry run: pass --apply to do it."
  exit 0
fi

gh pr merge "$pr" --squash --match-head-commit "$head_sha"
if ((${#stack_prs[@]} > 0)); then
  gh pr edit "${stack_prs[0]}" --base master >/dev/null
fi

git fetch -q origin
worktree="$(mktemp -d)"
trap 'git worktree remove --force "$worktree" >/dev/null 2>&1 || true' EXIT
old_parent="$head_sha"
new_parent="$(git rev-parse origin/master)"
for branch in "${stack_branches[@]}"; do
  old="$(git rev-parse "origin/$branch")"
  git worktree add -q --detach "$worktree" "$old"
  if ! git -C "$worktree" rebase -q --onto "$new_parent" "$old_parent"; then
    git -C "$worktree" rebase --abort
    echo "Rebasing $branch onto its new parent conflicts; resolve it by hand" >&2
    exit 1
  fi
  new="$(git -C "$worktree" rev-parse HEAD)"
  before="$(git diff "$old_parent" "$old" | git patch-id --stable | cut -d' ' -f1)"
  after="$(git diff "$new_parent" "$new" | git patch-id --stable | cut -d' ' -f1)"
  if [[ "$before" != "$after" ]]; then
    echo "$branch's patch changed in the rebase; not pushing it" >&2
    exit 1
  fi
  git push -q --force-with-lease="$branch:$old" origin "$new:refs/heads/$branch"
  echo "Rebased $branch: ${old:0:7} -> ${new:0:7}"
  git worktree remove --force "$worktree"
  old_parent="$old"
  new_parent="$new"
done

git push -q origin --delete "$head_branch"
echo "Landed #$pr and deleted $head_branch."
