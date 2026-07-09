#!/usr/bin/env bash
# U7 surface gate — fixture-backed render checks (M0 shape; live index
# integration lands at M2 behind the same Store seam). Prints ALL GREEN on
# success per the house harness contract.
set -euo pipefail

cd "$(dirname "$0")/.."
export GOTOOLCHAIN=local

fail() { echo "FAIL: $*" >&2; exit 1; }

# Preflight: GOTOOLCHAIN=local (deliberate — the gate must not silently
# download a different toolchain) means the go on PATH must itself satisfy
# go.mod. Fail with the fix instead of a confusing compile error.
PINNED_EXPORT='export PATH=/home/grace/.local/share/mise/installs/go/1.26.4/bin:$PATH && export GOTOOLCHAIN=local'
need=$(awk '/^go /{print $2; exit}' go.mod)
command -v go >/dev/null 2>&1 ||
  fail "no 'go' on PATH; this module needs go >= ${need}. Playbook-pinned toolchain: ${PINNED_EXPORT}"
have=$(go env GOVERSION); have=${have#go}
if [ "$(printf '%s\n' "$need" "$have" | sort -V | head -n1)" != "$need" ]; then
  fail "go ${have} on PATH is older than the go.mod requirement (${need}) and GOTOOLCHAIN=local forbids auto-download. Playbook-pinned toolchain: ${PINNED_EXPORT}"
fi

go vet ./internal/surface/... || fail "go vet ./internal/surface/..."
# -count=1: cached greens are not evidence (playbook gate rule).
go test -count=1 ./internal/surface/... || fail "surface tests"
go test -count=1 ./tests || fail "module guard + fixture tests"

echo "ALL GREEN"
