#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/test/integration/docker-compose.yml"

docker compose -f "$COMPOSE_FILE" up -d --build dnsmock policy
trap 'docker compose -f "$COMPOSE_FILE" down -v' EXIT

docker compose -f "$COMPOSE_FILE" run --rm tester \
  go test -tags=integration ./integration/...
