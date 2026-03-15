#!/usr/bin/env bash
set -euo pipefail

# Load + chaos baseline suite for Issue #35.
# Runs normal load, optionally chaos drill, then degraded load.

ADDR="${1:-127.0.0.1:2525}"
APPLY="${2:-}"

echo "+ scripts/load/run_smtp_load.sh normal ${ADDR}"
scripts/load/run_smtp_load.sh normal "${ADDR}"

if [[ "${APPLY}" == "--apply" ]]; then
  echo "+ scripts/chaos/run_ha_drill.sh broker-a-down --apply"
  scripts/chaos/run_ha_drill.sh broker-a-down --apply
else
  echo "[info] chaos drill skipped (pass --apply to execute)"
fi

echo "+ scripts/load/run_smtp_load.sh degraded ${ADDR}"
scripts/load/run_smtp_load.sh degraded "${ADDR}"

echo "suite_completed"
