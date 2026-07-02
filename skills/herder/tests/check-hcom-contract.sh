#!/usr/bin/env bash
# check-hcom-contract.sh — lock the hcom delivery-driver contract (W3 identity
# resolution) against a hermetic mock `hcom`. This is the durable guard that the
# registry-driven transport selection + bus-scoped send behave to contract without
# a live bus. Complements check-send-contract.sh (which locks the herdr keystroke
# path). Asserts, driving the REAL delivery-driver library:
#
#   selection  — a registry row with a non-empty hcom_name routes to the hcom
#                driver; a bus-less row (and unknown/term_* targets) route to herdr.
#   scoping    — hcom_send scopes every hcom call to the peer's recorded hcom_dir
#                (proves team-bus isolation) and never leaks HCOM_DIR to the caller.
#   addressing — the send goes to @<hcom_name> (the recorded bus name), not the
#                user-facing guid/label, with --from <sender>.
#   verify     — deliver: ack in the window ⇒ verify=delivered/exit 0; no ack ⇒
#                queued/exit 0; not joined ⇒ exit 2; send failure ⇒ exit 1.
#
# Usage: check-hcom-contract.sh        # all assertions; nonzero exit on any failure

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="$TESTS_DIR/../scripts/lib"
HS="$TESTS_DIR/../scripts/herder-send"

# Hermetic bin: mock `hcom`/`herdr` first on PATH, real jq/date/bash behind it.
MOCKBIN="$(mktemp -d)"
ln -s "$TESTS_DIR/mock-hcom" "$MOCKBIN/hcom"
ln -s "$TESTS_DIR/mock-herdr" "$MOCKBIN/herdr"

# Registry with a bus-bound peer (alpha team) and a bus-less peer (bash pane).
REG_DIR="$(mktemp -d)"
BUS_DIR="$(mktemp -d)"   # stands in for the team's HCOM_DIR
{
  jq -nc --arg dir "$BUS_DIR" \
    '{guid:"g-bus", short_guid:"busagent", label:"busagent", role:"reviewer", agent:"claude",
      terminal_id:"term_BUS", pane_id:"p_10",
      team:"alpha", hcom_dir:$dir, hcom_name:"busagent-rive", hcom_tag:"reviewer", status:"active"}'
  jq -nc \
    '{guid:"g-plain", short_guid:"plain", label:"plain", role:"worker", agent:"bash",
      terminal_id:"term_PLAIN", pane_id:"p_20",
      team:"", hcom_dir:"", hcom_name:"", hcom_tag:"", status:"active"}'
} > "$REG_DIR/registry.jsonl"

cleanup() { rm -rf "$MOCKBIN" "$REG_DIR" "$BUS_DIR"; }
trap cleanup EXIT

PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

fail=0
ok()   { printf 'PASS  %s\n' "$1"; }
bad()  { printf 'FAIL  %s — %s\n' "$1" "$2"; fail=1; }

# Run a snippet against the sourced delivery-driver library in a hermetic env.
# $1=HERDER_BUS $2=MOCK_HCOM_SCENARIO $3=MOCK_HCOM_PROBE $4=snippet. Echoes the
# snippet's stdout; exports RC (its exit) and ERRTXT (its stderr) to the caller
# via temp files referenced by the caller. Kept simple: returns via globals.
run_lib() {
  local bus="$1" scen="$2" probe="$3" snippet="$4"
  local errf; errf="$(mktemp)"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" HOME="$HOME" \
    HERDER_STATE_DIR="$REG_DIR" HERDER_BUS="$bus" HERDER_LABEL="orchestrator" \
    MOCK_HCOM_SCENARIO="$scen" MOCK_HCOM_PROBE="$probe" \
    bash -c "source '$LIB/delivery-driver.sh'; $snippet" 2>"$errf")"
  RUN_RC=$?
  RUN_ERR="$(cat "$errf")"
  rm -f "$errf"
}

# Drive the REAL worktree herder-send for dry-run contract coverage. $1=HERDER_BUS
# $2=MOCK_HCOM_SCENARIO $3=MOCK_HCOM_PROBE $4=ambient HCOM_DIR or empty; rest=args.
run_send() {
  local bus="$1" scen="$2" probe="$3" ambient_dir="$4" errf
  shift 4
  errf="$(mktemp)"
  if [[ -n "$ambient_dir" ]]; then
    RUN_OUT="$(env -i \
      PATH="$PATH_HERMETIC" HOME="$HOME" HCOM_DIR="$ambient_dir" \
      HERDR_ENV=1 HERDER_STATE_DIR="$REG_DIR" HERDER_BUS="$bus" HERDER_LABEL="orchestrator" \
      MOCK_HCOM_SCENARIO="$scen" MOCK_HCOM_PROBE="$probe" \
      bash "$HS" "$@" 2>"$errf")"
  else
    RUN_OUT="$(env -i \
      PATH="$PATH_HERMETIC" HOME="$HOME" \
      HERDR_ENV=1 HERDER_STATE_DIR="$REG_DIR" HERDER_BUS="$bus" HERDER_LABEL="orchestrator" \
      MOCK_HCOM_SCENARIO="$scen" MOCK_HCOM_PROBE="$probe" \
      bash "$HS" "$@" 2>"$errf")"
  fi
  RUN_RC=$?
  RUN_ERR="$(cat "$errf")"
  rm -f "$errf"
}

