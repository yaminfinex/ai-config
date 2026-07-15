#!/usr/bin/env bash
# check-hcom-contract.sh — lock the hcom bus-send contract (W3 identity
# resolution) against a hermetic mock `hcom`. This is the durable guard that
# registry-driven resolution + bus-scoped send behave to contract without a
# live bus. hcom is THE transport (TASK-003): a bus-less or unknown target is
# REFUSED, never typed at. Complements check-send-contract.sh (bus-only send
# goldens). Asserts, driving the REAL herder send CLI:
#
#   resolution — a registry row with a non-empty hcom_name routes to its bus
#                name; a bus-less row (and its term_* coordinates) REFUSES
#                with exit 2 (no keystroke fallback exists).
#   scoping    — the send scopes every hcom call to the peer's recorded hcom_dir
#                (proves recorded-bus isolation) and never leaks HCOM_DIR to the caller.
#   addressing — the send goes to @<hcom_name> (the recorded bus name), not the
#                user-facing guid/label, with --from <sender>.
#   verify     — deliver: ack in the window ⇒ verify=delivered/exit 0; no ack ⇒
#                queued/exit 0; not joined ⇒ exit 2; send failure ⇒ exit 1.
#
# Usage: check-hcom-contract.sh        # all assertions; nonzero exit on any failure
#
# HERDER_CMD_SEND_BIN may point at ANY executable honouring the herder send CLI
# (the bash script or the Go `bin/herder send` shim); it is exec'd directly,
# not via `bash`, so the same suite gates either implementation.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
# Env hygiene (TASK-019): herder-spawned agents export HERDER_BIN/AI_CONFIG_ROOT
# pointing at the spawner's checkout — honoring them silently drives another
# tree's wrapper/sources. Ignore the binary override; pin the root to THIS tree.
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"
HS=("$REPO_ROOT/bin/herder" send)
[[ -n "${HERDER_CMD_SEND_BIN:-}" ]] && HS=("$HERDER_CMD_SEND_BIN")

# Hermetic bin: mock `hcom` first on PATH, real jq/date/bash behind it.
# Deliberately NO herdr — optional caller-coordinate expansion may fail soft,
# while transport must remain bus-only; a lingering keystroke path fails loudly.
MOCKBIN="$(mktemp -d)"
ln -s "$TESTS_DIR/mock-hcom" "$MOCKBIN/hcom"

# Registry with a bus-bound peer (alpha team) and a bus-less peer (bash pane).
# The bus-less peer's terminal_id is term_AAA, so the resolution tests can
# prove that BOTH its label and its terminal coordinates are refused (exit 2)
# now that no keystroke fallback exists.
REG_DIR="$(mktemp -d)"
BUS_DIR="$(mktemp -d)"   # stands in for the team's HCOM_DIR
{
	jq -nc --arg dir "$BUS_DIR" \
	  '{kind:"session", guid:"g-sender", event:"seated", state:"seated", label:"sender", role:"lead", tool:"claude",
	    seat:{kind:"herdr", terminal_id:"term_SENDER", pane_id:"p_sender", namespace:$dir, hcom_name:"sender-bus"}}'
  jq -nc --arg dir "$BUS_DIR" \
    '{guid:"g-bus", short_guid:"busagent", label:"busagent", role:"reviewer", agent:"claude",
      terminal_id:"term_BUS", pane_id:"p_10",
      team:"alpha", hcom_dir:$dir, hcom_name:"busagent-rive", hcom_tag:"reviewer", status:"active"}'
  jq -nc \
    '{guid:"g-plain", short_guid:"plain", label:"plain", role:"worker", agent:"bash",
      terminal_id:"term_AAA", pane_id:"p_10",
      team:"", hcom_dir:"", hcom_name:"", hcom_tag:"", status:"active"}'
} > "$REG_DIR/registry.jsonl"

cleanup() { rm -rf "$MOCKBIN" "$REG_DIR" "$BUS_DIR"; }
trap cleanup EXIT

PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

fail=0
ok()   { printf 'PASS  %s\n' "$1"; }
bad()  { printf 'FAIL  %s — %s\n' "$1" "$2"; fail=1; }

