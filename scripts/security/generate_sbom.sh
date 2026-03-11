#!/usr/bin/env bash
set -euo pipefail

OUT_DIR="${1:-./artifacts/sbom}"
mkdir -p "${OUT_DIR}"

# Minimal SBOM-like output based on Go module graph.
go list -m -json all > "${OUT_DIR}/go-modules.sbom.json"

echo "generated: ${OUT_DIR}/go-modules.sbom.json"