# ---- 1. selection (registry-driven, HERDER_BUS=auto) ----
run_lib auto delivered "$(mktemp -d)" 'select_driver busagent'
[[ "$RUN_OUT" == "hcom" ]] && ok "select: bus row → hcom" || bad "select: bus row → hcom" "got '$RUN_OUT'"

run_lib auto delivered "$(mktemp -d)" 'select_driver plain'
[[ "$RUN_OUT" == "herdr" ]] && ok "select: bus-less row → herdr" || bad "select: bus-less row → herdr" "got '$RUN_OUT'"

run_lib auto delivered "$(mktemp -d)" 'select_driver term_UNKNOWN'
[[ "$RUN_OUT" == "herdr" ]] && ok "select: unknown/term_* → herdr" || bad "select: unknown → herdr" "got '$RUN_OUT'"

# ---- 2. delivered: scoping + addressing + verify=delivered/exit 0 ----
P="$(mktemp -d)"
run_lib hcom delivered "$P" 'driver_dispatch send busagent "hello world" --json 1 || exit $?'
[[ "$RUN_RC" -eq 0 ]] && ok "delivered: exit 0" || bad "delivered: exit 0" "rc=$RUN_RC err=$RUN_ERR"
grep -q 'verify=delivered' <<<"$RUN_ERR" && ok "delivered: verify=delivered" || bad "delivered: verify=delivered" "err=$RUN_ERR"
grep -q '@busagent-rive'   <<<"$RUN_ERR" && ok "delivered: reports bus name" || bad "delivered: reports bus name" "err=$RUN_ERR"
# scoping: mock recorded the effective HCOM_DIR == the peer's recorded hcom_dir
[[ "$(cat "$P/hcom_dir" 2>/dev/null)" == "$BUS_DIR" ]] && ok "scoping: HCOM_DIR pinned to team bus" || bad "scoping: HCOM_DIR pinned" "got '$(cat "$P/hcom_dir" 2>/dev/null)' want '$BUS_DIR'"
# addressing: send used @<hcom_name>, --from orchestrator, -- <message>
SARGV="$(cat "$P/send_argv" 2>/dev/null || true)"
grep -q -- '--from orchestrator' <<<"$SARGV" && ok "addressing: --from sender" || bad "addressing: --from sender" "argv='$SARGV'"
grep -q -- '@busagent-rive'       <<<"$SARGV" && ok "addressing: @hcom_name recipient" || bad "addressing: @hcom_name" "argv='$SARGV'"
grep -q -- '-- hello world'        <<<"$SARGV" && ok "addressing: message after --" || bad "addressing: message after --" "argv='$SARGV'"
# verify probe correlated on the bus name
grep -q 'deliver:busagent-rive' "$P/events_argv" 2>/dev/null && ok "verify: ack keyed on bus name" || bad "verify: ack keyed on bus name" "$(cat "$P/events_argv" 2>/dev/null)"
# JSON record shape
grep -q '"hcom_name":"busagent-rive"' <<<"$RUN_OUT" && ok "json: hcom_name field" || bad "json: hcom_name field" "out='$RUN_OUT'"

# ---- 3. queued: no ack in window ⇒ queued/exit 0 ----
run_lib hcom queued "$(mktemp -d)" 'driver_dispatch send busagent "hi" --timeout 1000 || exit $?'
[[ "$RUN_RC" -eq 0 ]] && grep -q 'verify=queued' <<<"$RUN_ERR" \
  && ok "queued: exit 0 + verify=queued" || bad "queued" "rc=$RUN_RC err=$RUN_ERR"

# ---- 4. notjoined: list fails ⇒ exit 2 ----
run_lib hcom notjoined "$(mktemp -d)" 'driver_dispatch send busagent "hi" || exit $?'
[[ "$RUN_RC" -eq 2 ]] && ok "notjoined: exit 2" || bad "notjoined: exit 2" "rc=$RUN_RC err=$RUN_ERR"

# ---- 5. sendfail: send returns nonzero ⇒ exit 1 ----
run_lib hcom sendfail "$(mktemp -d)" 'driver_dispatch send busagent "hi" || exit $?'
[[ "$RUN_RC" -eq 1 ]] && ok "sendfail: exit 1" || bad "sendfail: exit 1" "rc=$RUN_RC err=$RUN_ERR"

