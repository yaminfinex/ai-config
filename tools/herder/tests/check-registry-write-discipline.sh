#!/usr/bin/env bash
# check-registry-write-discipline.sh — gate the A2 flocked writer invariants.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
HERDER_ROOT="$REPO_ROOT/tools/herder"

unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"

cd "$HERDER_ROOT" || exit 1

if go test ./internal/registry -run 'Test(LoadDerivesLegacyViewFromV2Rows|ConcurrentLabelClaimsOneWinner|LockedValidatorPreservesRenameAgainstStaleEnrichment|LockedValidatorDoesNotResurrectUnseatedSession|LockedWriterRefusesUnlocked)$'; then
  printf '\nALL GREEN — registry write-discipline invariants pass.\n'
  exit 0
fi

printf '\nREGISTRY WRITE-DISCIPLINE CONTRACT DRIFT — see failures above.\n'
exit 1
