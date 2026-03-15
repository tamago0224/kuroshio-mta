#!/usr/bin/env bash
set -euo pipefail

# Create a compressed backup of queue state for DR operation.

QUEUE_DIR="${1:-./var/queue}"
BACKUP_DIR="${2:-./var/backups}"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
HOST="$(hostname 2>/dev/null || echo unknown)"
ARCHIVE="${BACKUP_DIR}/orinoco-queue-${TS}.tar.gz"

if [[ ! -d "${QUEUE_DIR}" ]]; then
  echo "queue dir not found: ${QUEUE_DIR}" >&2
  exit 1
fi

mkdir -p "${BACKUP_DIR}"

# Pack queue content as-is so runtime layout is preserved on restore.
tar -czf "${ARCHIVE}" \
  -C "${QUEUE_DIR}" .

echo "backup_created=${ARCHIVE} host=${HOST} queue_dir=${QUEUE_DIR}"
