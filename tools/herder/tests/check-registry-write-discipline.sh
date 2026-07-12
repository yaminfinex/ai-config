#!/usr/bin/env bash
# check-registry-write-discipline.sh — gate the flocked-writer invariants.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
HERDER_ROOT="$REPO_ROOT/tools/herder"

unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"

cd "$HERDER_ROOT" || exit 1

test_names=(
  TestLoadPreservesFourStateViewFromV2Rows
  TestReadPredicatesKeepSeatAndLeaseQuestionsSeparate
  TestTwoProcessLabelClaimsOneWinner
  TestLockedWriteMintsNodeOnceAndStampsRows
  TestTwoProcessFirstWritersConvergeOnOneNode
  TestLockedWriteRefusesHalfPresentNodeState
  TestNodeInitRepairsAndCloneRepairKeepsPriorRows
  TestLockedValidatorPreservesRenameAgainstStaleEnrichment
  TestLockedValidatorDoesNotResurrectUnseatedSession
  TestLockedWriterRefusesUnlocked
)

require_declared_tests() {
  local listed_tests="$1"
  shift

  local test_name
  local missing=0
  for test_name in "$@"; do
    if ! grep -Fxq "$test_name" <<<"$listed_tests"; then
      printf 'DECLARED REGISTRY GATE TEST MISSING — "%s" does not exist; fix its name in check-registry-write-discipline.sh.\n' "$test_name" >&2
      missing=1
    fi
  done
  return "$missing"
}

if ! listed_tests="$(go test ./internal/registry -list '^Test')"; then
  printf '\nREGISTRY WRITE-DISCIPLINE TEST DISCOVERY FAILED — fix the compile or listing failure above; the gate cannot verify its declared tests.\n'
  exit 1
fi

# Exercise the fail-closed path so a missing declaration can never become harmless.
missing_test_probe=TestRegistryWriteDisciplineGateMissingNameProbe
if require_declared_tests "$listed_tests" "$missing_test_probe" >/dev/null 2>&1; then
  printf '\nREGISTRY WRITE-DISCIPLINE GATE SELF-CHECK FAILED — the deliberately nonexistent test "%s" was accepted; fix missing-name validation in check-registry-write-discipline.sh.\n' "$missing_test_probe"
  exit 1
fi

if ! require_declared_tests "$listed_tests" "${test_names[@]}"; then
  exit 1
fi

test_pattern="^($(IFS='|'; printf '%s' "${test_names[*]}"))$"
if ! test_output="$(go test -v ./internal/registry -run "$test_pattern")"; then
  printf '%s\n' "$test_output"
  printf '\nREGISTRY WRITE-DISCIPLINE CONTRACT DRIFT — fix the failing test or its declared name in check-registry-write-discipline.sh.\n'
  exit 1
fi

printf '%s\n' "$test_output"

for test_name in "${test_names[@]}"; do
  if ! grep -Fq -- "=== RUN   $test_name" <<<"$test_output" ||
    ! grep -Fq -- "--- PASS: $test_name " <<<"$test_output"; then
    printf '\nDECLARED REGISTRY GATE TEST DID NOT EXECUTE AND PASS — "%s" lacks RUN or PASS output; fix its name or execution in check-registry-write-discipline.sh.\n' "$test_name"
    exit 1
  fi
done

printf '\nALL GREEN — registry write-discipline invariants pass.\n'
