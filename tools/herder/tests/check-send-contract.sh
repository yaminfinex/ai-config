#!/usr/bin/env bash
# check-send-contract.sh — lock the BUS-ONLY herder send contract (TASK-003)
# with committed golden fixtures. hcom is THE transport: every target form
# (guid | short-guid | label | terminal_id | pane_id) resolves through the
# spawn registry to a recorded bus name, and a target that cannot resolve to a
# bus-bound agent is REFUSED with exit 2 — keystrokes are never typed. The old
# herdr keystroke transport (and its goldens: normal/busy/bootrace/noenter/
# force/modal*/timeout/dryrun_*) was removed; the one surviving keystroke path
# is spawn's boot-time initial-prompt paste, exercised by
# check-spawn-contract.sh, not here. Proof the transport is gone: this suite
# puts NO herdr on the hermetic PATH — a send that still tried keystrokes
# would fail loudly.
#
# Drives the REAL herder send CLI against a hermetic mock `hcom` and diffs the
# stderr human line, the --json record, exit code, AND the recorded hcom argv
# (addressing + bus scoping) against goldens/send/<scenario>.txt.
#
# Usage:
#   check-send-contract.sh            # verify current worktree herder send vs goldens
#   check-send-contract.sh --write    # (re)generate goldens from $HERDER_CMD_SEND_BIN
#   HERDER_CMD_SEND_BIN=/path/to/herder-send check-send-contract.sh [--write]
#
# HERDER_CMD_SEND_BIN may point at ANY executable honouring the herder send
# CLI; it is exec'd directly, so the same suite gates any implementation.
#
# Determinism: the registry + recorded bus dir are per-run tempdirs normalized to
# <BUS>, and the --after ISO timestamp in the recorded events argv is
# normalized to <TS>.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
# Env hygiene (TASK-019): herder-spawned agents export HERDER_BIN/AI_CONFIG_ROOT
# pointing at the spawner's checkout — honoring them silently drives another
# tree's wrapper/sources. Ignore the binary override; pin the root to THIS tree.
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"
GOLDENS="$TESTS_DIR/goldens/send"
HS=("$REPO_ROOT/bin/herder" send)
[[ -n "${HERDER_CMD_SEND_BIN:-}" ]] && HS=("$HERDER_CMD_SEND_BIN")

WRITE=0
[[ "${1:-}" == "--write" ]] && WRITE=1

# Hermetic bin dir: mock `hcom` first on PATH, real jq/awk/grep behind it.
# Deliberately NO herdr here — bus-only send must never need one.
MOCKBIN="$(mktemp -d)"
ln -s "$TESTS_DIR/mock-hcom" "$MOCKBIN/hcom"

# Registry: a bus-bound peer, a bus-less peer (bash pane), and a
# RETIRED bus-bound session — its pane/terminal coordinates must NOT resolve
# (coordinates are positional; retired sessions are refused on that path).
REG_DIR="$(mktemp -d)"
BUS_DIR="$(mktemp -d)"
{
  jq -nc --arg dir "$BUS_DIR" \
    '{kind:"session", guid:"guid-alpha-0000", event:"seated", state:"seated", label:"alpha", role:"reviewer", tool:"claude", team:"alpha-team",
      seat:{kind:"herdr", terminal_id:"term_AAA", pane_id:"p_10", namespace:$dir, hcom_name:"alpha-rive"},
      provenance:{tag:"reviewer"}}'
  jq -nc \
    '{kind:"session", guid:"guid-plain-0000", event:"seated", state:"seated", label:"plain", role:"worker", tool:"bash",
      seat:{kind:"herdr", terminal_id:"term_BBB", pane_id:"p_20"}}'
  jq -nc --arg dir "$BUS_DIR" \
    '{kind:"session", guid:"guid-gone-0000", event:"retired", state:"retired", label:"gone", role:"worker", tool:"claude",
      seat:{kind:"herdr", terminal_id:"term_CCC", pane_id:"p_30", namespace:$dir, hcom_name:"gone-lilo"},
      provenance:{tag:"worker"}}'
  jq -nc --arg dir "$BUS_DIR" \
    '{guid:"guid-legacy-0000", short_guid:"legacy", label:"legacy", role:"worker", agent:"claude",
      terminal_id:"term_DDD", pane_id:"p_40", hcom_dir:$dir, hcom_name:"legacy-bus", status:"active"}'
  jq -nc --arg dir "$BUS_DIR" \
    '{kind:"session", guid:"guid-dormant-0000", event:"unseated", state:"unseated", label:"dormant", role:"worker", tool:"claude",
      seat:{kind:"herdr", terminal_id:"term_EEE", pane_id:"p_50", namespace:$dir, hcom_name:"dormant-bus"}}'
} > "$REG_DIR/registry.jsonl"

