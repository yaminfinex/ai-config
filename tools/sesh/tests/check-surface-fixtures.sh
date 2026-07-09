#!/usr/bin/env bash
# U7 surface gate — fixture-backed render checks (M0 shape; live index
# integration lands at M2 behind the same Store seam). Prints ALL GREEN on
# success per the house harness contract.
set -euo pipefail

cd "$(dirname "$0")/.."
export GOTOOLCHAIN=local

fail() { echo "FAIL: $*" >&2; exit 1; }

go vet ./internal/surface/... || fail "go vet ./internal/surface/..."
# -count=1: cached greens are not evidence (playbook gate rule).
go test -count=1 ./internal/surface/... || fail "surface tests"
go test -count=1 ./tests || fail "module guard + fixture tests"

echo "ALL GREEN"
