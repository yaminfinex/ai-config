#!/usr/bin/env bash
# check-send-resolution.sh — lock TASK-035 pane-id resolution: a reused pane
# accumulates several LIVE=working manual rows, and `herder send <pane-id>`
# must NOT silently pick one. Resolution prefers the single row whose recorded
# bus name is currently JOINED and refuses loudly (exit 2, candidate list) when
# a pane coordinate is ambiguous — 0 or >1 rows bus-live.
#
# Boundary pinned here per the wave-6 fence note: bus liveness is a TIEBREAKER
# among MULTIPLE candidates, never a new gate on every send. A single candidate
# for a coordinate resolves exactly as before — a joined row delivers, a
# busy/not-yet-acked row queues, a bus-less row is refused for having no bus
# name — none of which go through the ambiguity refusal.
#
# Hermetic: a mock `hcom` whose `list <name>` joins only STUB_JOINED names, and
# whose `events` acks only when STUB_ACK=1. NO real bus, NO herdr on PATH.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
# Env hygiene (TASK-019): ignore an inherited HERDER_BIN; pin the root to THIS tree.
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"
HS=("$REPO_ROOT/bin/herder" send)
[[ -n "${HERDER_CMD_SEND_BIN:-}" ]] && HS=("$HERDER_CMD_SEND_BIN")

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
BUS_DIR="$ROOT/bus"
REG_DIR="$ROOT/state"
mkdir -p "$MOCKBIN" "$BUS_DIR" "$REG_DIR"
trap 'rm -rf "$ROOT"' EXIT

cat >"$MOCKBIN/hcom" <<'MOCK_HCOM'
#!/usr/bin/env bash
set -uo pipefail
PROBE="${MOCK_PROBE:-/tmp/mock-send-resolution}"
mkdir -p "$PROBE"
case "${1:-}" in
  list)
    # joined iff the queried name is listed in STUB_JOINED (space-separated)
    for n in ${STUB_JOINED:-}; do [[ "$n" == "${2:-}" ]] && exit 0; done
    exit 1;;
  send)
    printf '%s\n' "$*" >>"$PROBE/sends"
    exit 0;;
  events)
    # Stateful ack, mirroring mock-hcom: snapshot (first call) sees nothing,
    # later polls see this send's receipt — but only when STUB_ACK=1.
    if [[ "${STUB_ACK:-0}" == "1" && -f "$PROBE/polled" ]]; then
      jq -cn '{id:42, data:{context:"deliver:orchestrator"}, type:"status"}'
    else
      : >"$PROBE/polled"
    fi
    exit 0;;
  *) exit 1;;
esac
MOCK_HCOM
chmod +x "$MOCKBIN/hcom"

# Registry: pane p_reuse carries three ACTIVE manual rows (the reused-pane bug),
# each bus-bound. pane p_solo carries exactly ONE active bus-bound row (single-
# candidate path). pane p_bash carries one active BUS-LESS row.
{
  jq -nc --arg d "$BUS_DIR" '{guid:"guid-hera-0000", short_guid:"hera", label:"hera", role:"lead", agent:"claude", pane_id:"p_reuse", terminal_id:"term_reuse", hcom_dir:$d, hcom_name:"hera-rive", status:"active"}'
  jq -nc --arg d "$BUS_DIR" '{guid:"guid-vore-0000", short_guid:"vore", label:"vore", role:"worker", agent:"claude", pane_id:"p_reuse", terminal_id:"term_reuse", hcom_dir:$d, hcom_name:"vore-lilo", status:"active"}'
  jq -nc --arg d "$BUS_DIR" '{guid:"guid-zero-0000", short_guid:"zero", label:"zero", role:"worker", agent:"claude", pane_id:"p_reuse", terminal_id:"term_reuse", hcom_dir:$d, hcom_name:"zero-mano", status:"active"}'
  jq -nc --arg d "$BUS_DIR" '{guid:"guid-solo-0000", short_guid:"solo", label:"solo", role:"worker", agent:"claude", pane_id:"p_solo", terminal_id:"term_solo", hcom_dir:$d, hcom_name:"solo-teki", status:"active"}'
  jq -nc '{guid:"guid-bash-0000", short_guid:"bash", label:"bashrow", role:"worker", agent:"bash", pane_id:"p_bash", terminal_id:"term_bash", hcom_dir:"", hcom_name:"", status:"active"}'
} > "$REG_DIR/registry.jsonl"

