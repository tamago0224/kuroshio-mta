#!/usr/bin/env bash
set -euo pipefail

# Restore queue state from backup archive.
# Example:
#   scripts/dr/restore_queue.sh ./var/backups/kuroshio-queue-20260316T000000Z.tar.gz ./var/queue --force

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 <backup-archive> [target-queue-dir] [--force]" >&2
  exit 1
fi

ARCHIVE="$1"
TARGET_DIR="${2:-./var/queue}"
FORCE="${3:-}"

if [[ ! -f "${ARCHIVE}" ]]; then
  echo "archive not found: ${ARCHIVE}" >&2
  exit 1
fi

if [[ -d "${TARGET_DIR}" ]] && [[ "$(find "${TARGET_DIR}" -mindepth 1 -print -quit 2>/dev/null || true)" != "" ]] && [[ "${FORCE}" != "--force" ]]; then
  echo "target dir is not empty: ${TARGET_DIR}" >&2
  echo "pass --force to overwrite" >&2
  exit 1
fi

mkdir -p "${TARGET_DIR}"

if [[ "${FORCE}" == "--force" ]]; then
  # Clear only children under the target directory.
  find "${TARGET_DIR}" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
fi

tar -xzf "${ARCHIVE}" -C "${TARGET_DIR}"

echo "restore_completed target=${TARGET_DIR} archive=${ARCHIVE}"
