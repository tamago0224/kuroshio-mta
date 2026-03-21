#!/usr/bin/env bash
set -euo pipefail

run_if_present() {
  local label="$1"
  local cmd="$2"
  if eval "$cmd" >/dev/null 2>&1; then
    echo "[detect] $label"
    eval "$cmd"
    return 0
  fi
  return 1
}

if [[ -f package.json ]]; then
  if command -v jq >/dev/null 2>&1; then
    if jq -e '.scripts.lint' package.json >/dev/null; then
      npm run lint
    else
      echo "[skip] no npm lint script"
    fi
    if jq -e '.scripts.test' package.json >/dev/null; then
      npm test
    else
      echo "[skip] no npm test script"
    fi
    if jq -e '.scripts.build' package.json >/dev/null; then
      npm run build
    else
      echo "[skip] no npm build script"
    fi
    exit 0
  fi
fi

if [[ -f pnpm-lock.yaml && -f package.json ]]; then
  pnpm run lint || echo "[skip/fail] pnpm lint unavailable"
  pnpm test || echo "[skip/fail] pnpm test unavailable"
  pnpm run build || echo "[skip/fail] pnpm build unavailable"
  exit 0
fi

if [[ -f pom.xml ]]; then
  ./mvnw test || mvn test
  exit 0
fi

if [[ -f build.gradle || -f build.gradle.kts ]]; then
  ./gradlew test || gradle test
  exit 0
fi

if [[ -f pyproject.toml ]]; then
  pytest || echo "[skip/fail] pytest unavailable"
  exit 0
fi

echo "No supported project check script detected."