# Drive the herder send CLI under test. $1=HERDER_BUS
# $2=MOCK_HCOM_SCENARIO $3=MOCK_HCOM_PROBE $4=ambient HCOM_DIR or empty; rest=args.
run_send() {
  local bus="$1" scen="$2" probe="$3" ambient_dir="$4" errf
  shift 4
  errf="$(mktemp)"
  if [[ -n "$ambient_dir" ]]; then
    RUN_OUT="$(env -i \
      PATH="$PATH_HERMETIC" HOME="$HOME" HCOM_DIR="$ambient_dir" \
	  HERDR_ENV=1 HERDR_PANE_ID=p_sender HERDER_GUID=g-sender HERDER_STATE_DIR="$REG_DIR" HERDER_BUS="$bus" \
      MOCK_HCOM_SCENARIO="$scen" MOCK_HCOM_PROBE="$probe" \
      "${HS[@]}" "$@" 2>"$errf")"
  else
    RUN_OUT="$(env -i \
      PATH="$PATH_HERMETIC" HOME="$HOME" \
	  HERDR_ENV=1 HERDR_PANE_ID=p_sender HERDER_GUID=g-sender HERDER_STATE_DIR="$REG_DIR" HERDER_BUS="$bus" \
      MOCK_HCOM_SCENARIO="$scen" MOCK_HCOM_PROBE="$probe" \
      "${HS[@]}" "$@" 2>"$errf")"
  fi
  RUN_RC=$?
  RUN_ERR="$(cat "$errf")"
  rm -f "$errf"
}

# ---- 1. resolution (registry-driven, HERDER_BUS=auto), proven end-to-end ----
# A bus row routes to its recorded bus name (dry-run reports the hcom
# transport); a bus-less row REFUSES — by label AND by its term_* coordinates —
# because no keystroke fallback exists anymore (TASK-003).
run_send auto delivered "$(mktemp -d)" "" --dry-run --json busagent
[[ "$RUN_RC" -eq 0 ]] && grep -q '"transport":"hcom"' <<<"$RUN_OUT" \
  && ok "resolve: bus row → hcom" || bad "resolve: bus row → hcom" "rc=$RUN_RC out='$RUN_OUT'"

run_send auto delivered "$(mktemp -d)" "" --dry-run --json plain
[[ "$RUN_RC" -eq 2 ]] && grep -q '"would":"refuse"' <<<"$RUN_OUT" \
  && ok "resolve: bus-less row → refuse (no keystroke fallback)" || bad "resolve: bus-less row → refuse" "rc=$RUN_RC out='$RUN_OUT' err=$RUN_ERR"
grep -q 'would REFUSE (exit 2)' <<<"$RUN_ERR" \
  && ok "resolve: bus-less row refusal says refuse" || bad "resolve: bus-less row refusal says refuse" "err=$RUN_ERR"

run_send auto delivered "$(mktemp -d)" "" --dry-run --json term_AAA
[[ "$RUN_RC" -eq 2 ]] && grep -q '"would":"refuse"' <<<"$RUN_OUT" \
  && ok "resolve: term_* of bus-less row → refuse" || bad "resolve: term_* of bus-less row → refuse" "rc=$RUN_RC out='$RUN_OUT' err=$RUN_ERR"

