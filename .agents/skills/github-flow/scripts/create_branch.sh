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

git switch -c "$branch_name"
echo "Created and switched to branch: $branch_name"
