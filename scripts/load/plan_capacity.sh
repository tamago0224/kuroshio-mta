#!/usr/bin/env bash
set -euo pipefail

# Convert newline-delimited JSON summaries from cmd/smtpload into a markdown table.

INPUT="${1:-}"

if [[ -z "${INPUT}" ]]; then
  echo "usage: $0 <results.ndjson>" >&2
  exit 2
fi

if [[ ! -f "${INPUT}" ]]; then
  echo "results file not found: ${INPUT}" >&2
  exit 1
fi

jq -sr '
  (
    [
    "| scenario | concurrency | requested | succeeded | failed | tps | avg_ms | p95_ms | max_ms |",
    "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |"
    ][]
  ),
  (
    .[] as $row |
    "| \($row.scenario // "unknown") | \($row.concurrency) | \($row.requested) | \($row.succeeded) | \($row.failed) | \($row.tps) | \($row.avg_ms) | \($row.p95_ms) | \($row.max_ms) |"
  )
' "${INPUT}"
