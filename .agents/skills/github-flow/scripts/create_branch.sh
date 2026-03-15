#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 <branch-name>" >&2
  exit 1
fi

branch_name="$1"

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "Not inside a Git repository" >&2
  exit 1
fi

if [[ "$branch_name" == "main" || "$branch_name" == "master" ]]; then
  echo "Refusing to create or switch to protected branch name: $branch_name" >&2
  exit 1
fi

if git rev-parse --verify --quiet "refs/heads/$branch_name" >/dev/null; then
  git switch "$branch_name"
  echo "Switched to existing branch: $branch_name"
  exit 0
fi

if git switch -c "$branch_name"; then
  echo "Created and switched to branch: $branch_name"
  exit 0
fi

cat >&2 <<EOF
Failed to create branch: $branch_name

Troubleshooting:
- Ensure the branch name is valid and not blocked by repository settings.
- Try a branch name without "/" (for example: feature-foo) if your environment has ref path restrictions.
- This script intentionally does not modify .git internals directly.
EOF
exit 1
