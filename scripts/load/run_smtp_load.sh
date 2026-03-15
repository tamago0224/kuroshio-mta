#!/usr/bin/env bash
set -euo pipefail

# SMTP load scenario runner for Issue #35.

SCENARIO="${1:-normal}"
ADDR="${2:-127.0.0.1:2525}"

case "${SCENARIO}" in
  normal)
    CONCURRENCY=10
    MESSAGES=200
    ;;
  peak)
    CONCURRENCY=50
    MESSAGES=2000
    ;;
  degraded)
    CONCURRENCY=20
    MESSAGES=500
    ;;
  *)
    echo "unknown scenario: ${SCENARIO}" >&2
    echo "usage: $0 <normal|peak|degraded> [addr]" >&2
    exit 2
    ;;
esac

echo "scenario=${SCENARIO} addr=${ADDR} concurrency=${CONCURRENCY} messages=${MESSAGES}"

go run ./cmd/smtpload \
  -addr "${ADDR}" \
  -concurrency "${CONCURRENCY}" \
  -messages "${MESSAGES}" \
  -from "loadtest@orinoco.local" \
  -to "receiver@orinoco.local"
