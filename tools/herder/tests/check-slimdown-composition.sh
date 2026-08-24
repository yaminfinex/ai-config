#!/usr/bin/env bash
# check-slimdown-composition.sh — surviving compact composition contract.
#
# Successor notes for the retired incident fixtures:
# - Incident 041 (stored-coordinate compact): this check pins hook-bound
#   `hcom term inject` before queued `hcom send`; tools/fleet's hermetic
#   selfcompact suite separately pins the self-helper.
# - Incident 262 (uncorroborated-bus spawn): the gateway verb is gone. Its
#   successor is the fleet wrapper obligation: a local launch with explicit
#   `--go`, pinned by tools/fleet/tests/check-fleet.sh.
# - Incident 268 (adopt circularity): the adopt verb is gone. Its successor is
#   hcom construction: hook-bound names are bus-unique and continuity uses
#   `hcom r <name-or-uuid>`.
# - The retired verb-side label, sender, and coordinate negatives transfer to
#   hcom construction, fleet wrapper obligations, and observer/occupant
#   hygiene respectively. The observer and occupant keep-greens remain in
#   their Go and hermetic contract suites.
#   Concretely: foreign-label refusal and the HERDER_LABEL workaround yield to
#   bus-unique names plus `hcom r`; compact mismatch/pane-gone refusal yields
#   to hook-bound term targeting plus observer/occupant fail-closed hygiene;
#   spawn sender multi-match refusal yields to the local fleet `--go` launch.
#
# Battery retirement successor notes:
# - check-compact-contract -> this check + tools/fleet/tests/check-fleet.sh.
# - check-spawn-contract, check-cull-busdrop, check-cull-closed-record ->
#   tools/fleet/tests/check-fleet.sh (wrapper placement, launch, courtesy, and close).
# - check-send-contract, check-send-resolution, check-hcom-contract -> direct
#   hcom construction; this check pins the ordered compact/send composition.
# - check-enroll-contract, check-rename-contract,
#   check-retire-reopen-contract -> observer hygiene + hook-bound hcom names.
# - check-fork-contract, check-resume-contract, check-wait-contract -> direct
#   hcom resume/fork and herdr/hcom read surfaces; check-live-contract keeps the
#   surviving read-only substrate wire pinned.
# - check-credential-contract -> no replacement gate: credential authority
#   existed only for the deleted gateways; fleet/hcom are the surviving local
#   lifecycle boundary.
# - check-node-contract -> registry/observer cache checks.
# - check-grok-doctor, check-grok-transport -> no successor; Grok support was
#   explicitly dropped by the teardown ruling.
# - check-launchers -> the direct-vendor resolver/exec contract in the new
#   tools/herder/tests/check-launchers.sh.
# - check-hook-bootstrap, check-launch-contract, check-launcher-doctor,
#   check-shims, check-statusline-snapshot -> hcom hook construction
#   (check-hcom-hooks) and the fleet wrapper suite.

set -euo pipefail

ROOT="$(mktemp -d)"
trap 'rm -rf "$ROOT"' EXIT
MOCKBIN="$ROOT/bin"
LOG="$ROOT/hcom.log"
STATE="$ROOT/state"
QUEUED="$ROOT/queued"
DELIVERED="$ROOT/delivered"
mkdir -p "$MOCKBIN"

cat >"$MOCKBIN/hcom" <<'MOCK_HCOM'
#!/usr/bin/env bash
set -euo pipefail
: "${MOCK_HCOM_LOG:?}" "${MOCK_HCOM_STATE:?}" "${MOCK_HCOM_QUEUED:?}" "${MOCK_HCOM_DELIVERED:?}"
case "${1:-} ${2:-}" in
  "term inject")
    [[ "${3:-}" == "worker-vava" ]]
    [[ "${4:-}" == "/compact retain the contract" ]]
    [[ "${5:-}" == "--enter" ]]
    printf 'term-inject compact\n' >>"$MOCK_HCOM_LOG"
    printf 'compacting\n' >"$MOCK_HCOM_STATE"
    ;;
  "send @worker-vava")
    [[ "$(cat "$MOCK_HCOM_STATE")" == "compacting" ]]
    [[ "$*" == *"--intent request"* ]]
    [[ "$*" == *"Continue after compaction"* ]]
    printf 'send queued\n' >>"$MOCK_HCOM_LOG"
    printf 'Continue after compaction\n' >"$MOCK_HCOM_QUEUED"
    ;;
  "__test settle")
    [[ "$(cat "$MOCK_HCOM_STATE")" == "compacting" ]]
    [[ -s "$MOCK_HCOM_QUEUED" ]]
    printf 'compact complete\n' >>"$MOCK_HCOM_LOG"
    mv "$MOCK_HCOM_QUEUED" "$MOCK_HCOM_DELIVERED"
    printf 'listening\n' >"$MOCK_HCOM_STATE"
    printf 'queued send delivered\n' >>"$MOCK_HCOM_LOG"
    ;;
  *)
    printf 'unexpected mock hcom call: %s\n' "$*" >&2
    exit 64
    ;;
esac
MOCK_HCOM
chmod +x "$MOCKBIN/hcom"

export MOCK_HCOM_LOG="$LOG"
export MOCK_HCOM_STATE="$STATE"
export MOCK_HCOM_QUEUED="$QUEUED"
export MOCK_HCOM_DELIVERED="$DELIVERED"
printf 'listening\n' >"$STATE"

PATH="$MOCKBIN:$PATH" hcom term inject worker-vava '/compact retain the contract' --enter
PATH="$MOCKBIN:$PATH" hcom send @worker-vava --intent request -- 'Continue after compaction'

[[ "$(cat "$STATE")" == "compacting" ]]
[[ -s "$QUEUED" && ! -e "$DELIVERED" ]]
[[ "$(sed -n '1p' "$LOG")" == "term-inject compact" ]]
[[ "$(sed -n '2p' "$LOG")" == "send queued" ]]

PATH="$MOCKBIN:$PATH" hcom __test settle

[[ "$(cat "$STATE")" == "listening" ]]
[[ "$(cat "$DELIVERED")" == "Continue after compaction" ]]
[[ "$(sed -n '3p' "$LOG")" == "compact complete" ]]
[[ "$(sed -n '4p' "$LOG")" == "queued send delivered" ]]

printf 'PASS  term-inject precedes queued-send delivery through compaction\n'
printf 'ALL GREEN — compact composition is hook-bound and ordered.\n'
