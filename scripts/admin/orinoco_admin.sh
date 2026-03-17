#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${ORINOCO_ADMIN_URL:-http://127.0.0.1:9091}"
TOKEN="${ORINOCO_ADMIN_TOKEN:-}"
ACTOR="${ORINOCO_ADMIN_ACTOR:-cli}"

if [[ -z "${TOKEN}" ]]; then
  echo "ORINOCO_ADMIN_TOKEN is required" >&2
  exit 1
fi

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <command> [args...]" >&2
  exit 2
fi

auth_args=(
  -H "Authorization: Bearer ${TOKEN}"
  -H "X-Admin-Actor: ${ACTOR}"
  -H "Content-Type: application/json"
)

cmd="$1"
shift

case "${cmd}" in
  list-suppressions)
    curl -fsS "${auth_args[@]}" "${BASE_URL}/api/v1/suppressions"
    ;;
  add-suppression)
    addr="${1:-}"
    reason="${2:-manual}"
    curl -fsS "${auth_args[@]}" -X POST \
      -d "{\"address\":\"${addr}\",\"reason\":\"${reason}\"}" \
      "${BASE_URL}/api/v1/suppressions"
    ;;
  remove-suppression)
    addr="${1:-}"
    curl -fsS "${auth_args[@]}" -X DELETE \
      "${BASE_URL}/api/v1/suppressions/${addr}"
    ;;
  list-queue)
    state="${1:-retry}"
    limit="${2:-50}"
    curl -fsS "${auth_args[@]}" \
      "${BASE_URL}/api/v1/queue/${state}?limit=${limit}"
    ;;
  requeue)
    state="${1:-}"
    id="${2:-}"
    dry="${3:-}"
    url="${BASE_URL}/api/v1/queue/${state}/${id}/requeue"
    if [[ "${dry}" == "--dry-run" ]]; then
      url="${url}?dry_run=1"
    fi
    curl -fsS "${auth_args[@]}" -X POST "${url}"
    ;;
  *)
    echo "unknown command: ${cmd}" >&2
    exit 2
    ;;
esac