# ---- 6. bus-less peer forced through hcom driver ⇒ exit 2 (won't send blind) ----
run_lib hcom delivered "$(mktemp -d)" 'driver_dispatch send plain "hi" || exit $?'
[[ "$RUN_RC" -eq 2 ]] && ok "bus-less via hcom: exit 2" || bad "bus-less via hcom: exit 2" "rc=$RUN_RC err=$RUN_ERR"

# ---- 7. unregistered target forced through hcom ⇒ literal bus name on ambient bus ----
AMBIENT_REAL_DIR="$(mktemp -d)"
P="$(mktemp -d)"
run_send hcom delivered "$P" "$AMBIENT_REAL_DIR" --json ghost "literal hi"
[[ "$RUN_RC" -eq 0 ]] && ok "literal via hcom: exit 0" || bad "literal via hcom: exit 0" "rc=$RUN_RC err=$RUN_ERR"
grep -q -- '@ghost' "$P/send_argv" 2>/dev/null && ok "literal via hcom: sends @target" || bad "literal via hcom: sends @target" "$(cat "$P/send_argv" 2>/dev/null)"
[[ "$(cat "$P/hcom_dir" 2>/dev/null)" == "$AMBIENT_REAL_DIR" ]] && ok "literal via hcom: uses ambient HCOM_DIR" || bad "literal via hcom: uses ambient HCOM_DIR" "got '$(cat "$P/hcom_dir" 2>/dev/null)' want '$AMBIENT_REAL_DIR'"
rm -rf "$AMBIENT_REAL_DIR" "$P"

# ---- 8. herder-send --dry-run mirrors hcom driver resolution ----
run_send hcom delivered "$(mktemp -d)" "" --dry-run --json busagent
[[ "$RUN_RC" -eq 0 ]] && ok "dry-run forced hcom bus row: exit 0" || bad "dry-run forced hcom bus row: exit 0" "rc=$RUN_RC err=$RUN_ERR"
grep -q '@busagent-rive' <<<"$RUN_ERR" && ok "dry-run forced hcom bus row: reports @hcom_name" || bad "dry-run forced hcom bus row: reports @hcom_name" "err=$RUN_ERR"
grep -q "HCOM_DIR=$BUS_DIR" <<<"$RUN_ERR" && ok "dry-run forced hcom bus row: reports recorded HCOM_DIR" || bad "dry-run forced hcom bus row: reports HCOM_DIR" "err=$RUN_ERR"

run_send hcom delivered "$(mktemp -d)" "" --dry-run --json plain
[[ "$RUN_RC" -eq 2 ]] && ok "dry-run forced hcom bus-less row: exit 2" || bad "dry-run forced hcom bus-less row: exit 2" "rc=$RUN_RC err=$RUN_ERR out=$RUN_OUT"
grep -q 'would REFUSE (exit 2)' <<<"$RUN_ERR" && ok "dry-run forced hcom bus-less row: says refuse" || bad "dry-run forced hcom bus-less row: says refuse" "err=$RUN_ERR"
grep -q '"would":"refuse"' <<<"$RUN_OUT" && ok "dry-run forced hcom bus-less row: json refuse" || bad "dry-run forced hcom bus-less row: json refuse" "out=$RUN_OUT"

AMBIENT_DIR="$(mktemp -d)"
run_send hcom delivered "$(mktemp -d)" "$AMBIENT_DIR" --dry-run --json ghost
[[ "$RUN_RC" -eq 0 ]] && ok "dry-run forced hcom literal target: exit 0" || bad "dry-run forced hcom literal target: exit 0" "rc=$RUN_RC err=$RUN_ERR"
grep -q '@ghost' <<<"$RUN_ERR" && ok "dry-run forced hcom literal target: reports @target" || bad "dry-run forced hcom literal target: reports @target" "err=$RUN_ERR"
grep -q "HCOM_DIR=$AMBIENT_DIR" <<<"$RUN_ERR" && ok "dry-run forced hcom literal target: reports ambient HCOM_DIR" || bad "dry-run forced hcom literal target: reports ambient HCOM_DIR" "err=$RUN_ERR"
rm -rf "$AMBIENT_DIR"

run_send auto delivered "$(mktemp -d)" "" --dry-run --json busagent
[[ "$RUN_RC" -eq 0 ]] && grep -q '"transport":"hcom"' <<<"$RUN_OUT" \
  && ok "dry-run auto bus row: reports hcom transport" || bad "dry-run auto bus row: reports hcom transport" "rc=$RUN_RC out=$RUN_OUT err=$RUN_ERR"

echo
if [[ "$fail" -eq 0 ]]; then
  printf 'ALL GREEN — hcom delivery-driver contract holds.\n'; exit 0
else
  printf 'CONTRACT DRIFT — see failures above.\n'; exit 1
fi
