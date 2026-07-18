#!/usr/bin/env bash
# Fail-closed gate for the Grok transport receipt and real-hcom contracts.
set -uo pipefail

TESTS_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$TESTS_DIR/../../.." && pwd -P)
HERDER_ROOT="$REPO_ROOT/tools/herder"
export GOTOOLCHAIN=local
unset GOROOT
GO_BIN=$(mise which go 2>/dev/null) || { printf 'GROK TRANSPORT GATE BLOCKED — mise cannot resolve the repository Go pin.\n' >&2; exit 1; }

real_hcom=${HERDER_TEST_HCOM_BIN:-}
if [[ -z $real_hcom ]]; then
  while IFS= read -r candidate; do
    [[ $candidate == *tools/herder/shims/hcom ]] && continue
    real_hcom=$candidate
    break
  done < <(type -a -p hcom 2>/dev/null || true)
fi
if [[ -z $real_hcom || ! -x $real_hcom ]]; then
  printf 'GROK TRANSPORT GATE BLOCKED — real hcom 0.7.23 is unavailable; install it or set HERDER_TEST_HCOM_BIN to its executable path. Real-bus tests cannot be skipped.\n' >&2
  exit 1
fi
if version=$($real_hcom --version 2>&1); then :; else
  printf 'GROK TRANSPORT GATE BLOCKED — cannot execute %s --version; repair the installation or set HERDER_TEST_HCOM_BIN correctly.\n' "$real_hcom" >&2
  exit 1
fi
if [[ $version != *'0.7.23'* ]]; then
  printf 'GROK TRANSPORT GATE BLOCKED — %s reports %q, but these contracts pin hcom 0.7.23; select the pinned binary.\n' "$real_hcom" "$version" >&2
  exit 1
fi
export HERDER_TEST_HCOM_BIN=$real_hcom

cd "$HERDER_ROOT" || exit 1

test_names=(
  TestReceiptStateMachineContracts
  TestT1InitialDeliveryThroughPendingFetchAck
  TestT2IdleDeliveryThroughTapFetchAck
  TestT4DuplicateWakeThroughTapIsIdempotent
  TestT5DuplicateAckThroughMCPIsIdempotent
  TestT6OutOfOrderMessagesDeliverIndependentlyThroughMCP
  TestT7AckBeforeFetchRejectedThroughMCP
  TestT8ForeignMessageIDRejectedThroughMCP
  TestT9QueuedBeforeWakeRecoversThroughPendingMCP
  TestT10RestartEmitsSingleRecoveryWithoutPerIDRewake
  TestT11TapDeathQueuesUntilRecoveryReconnect
  TestT12FetchedNotAckedPersistsAcrossBridgeRestart
  TestT13RecoveryRelistAfterMonitorReset
  TestT14SameSeatRestartRelistsPendingWithoutRegression
  TestT15FreshSeatCannotFetchParentPendingMessage
  TestT16SubagentBoundaryRejectsForeignAndUnownedSessionEvidence
  TestT17IdleBinderAndTapEmitZeroModelFacingBytes
  TestT18ReportingClaimsDeliveredOnlyAfterMCPAck
  TestT23DualBinderLockAndGenerationFence
  TestClientStraddlesBinderRestartReconnectsOnceAndDelivers
  TestPersistentMCPServerStraddlesBinderRestart
  TestSurfaceFailureIsDiagnosedAndTapDroppedForRecovery
  TestTapClientPreservesImmediateRecoveryLine
  TestRetireUnackedTransitionsOnlyPendingMessages
  TestSocketPathLengthPreflightNamesRemedy
  TestDefaultWaitUsesHcomScaleWithoutCorrectnessWeight
  TestRealHcomBindIdentityUsesSeatOwnedProcessAndPreservesForeignBinding
  TestStatusRepairsMissingBusRowBeforeReportingHealthy
  TestStatusRefusesHealthyClaimWhenBusRowCannotBeRebound
  TestOutboundSendRepairsMissingBusRowBeforeSending
  TestIdentityLoopRefreshesExactBusRow
  TestIdentityLoopTreatsRetirementAsOrderlyStop
  TestRealHcomReapedRowRebindPreservesQueuedDelivery
  TestReadInvocationChildEnvironmentScrubsPinnedIdentityInputs
  TestT24RealHcomStaleBacklogComesFromDrain
  TestT25RealHcomReadsAreIdentityFreeAndNonDestructive
  TestT26RealHcomDeliveredToRoutingExcludesSelf
  TestT27RealHcomPagedHostileOrderingSurvivesPrefixCrash
)

