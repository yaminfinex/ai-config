#!/usr/bin/env bash
# check-observer-contract.sh — hermetic observer-as-cache contract.

set -euo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
HERDER_ROOT="$REPO_ROOT/tools/herder"
GO_MOD="$HERDER_ROOT/go.mod"
GO_VERSION="$(awk '$1 == "go" {print $2; exit}' "$GO_MOD")"
TOOLCHAIN="$(awk '$1 == "toolchain" {print $2; exit}' "$GO_MOD")"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

[ -n "$GO_VERSION" ] || fail "cannot read Go version from $GO_MOD"
[ -z "$TOOLCHAIN" ] || [ "$TOOLCHAIN" = "go$GO_VERSION" ] ||
  fail "go.mod declares toolchain $TOOLCHAIN but pins go $GO_VERSION"
GO_ROOT="$(mise where "go@$GO_VERSION")" || fail "go@$GO_VERSION is unavailable through mise"
GO_BIN="$GO_ROOT/bin/go"
GO_HAVE="$(env -u GOROOT GOTOOLCHAIN=local "$GO_BIN" env GOVERSION 2>/dev/null)" ||
  fail "cannot execute pinned go@$GO_VERSION"
[ "${GO_HAVE#go}" = "$GO_VERSION" ] || fail "resolved Go ${GO_HAVE#go}, want $GO_VERSION"

[ ! -e "$HERDER_ROOT/internal/observercmd/grok.go" ] || fail "observer Grok adapter still exists"
[ ! -e "$HERDER_ROOT/internal/observercmd/grok_test.go" ] || fail "observer Grok tests still exist"
if rg -n 'grokbridge|seatcompletion|DoctrineDeliver|doctrineCandidates|recognisedCandidate|turnoverCandidate|credentialGUID' "$HERDER_ROOT/internal/observercmd"; then
  fail "observer authority, credential, doctrine, or Grok machinery remains"
fi

pattern='^(TestCacheStampCollapsesRecognitionAndTurnover|TestCacheStampDedupeKeepsProbeCorroboratedRow|TestCacheStampRetiresDeadRowsAfterGrace|TestCacheStampMarksRecordedOccupantMismatchDead|TestCacheStampMakesBlockedStateVisible|TestCacheStampBootRaceIsLastWriteWinsWithoutIdentityEvent|TestCacheStampBootingPaneWaitsForCorroboration|TestCacheStampRelocatesLiveIdentityBeforeDeath|TestCacheStampLiveBusWithoutRelocationNeverDies|TestObservePanesEnforcesAllChannelsBeforeDeath|TestObservePanesBusFailureCannotAgreeToDeath|TestCacheStampRelocatesLiveDedupeLoserBeforeDeadStamp|TestLiveBusRowUsesHcomJoinedClassification|TestReviewConflictingBusCorrelatesStillVetoDeath|TestOccupiedForeignSIDAliasDoesNotRelocate|TestObservedStampBypassesFrozenBindingLegality)$'
output="$(cd "$HERDER_ROOT" && env -u GOROOT GOTOOLCHAIN=local "$GO_BIN" test -count=1 -v ./internal/observercmd -run "$pattern")" || {
  printf '%s\n' "$output"
  fail "observer-as-cache contract failed"
}
printf '%s\n' "$output"

for name in \
  TestCacheStampCollapsesRecognitionAndTurnover \
  TestCacheStampDedupeKeepsProbeCorroboratedRow \
  TestCacheStampRetiresDeadRowsAfterGrace \
  TestCacheStampMarksRecordedOccupantMismatchDead \
  TestCacheStampMakesBlockedStateVisible \
  TestCacheStampBootRaceIsLastWriteWinsWithoutIdentityEvent \
  TestCacheStampBootingPaneWaitsForCorroboration \
  TestCacheStampRelocatesLiveIdentityBeforeDeath \
  TestCacheStampLiveBusWithoutRelocationNeverDies \
  TestObservePanesEnforcesAllChannelsBeforeDeath \
  TestObservePanesBusFailureCannotAgreeToDeath \
  TestCacheStampRelocatesLiveDedupeLoserBeforeDeadStamp \
  TestLiveBusRowUsesHcomJoinedClassification \
  TestReviewConflictingBusCorrelatesStillVetoDeath \
  TestOccupiedForeignSIDAliasDoesNotRelocate \
  TestObservedStampBypassesFrozenBindingLegality
do
  grep -Fq -- "--- PASS: $name" <<<"$output" || fail "$name did not run and pass"
done

printf 'ALL GREEN — observer ledger-cache invariants pass.\n'