# ---- 2. delivered: scoping + addressing + verify=delivered/exit 0 ----
P="$(mktemp -d)"
run_send hcom delivered "$P" "" --json busagent "hello world"
[[ "$RUN_RC" -eq 0 ]] && ok "delivered: exit 0" || bad "delivered: exit 0" "rc=$RUN_RC err=$RUN_ERR"
grep -q 'verify=delivered' <<<"$RUN_ERR" && ok "delivered: verify=delivered" || bad "delivered: verify=delivered" "err=$RUN_ERR"
grep -q '@busagent-rive'   <<<"$RUN_ERR" && ok "delivered: reports bus name" || bad "delivered: reports bus name" "err=$RUN_ERR"
# scoping: mock recorded the effective HCOM_DIR == the peer's recorded hcom_dir
[[ "$(cat "$P/hcom_dir" 2>/dev/null)" == "$BUS_DIR" ]] && ok "scoping: HCOM_DIR pinned to recorded bus" || bad "scoping: HCOM_DIR pinned" "got '$(cat "$P/hcom_dir" 2>/dev/null)' want '$BUS_DIR'"
# addressing: send used @<hcom_name>, --from the verified live sender, -- <message>
SARGV="$(cat "$P/send_argv" 2>/dev/null || true)"
grep -q -- '--from sender-bus' <<<"$SARGV" && ok "addressing: --from sender" || bad "addressing: --from sender" "argv='$SARGV'"
grep -q -- '@busagent-rive'       <<<"$SARGV" && ok "addressing: @hcom_name recipient" || bad "addressing: @hcom_name" "argv='$SARGV'"
grep -q -- '-- hello world'        <<<"$SARGV" && ok "addressing: message after --" || bad "addressing: message after --" "argv='$SARGV'"
# verify probe correlated on the RECEIVER instance + the sender's receipt
# context (receipts land as instance=<target>, context=deliver:<sender>)
grep -q -- '--agent busagent-rive' "$P/events_argv" 2>/dev/null && ok "verify: ack keyed on receiver instance" || bad "verify: ack keyed on receiver instance" "$(cat "$P/events_argv" 2>/dev/null)"
grep -q 'deliver:sender-bus' "$P/events_argv" 2>/dev/null && ok "verify: ack keyed on sender receipt context" || bad "verify: ack keyed on sender receipt context" "$(cat "$P/events_argv" 2>/dev/null)"
# JSON record shape
grep -q '"hcom_name":"busagent-rive"' <<<"$RUN_OUT" && ok "json: hcom_name field" || bad "json: hcom_name field" "out='$RUN_OUT'"

# ---- 3. queued: no ack in window ⇒ queued/exit 0 ----
run_send hcom queued "$(mktemp -d)" "" --timeout 1000 busagent "hi"
[[ "$RUN_RC" -eq 0 ]] && grep -q 'verify=queued' <<<"$RUN_ERR" \
  && ok "queued: exit 0 + verify=queued" || bad "queued" "rc=$RUN_RC err=$RUN_ERR"

# ---- 4. notjoined: list fails ⇒ exit 2 ----
run_send hcom notjoined "$(mktemp -d)" "" busagent "hi"
[[ "$RUN_RC" -eq 2 ]] && ok "notjoined: exit 2" || bad "notjoined: exit 2" "rc=$RUN_RC err=$RUN_ERR"

# ---- 5. sendfail: send returns nonzero ⇒ exit 1 ----
run_send hcom sendfail "$(mktemp -d)" "" busagent "hi"
[[ "$RUN_RC" -eq 1 ]] && ok "sendfail: exit 1" || bad "sendfail: exit 1" "rc=$RUN_RC err=$RUN_ERR"

# ---- 6. bus-less peer forced through hcom driver ⇒ exit 2 (won't send blind) ----
run_send hcom delivered "$(mktemp -d)" "" plain "hi"
[[ "$RUN_RC" -eq 2 ]] && ok "bus-less via hcom: exit 2" || bad "bus-less via hcom: exit 2" "rc=$RUN_RC err=$RUN_ERR"

# ---- 7. unregistered target forced through hcom ⇒ literal bus name on ambient bus ----
AMBIENT_REAL_DIR="$(mktemp -d)"
P="$(mktemp -d)"
run_send hcom delivered "$P" "$AMBIENT_REAL_DIR" --json ghost "literal hi"
[[ "$RUN_RC" -eq 0 ]] && ok "literal via hcom: exit 0" || bad "literal via hcom: exit 0" "rc=$RUN_RC err=$RUN_ERR"
grep -q -- '@ghost' "$P/send_argv" 2>/dev/null && ok "literal via hcom: sends @target" || bad "literal via hcom: sends @target" "$(cat "$P/send_argv" 2>/dev/null)"
[[ "$(cat "$P/hcom_dir" 2>/dev/null)" == "$AMBIENT_REAL_DIR" ]] && ok "literal via hcom: uses ambient HCOM_DIR" || bad "literal via hcom: uses ambient HCOM_DIR" "got '$(cat "$P/hcom_dir" 2>/dev/null)' want '$AMBIENT_REAL_DIR'"
rm -rf "$AMBIENT_REAL_DIR" "$P"

# ---- 8. herder send --dry-run mirrors hcom driver resolution ----
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
