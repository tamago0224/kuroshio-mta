#!/usr/bin/env bash
set -euo pipefail

# HA drill helper for Issue #26.
# This script is intentionally explicit and non-destructive by default.
# It prints commands to run and can execute only when --apply is given.

SCENARIO="${1:-}"
APPLY="${2:-}"

if [[ -z "${SCENARIO}" ]]; then
  cat <<'USAGE'
Usage:
  scripts/chaos/run_ha_drill.sh <scenario> [--apply]

Scenarios:
  az-a-down       Stop AZ-A ingress/worker (simulated)
  broker-a-down   Stop broker A (simulated)
  dns-failure     Break DNS resolution for test container (simulated)

Default behavior is dry-run (print only).
USAGE
  exit 1
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

echo "Scenario: ${SCENARIO}"
echo "Mode: ${APPLY:---dry-run--}"

case "${SCENARIO}" in
  az-a-down)
    run "docker compose stop kuroshio-ingress-a"
    run "docker compose stop kuroshio-worker-a"
    ;;
  broker-a-down)
    run "docker compose stop kafka-a"
    ;;
  dns-failure)
    run "docker compose exec -T kuroshio-worker-a sh -lc 'echo nameserver 203.0.113.1 > /etc/resolv.conf'"
    ;;
  *)
    echo "unknown scenario: ${SCENARIO}" >&2
    exit 2
    ;;
esac

cat <<'CHECKS'
Post-checks:
  1) curl -fsS http://<observability-host>:9090/healthz
  2) curl -fsS http://<observability-host>:9090/slo
  3) Confirm backlog and retry metrics trend on dashboard
  4) Validate RTO/RPO against target in docs/architecture/ha_reference.md
CHECKS