declare -A declared=()
for name in "${test_names[@]}"; do
  if [[ -n ${declared[$name]+present} ]]; then
    printf 'GROK TRANSPORT GATE DECLARATION DUPLICATED — %s appears twice; keep one distinct invariant anchor per declaration.\n' "$name" >&2
    exit 1
  fi
  declared[$name]=1
done
minimum_test_count=38
if ((${#declared[@]} < minimum_test_count)); then
  printf 'GROK TRANSPORT GATE DECLARATION FLOOR VIOLATED — only %d distinct tests remain; restore the %d-test invariant floor.\n' "${#declared[@]}" "$minimum_test_count" >&2
  exit 1
fi

require_declared_tests() {
  local listed=$1; shift
  local name missing=0
  for name in "$@"; do
    if ! grep -Fxq "$name" <<<"$listed"; then
      printf 'DECLARED GROK TRANSPORT TEST MISSING — %s does not exist; restore it or update the declaration to the replacement invariant test.\n' "$name" >&2
      missing=1
    fi
  done
  return "$missing"
}

executed_and_passed() {
  local output=$1 name=$2
  grep -Fq -- "=== RUN   $name" <<<"$output" && grep -Fq -- "--- PASS: $name " <<<"$output"
}

if ! listed=$("$GO_BIN" test ./internal/grokbridge -list '^Test'); then
  printf '\nGROK TRANSPORT TEST DISCOVERY FAILED — fix compilation before claiming the gate.\n' >&2
  exit 1
fi
missing_probe=TestGrokTransportGateMissingNameProbe
if require_declared_tests "$listed" "$missing_probe" >/dev/null 2>&1; then
  printf 'GROK TRANSPORT GATE SELF-CHECK FAILED — a nonexistent declaration was accepted.\n' >&2
  exit 1
fi
require_declared_tests "$listed" "${test_names[@]}" || exit 1

pattern="^($(IFS='|'; printf '%s' "${test_names[*]}"))$"
if ! output=$("$GO_BIN" test -v ./internal/grokbridge -run "$pattern"); then
  printf '%s\n' "$output"
  printf '\nGROK TRANSPORT CONTRACT DRIFT — fix the failing declared test.\n' >&2
  exit 1
fi
printf '%s\n' "$output"

skip_probe=$(printf '=== RUN   %s\n--- SKIP: %s (0.00s)\n' "$missing_probe" "$missing_probe")
if executed_and_passed "$skip_probe" "$missing_probe"; then
  printf 'GROK TRANSPORT GATE SELF-CHECK FAILED — skip-shaped output was accepted as PASS.\n' >&2
  exit 1
fi
for name in "${test_names[@]}"; do
  if ! executed_and_passed "$output" "$name"; then
    if grep -Fq -- "=== RUN   $name" <<<"$output"; then
      printf 'DECLARED GROK TRANSPORT TEST DID NOT PASS — %s may have skipped; real and hermetic contracts must PASS.\n' "$name" >&2
    else
      printf 'DECLARED GROK TRANSPORT TEST DID NOT RUN — %s lacks execution evidence.\n' "$name" >&2
    fi
    exit 1
  fi
done

if ! "$GO_BIN" test ./internal/cli; then
  printf 'GROK TRANSPORT CLI REGISTRATION DRIFT — repair the root command surface.\n' >&2
  exit 1
fi
printf '\nALL GREEN — Grok transport hermetic and real-hcom contracts pass fail-closed.\n'
