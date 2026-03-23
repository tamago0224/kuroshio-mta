#!/usr/bin/env bash
set -euo pipefail

# DR drill helper: backup -> simulated outage -> restore -> validation.

QUEUE_DIR="${1:-./var/queue}"
BACKUP_DIR="${2:-./var/backups}"
APPLY="${3:-}"

if [[ "${APPLY}" != "--apply" ]]; then
  cat <<USAGE
Usage:
  scripts/chaos/run_dr_drill.sh [queue-dir] [backup-dir] --apply

Without --apply this script only prints what would run.
USAGE
fi

run() {
  local cmd="$1"
  if [[ "${APPLY}" == "--apply" ]]; then
    echo "+ ${cmd}"
    eval "${cmd}"
  else
    echo "[dry-run] ${cmd}"
  fi
}

START_TS="$(date +%s)"

run "scripts/dr/backup_queue.sh '${QUEUE_DIR}' '${BACKUP_DIR}'"
run "echo 'simulate outage: stop MTA / queue backend here'"
run "LATEST=\$(ls -1t '${BACKUP_DIR}'/orinoco-queue-*.tar.gz | head -n1) && scripts/dr/restore_queue.sh \"\${LATEST}\" '${QUEUE_DIR}' --force"
run "echo 'post-check: go run ./cmd/kuroshio and validate /healthz + queue processing'"

END_TS="$(date +%s)"
DURATION="$((END_TS - START_TS))"
echo "drill_elapsed_seconds=${DURATION}"

cat <<CHECKS
Checks:
  - RPO: latest backup timestamp <= target window
  - RTO: restore+service recovery duration <= target window
  - queue consistency: pending/retry/dlq counts are sensible after restore
CHECKS
