#!/usr/bin/env bash
set -euo pipefail

# Offline DR helper. The operator must stop every writer before --apply.
QUEUE_DIR="${1:-./var/queue}"
BACKUP_DIR="${2:-./var/backups}"
APPLY="${3:-}"
STOPPED="${4:-}"

if [[ "${APPLY}" != "--apply" ]]; then
  cat <<USAGE
Usage:
  scripts/chaos/run_dr_drill.sh [queue-dir] [backup-dir] --apply --stopped

Stop the MTA before applying. --stopped acknowledges that all queue writers are stopped.
Without --apply this script only prints what would run.
USAGE
  printf '[dry-run] backup %q to %q, then restore the latest archive\n' "${QUEUE_DIR}" "${BACKUP_DIR}"
  exit 0
fi

if [[ "${STOPPED}" != "--stopped" || $# -gt 4 ]]; then
  echo "Stop all MTA queue writers, then pass --apply --stopped. Nothing was changed." >&2
  exit 1
fi

# A backup inside the queue would be deleted by restore --force.
QUEUE_PATH="$(realpath -m -- "${QUEUE_DIR}")"
BACKUP_PATH="$(realpath -m -- "${BACKUP_DIR}")"
if [[ "${BACKUP_PATH}" == "${QUEUE_PATH}" || "${BACKUP_PATH}" == "${QUEUE_PATH}/"* ]]; then
  echo "backup directory must be outside the queue directory" >&2
  exit 1
fi

START_TS="$(date +%s)"
scripts/dr/backup_queue.sh "${QUEUE_DIR}" "${BACKUP_DIR}"

LATEST=""
for archive in "${BACKUP_DIR}"/kuroshio-queue-*.tar.gz; do
  [[ -f "${archive}" ]] || continue
  if [[ -z "${LATEST}" || "${archive}" -nt "${LATEST}" ]]; then
    LATEST="${archive}"
  fi
done
[[ -n "${LATEST}" ]] || { echo "no backup archive found" >&2; exit 1; }
scripts/dr/restore_queue.sh "${LATEST}" "${QUEUE_DIR}" --force

END_TS="$(date +%s)"
echo "drill_elapsed_seconds=$((END_TS - START_TS))"

cat <<CHECKS
Manual post-checks:
  - Restart the MTA and validate /healthz and queue processing.
  - RPO: latest backup timestamp <= target window.
  - RTO: measure outage through service recovery separately; the duration above is backup+restore only.
  - Queue consistency: inbound/retry/dlq counts are sensible after restore.
CHECKS
