#!/bin/bash
# Post-merge cleanup. Usage: post-merge-cleanup.sh <merged-branch>
# Returns the main checkout to main, removes any leftover worktree still on the
# merged branch (dev agents historically leak these), deletes the local branch,
# and fast-forwards main. Tolerant: every step is best-effort.

BRANCH="$1"
REPO_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_DIR" || exit 1

git checkout main 2>/dev/null || true

# Remove leftover worktrees checked out on the merged branch.
git worktree list --porcelain | awk '/^worktree /{print $2}' | while read -r wt; do
  [ "$wt" = "$REPO_DIR" ] && continue
  wb=$(git -C "$wt" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
  if [ -n "$BRANCH" ] && [ "$wb" = "$BRANCH" ]; then
    git worktree remove --force "$wt" 2>/dev/null || true
  fi
done
git worktree prune 2>/dev/null

if [ -n "$BRANCH" ]; then
  git branch -D "$BRANCH" 2>/dev/null || true
fi

git pull --ff-only origin main
git status --short