trap 'rm -rf "$MOCKBIN" "$REG_DIR" "$BUS_DIR"' EXIT
mkdir -p "$GOLDENS"

MSG='ring: alpha unit DONE'

# scenario name | HERDER_BUS | mock hcom scenario | herder send args (@MSG@ substituted)
SCENARIOS=(
  "delivered|auto|delivered|--json alpha @MSG@"
  "queued|auto|queued|--timeout 1000 --json alpha @MSG@"
  # P2 regression (codex review): a receipt from a PREVIOUS same-sender send
  # sits inside the backdated --after window on every poll — the pre-send
  # snapshot must pin it and verify must report queued, never delivered.
  "stale_receipt|auto|stalereceipt|--timeout 1000 --json alpha @MSG@"
  "notjoined|auto|notjoined|alpha @MSG@"
  "sendfail|auto|sendfail|alpha @MSG@"
  "resolve_term|auto|delivered|--json term_AAA @MSG@"
  "resolve_pane|auto|delivered|--json p_10 @MSG@"
  "refuse_busless|auto|delivered|--json plain @MSG@"
  "refuse_busless_term|auto|delivered|term_BBB @MSG@"
  "refuse_unknown|auto|delivered|--json ghost @MSG@"
  "refuse_closed_pane|auto|delivered|p_30 @MSG@"
  "refuse_legacy_active_term|auto|delivered|term_DDD @MSG@"
  "refuse_v2_unseated_term|auto|delivered|term_EEE @MSG@"
  "dryrun_bus|auto|delivered|--dry-run --json alpha"
  "dryrun_busless|auto|delivered|--dry-run --json plain"
  "dryrun_unknown|auto|delivered|--dry-run --json ghost"
  "herdr_forced_error|herdr|delivered|alpha @MSG@"
  "flag_noenter_gone|auto|delivered|--no-enter alpha @MSG@"
)

run_one() {  # $1=HERDER_BUS ('auto' → unset-equivalent), $2=mock scenario, rest=args
  local bus="$1" scen="$2"; shift 2
  local probe err out code
  [[ "$bus" == "auto" ]] && bus=""
  probe="$(mktemp -d)"
  err="$(mktemp)"
  out="$(env -i \
    PATH="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin" \
    HOME="$HOME" \
    HERDR_ENV=1 HERDER_BUS="$bus" HERDER_LABEL="orchestrator" \
    HERDER_STATE_DIR="$REG_DIR" \
    MOCK_HCOM_SCENARIO="$scen" MOCK_HCOM_PROBE="$probe" \
    "${HS[@]}" "$@" 2>"$err")"
  code=$?
  printf '=== STDERR ===\n%s\n=== STDOUT ===\n%s\n=== EXIT ===\n%s\n' \
    "$(cat "$err")" "$out" "$code"
  printf '=== HCOM SEND ARGV ===\n%s\n=== HCOM EVENTS ARGV ===\n%s\n=== HCOM DIR ===\n%s\n' \
    "$(cat "$probe/send_argv" 2>/dev/null)" \
    "$(cat "$probe/events_argv" 2>/dev/null)" \
    "$(cat "$probe/hcom_dir" 2>/dev/null)"
  rm -rf "$probe" "$err"
}

fail=0
for row in "${SCENARIOS[@]}"; do
  IFS='|' read -r name bus scen argstr <<<"$row"
  # shellcheck disable=SC2206
  args=(); for a in $argstr; do [[ "$a" == "@MSG@" ]] && args+=("$MSG") || args+=("$a"); done

  block="$(run_one "$bus" "$scen" "${args[@]}")"
  block="${block//$BUS_DIR/<BUS>}"
  block="$(sed -E 's/[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z/<TS>/g' <<<"$block")"
  gold="$GOLDENS/$name.txt"

  if [[ "$WRITE" -eq 1 ]]; then
    printf '%s\n' "$block" > "$gold"
    printf 'WROTE  %s\n' "$name"
    continue
  fi

  if [[ ! -f "$gold" ]]; then
    printf 'MISSING GOLDEN  %s (run --write first)\n' "$name"; fail=1; continue
  fi
  if diff -u "$gold" <(printf '%s\n' "$block") >/tmp/hs_diff.$$  2>&1; then
    printf 'PASS  %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"; cat /tmp/hs_diff.$$; fail=1
  fi
  rm -f /tmp/hs_diff.$$
done

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "${HS[*]}"; exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — bus-only send contract matches goldens.\n'; exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'; exit 1
fi