fail=0
run_send() {  # $1=joined-names $2=ack(0/1); rest=send args -> sets RC/ERR
  local joined="$1" ack="$2"; shift 2
  ERR="$ROOT/err"; PROBE="$ROOT/probe.$RANDOM"; mkdir -p "$PROBE"
  OUT="$(env -i \
    PATH="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin" \
    HOME="$HOME" \
    HERDR_ENV=1 HERDER_LABEL="orchestrator" \
    HERDER_STATE_DIR="$REG_DIR" \
    STUB_JOINED="$joined" STUB_ACK="$ack" MOCK_PROBE="$PROBE" \
    "${HS[@]}" "$@" 2>"$ERR")"
  RC=$?
}

want() {  # $1=label $2=want-rc; then --grep <pat>... / --nogrep <pat>...
  local label="$1" wrc="$2"; shift 2
  local ok=1 mode="grep"
  [[ "$RC" -eq "$wrc" ]] || { ok=0; printf 'FAIL  %s — rc=%s want %s\n      stderr: %s\n' "$label" "$RC" "$wrc" "$(cat "$ERR")"; }
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --grep) mode="grep";;
      --nogrep) mode="nogrep";;
      *)
        if [[ "$mode" == "grep" ]]; then
          grep -qF -- "$1" "$ERR" || { ok=0; printf 'FAIL  %s — stderr missing %q\n      stderr: %s\n' "$label" "$1" "$(cat "$ERR")"; }
        else
          grep -qF -- "$1" "$ERR" && { ok=0; printf 'FAIL  %s — stderr should NOT contain %q\n      stderr: %s\n' "$label" "$1" "$(cat "$ERR")"; }
        fi;;
    esac
    shift
  done
  [[ "$ok" -eq 1 ]] && printf 'PASS  %s\n' "$label" || fail=1
}

# --- MULTI-CANDIDATE: liveness is the tiebreaker ---------------------------

# One live (@hera) among two stale — deliver to the live one.
run_send "hera-rive" 1 p_reuse "ring: DONE"
want "reuse: one live delivers to it" 0 --grep "@hera-rive" "verify=delivered" \
  --nogrep "refusing to guess" "refused"

# terminal_id form resolves the same reused-pane candidate set.
run_send "vore-lilo" 1 term_reuse "ring: DONE"
want "reuse(term): one live delivers to it" 0 --grep "@vore-lilo"

# Zero live — cannot tell which session owns the pane; refuse with full list.
run_send "" 0 p_reuse "ring: DONE"
want "reuse: none live refuses" 2 \
  --grep "none is joined" "guid-hera-0000" "guid-vore-0000" "guid-zero-0000" "Nothing was sent" \
  --nogrep "verify=delivered"

# Two live at once — genuine ambiguity; refuse and list the LIVE rows only.
run_send "hera-rive zero-mano" 1 p_reuse "ring: DONE"
want "reuse: multi live refuses" 2 \
  --grep "bus-live at once" "guid-hera-0000" "guid-zero-0000" "Nothing was sent" \
  --nogrep "guid-vore-0000"

# --- SINGLE-CANDIDATE: unchanged from pre-TASK-035 (no ambiguity path) ------

# One candidate, joined + acked → delivered (never touches the tiebreaker).
run_send "solo-teki" 1 p_solo "ring: DONE"
want "solo: single joined delivers" 0 --grep "@solo-teki" "verify=delivered" \
  --nogrep "refusing to guess" "none is joined"

# One candidate, joined but no receipt in window → QUEUED, not refused.
run_send "solo-teki" 0 --timeout 1000 p_solo "ring: DONE"
want "solo: single busy queues (not refused)" 0 --grep "verify=queued" \
  --nogrep "refused" "refusing to guess"

# One candidate, NOT joined → deliver path's not_joined (exit 2), NOT the
# ambiguity refusal — proves single rows skip the candidate-list refuse.
run_send "" 0 p_solo "ring: DONE"
want "solo: single not-joined uses deliver refuse" 2 \
  --grep "not found on bus" \
  --nogrep "none is joined" "refusing to guess" "Candidates:"

# One candidate, bus-less bash row → refused for no bus name (unchanged).
run_send "" 0 p_bash "ring: DONE"
want "bash: single bus-less refused for no bus name" 2 \
  --grep "no recorded bus name" \
  --nogrep "none is joined" "refusing to guess"

if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — reused-pane resolution refuses to guess; single-candidate path unchanged.\n'; exit 0
else
  printf '\nRESOLUTION CONTRACT FAILED — see failures above.\n'; exit 1
fi
