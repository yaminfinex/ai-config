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

minimum_test_count=10
declare -A declared_test_names=()
for test_name in "${test_names[@]}"; do
  if [[ -n "${declared_test_names[$test_name]+present}" ]]; then
    printf 'REGISTRY GATE DECLARATION DUPLICATED — "%s" appears more than once; remove the duplicate or replace it with the distinct test that anchors the missing invariant in check-registry-write-discipline.sh.\n' "$test_name" >&2
    exit 1
  fi
  declared_test_names["$test_name"]=1
done
distinct_test_count=${#declared_test_names[@]}
if ((distinct_test_count < minimum_test_count)); then
  printf 'REGISTRY GATE DECLARATION FLOOR VIOLATED — only %d distinct tests remain, but at least %d invariant anchors are required; restore any removed declarations or replace them with the tests that now anchor those invariants in check-registry-write-discipline.sh.\n' "$distinct_test_count" "$minimum_test_count" >&2
  exit 1
fi

require_declared_tests() {
  local listed_tests="$1"
  shift

  local test_name
  local missing=0
  for test_name in "$@"; do
    if ! grep -Fxq "$test_name" <<<"$listed_tests"; then
      printf 'DECLARED REGISTRY GATE TEST MISSING — "%s" does not exist; fix its name, or replace it with the test that now anchors this invariant in check-registry-write-discipline.sh.\n' "$test_name" >&2
      missing=1
    fi
  done
  return "$missing"
}

test_executed_and_passed() {
  local test_output="$1"
  local test_name="$2"

  grep -Fq -- "=== RUN   $test_name" <<<"$test_output" &&
    grep -Fq -- "--- PASS: $test_name " <<<"$test_output"
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

# Prove the execution-evidence check also rejects a known-absent test.
if test_executed_and_passed "$test_output" "$missing_test_probe"; then
  printf '\nREGISTRY WRITE-DISCIPLINE GATE SELF-CHECK FAILED — the absent test "%s" had RUN and PASS evidence; fix execution-evidence validation in check-registry-write-discipline.sh.\n' "$missing_test_probe"
  exit 1
fi

# Pin the PASS half: skip-shaped output has RUN evidence but must be rejected.
skip_probe_output="$(printf '=== RUN   %s\n--- SKIP: %s (0.00s)\n' "$missing_test_probe" "$missing_test_probe")"
if test_executed_and_passed "$skip_probe_output" "$missing_test_probe"; then
  printf '\nREGISTRY WRITE-DISCIPLINE GATE SELF-CHECK FAILED — skip-shaped output for "%s" was accepted without PASS evidence; restore PASS-evidence validation in check-registry-write-discipline.sh.\n' "$missing_test_probe"
  exit 1
fi

for test_name in "${test_names[@]}"; do
  if test_executed_and_passed "$test_output" "$test_name"; then
    continue
  fi
  if ! grep -Fq -- "=== RUN   $test_name" <<<"$test_output"; then
    printf '\nDECLARED REGISTRY GATE TEST DID NOT RUN — "%s" lacks RUN output; fix its name, or replace it with the test that now anchors this invariant in check-registry-write-discipline.sh.\n' "$test_name"
  else
    printf '\nDECLARED REGISTRY GATE TEST DID NOT PASS — "%s" may be skipped; un-skip it, or replace it with the test that now anchors this invariant in check-registry-write-discipline.sh.\n' "$test_name"
  fi
  exit 1
done

printf '\nALL GREEN — registry write-discipline invariants pass.\n'
