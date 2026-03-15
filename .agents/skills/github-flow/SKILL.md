---
name: github-flow
description: Use this skill when the user asks to implement or modify code in this repository using GitHub Flow, to prepare a feature/fix branch, validate changes, and produce a pull request summary. Do not use this skill for one-off explanations of GitHub Flow with no code changes, or when the user explicitly asks to work directly on main.
---

# GitHub Flow implementation skill

## Purpose
Standardize repository work so Codex follows GitHub Flow during implementation tasks.

## When to use
Use this skill when:
- the task requires code changes in this repository
- the user wants work to proceed via GitHub + pull requests
- the task should stay isolated on a short-lived branch

Do not use this skill when:
- the user only wants a conceptual explanation
- the repository is not a Git repository
- the user explicitly instructs you to avoid GitHub Flow

## Required outcome
For every implementation task:
1. start from the latest `main`
2. create a short-lived branch
3. keep changes scoped to one concern
4. run local verification commands
5. summarize changes in PR-ready format

## Branch naming
Choose exactly one pattern based on the task:
- `feature/<topic>` for new behavior
- `fix/<topic>` for bug fixes
- `chore/<topic>` for maintenance
- `docs/<topic>` for documentation-only work

Use lowercase kebab-case for `<topic>`.

Examples:
- `feature/user-profile-edit`
- `fix/login-null-check`
- `chore/dependency-updates`

## Workflow
Follow these steps in order.

### 1) Inspect repository state
Run:
```bash
./.agents/skills/github-flow/scripts/git_status_check.sh
```

If there are unrelated uncommitted changes, call them out before proceeding.

### 2) Update local main
Run:
```bash
git fetch origin
git checkout main
git pull --rebase origin main
```

If the repo uses `master` instead of `main`, adapt accordingly.

### 3) Create a working branch
Create a branch that matches the naming rules.

Run:
```bash
./.agents/skills/github-flow/scripts/create_branch.sh <branch-name>
```

### 4) Implement minimally
Before coding, restate the implementation plan in 3–5 bullets.
Keep changes minimal and avoid unrelated refactors.

### 5) Validate locally
Prefer project-defined scripts. Detect and run the first applicable commands:

```bash
./.agents/skills/github-flow/scripts/run-checks.sh
```

If a check cannot run because the toolchain is missing or the project has no script for that category, say so explicitly.

### 6) Review diff
Run:
```bash
git status --short
git diff --stat
git diff -- . ':(exclude)package-lock.json' ':(exclude)pnpm-lock.yaml' || true
```

Confirm the diff matches the requested scope.

### 7) Prepare commit guidance
Propose a Conventional Commit style message:
- `feat: ...`
- `fix: ...`
- `chore: ...`
- `docs: ...`

### 8) Prepare PR summary
Always produce:
- suggested branch name
- suggested commit message
- PR title
- PR summary
- validation results
- risks / follow-ups

Use this template:

```md
## Summary
- ...

## Changes
- ...

## Validation
- ...

## Risks / Follow-ups
- ...
```

## Guardrails
- Never commit directly to `main`
- Never broaden scope without stating it explicitly
- Do not edit secrets, deployment config, or CI unless the task requires it
- Prefer existing project conventions over generic ones
- If AGENTS.md exists, follow it in addition to this skill

## Notes for repositories with CI
If `.github/workflows/` exists, inspect workflow names and mention likely CI checks in the PR summary.
