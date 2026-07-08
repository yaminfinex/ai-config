#!/usr/bin/env bash
# check-registry-v2.sh — gate the registry v2 loader/projection fixtures.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
HERDER_ROOT="$REPO_ROOT/tools/herder"

unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"

cd "$HERDER_ROOT" || exit 1

if go test ./internal/registry ./internal/registry/v2; then
  printf '\nALL GREEN — registry v2 projection fixtures pass.\n'
  exit 0
fi

printf '\nREGISTRY V2 CONTRACT DRIFT — see failures above.\n'
exit 1
