#!/usr/bin/env bash
# check-enroll-contract.sh — lock the herder enroll registry/provenance contract.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
# Env hygiene (TASK-019): herder-spawned agents export HERDER_BIN/AI_CONFIG_ROOT
# pointing at the spawner's checkout — honoring them silently drives another
# tree's wrapper/sources. Ignore the binary override; pin the root to THIS tree.
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"
GOLDENS="$TESTS_DIR/goldens/enroll"
HERDER=("$REPO_ROOT/bin/herder")
HEN=("$REPO_ROOT/bin/herder" enroll)
[[ -n "${HERDER_ENROLL_BIN:-}" ]] && HEN=("$HERDER_ENROLL_BIN")

WRITE=0
[[ "${1:-}" == "--write" ]] && WRITE=1

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN" "$GOLDENS"
trap 'rm -rf "$ROOT"' EXIT

cat >"$MOCKBIN/herdr" <<'MOCK_HERDR'
#!/usr/bin/env bash
set -euo pipefail
if [[ -n "${MOCK_HERDR_CALLS:-}" ]]; then
  printf '%s %s\n' "${1:-}" "${2:-}" >>"$MOCK_HERDR_CALLS"
fi
case "${1:-} ${2:-}" in
  "pane get")
    jq -n '{result:{pane:{pane_id:"p_self", terminal_id:"term_SELF", workspace_id:"ws_self", cwd:"/mock/cwd"}}}';;
  "agent list")
    jq -n '{result:{agents:[{pane_id:"p_self", terminal_id:"term_SELF", agent:"claude", agent_status:"idle"}]}}';;
  *)
    printf 'mock herdr (enroll suite): unhandled: %s\n' "$*" >&2
    exit 64;;
esac
MOCK_HERDR
chmod +x "$MOCKBIN/herdr"

cat >"$MOCKBIN/hcom" <<'MOCK_HCOM'
#!/usr/bin/env bash
set -euo pipefail
if [[ -n "${MOCK_HCOM_CALLS:-}" ]]; then
  printf '%s %s\n' "${1:-}" "${2:-}" >>"$MOCK_HCOM_CALLS"
fi
rows="${MOCK_HCOM_ROWS:-[]}"
if [[ "${1:-} ${2:-}" == "list --json" ]]; then
  printf '%s\n' "$rows"
  exit 0
fi
if [[ "${1:-}" == "list" && -n "${2:-}" ]]; then
  jq -e --arg name "$2" 'map(select(.name==$name and (.joined // true))) | length == 1' <<<"$rows" >/dev/null
  exit
fi
exit 64
MOCK_HCOM
chmod +x "$MOCKBIN/hcom"

PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"
fail=0

run_case() {
  local name="$1"; shift
  CASE="$ROOT/$name"
  mkdir -p "$CASE/home" "$CASE/state"
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self \
    HERDER_STATE_DIR="$CASE/state" \
    "$@" 2>"$RUN_ERR_F")"
  RUN_RC=$?
}

block_for() {
  local block guid short
  block="$(printf '=== STDERR ===\n%s\n=== STDOUT ===\n%s\n=== EXIT ===\n%s\n=== REGISTRY ===\n%s' \
    "$(cat "$RUN_ERR_F")" "$RUN_OUT" "$RUN_RC" "$(cat "$CASE/state/registry.jsonl" 2>/dev/null)")"
  guid="$(grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' <<<"$block" | head -n1 || true)"
  if [[ -n "$guid" ]]; then
    short="${guid:0:8}"
    block="${block//$guid/<GUID>}"
    block="${block//$short/<SHORT>}"
  fi
  block="$(sed -E 's/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/<GUID>/g; s/"hostname":"[^"]*"/"hostname":"<HOST>"/g' <<<"$block")"
  block="$(sed -E 's/[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z/<TS>/g' <<<"$block")"
  printf '%s' "$block"
}

check_one() {
  local name="$1" block gold
  block="$(block_for)"
  gold="$GOLDENS/$name.txt"
  if [[ "$WRITE" -eq 1 ]]; then
    printf '%s\n' "$block" >"$gold"
    printf 'WROTE  %s\n' "$name"
    return
  fi
  if [[ ! -f "$gold" ]]; then
    printf 'MISSING GOLDEN  %s (run --write first)\n' "$name"; fail=1; return
  fi
  if diff -u "$gold" <(printf '%s\n' "$block") >/tmp/hen_diff.$$ 2>&1; then
    printf 'PASS  %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"; cat /tmp/hen_diff.$$; fail=1
  fi
  rm -f /tmp/hen_diff.$$
}

scenario_default() {
  run_case default "$@"
  check_one default
}

scenario_ambient() {
  run_case ambient env \
    HERDER_GUID=guid-existing-0000 HERDER_LABEL=env-label HERDER_ROLE=env-role \
    HCOM_INSTANCE_NAME=stale-launch-name HCOM_SESSION_ID=sess-live \
    MOCK_HCOM_ROWS='[{"name":"bus-live","session_id":"sess-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
    HCOM_DIR=/hfake/.hcom HCOM_TAG=env-role HCOM_LAUNCH_BATCH_ID=batch-9 \
    "$@"
  check_one ambient
}

scenario_reenroll_spawned() {
  CASE="$ROOT/reenroll_spawned"
  mkdir -p "$CASE/home" "$CASE/state"
  cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-spawned-0000","short_guid":"guid","label":"spawned-old","role":"worker","agent":"codex","terminal_id":"term_OLD","pane_id":"p_old","status":"active","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-spawn","tag":"worker","batch_id":"","cwd":"/old","workspace_id":"ws_old","branch":"","ts":"2026-07-03T00:00:00Z"}}
JSONL
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self \
    HERDER_STATE_DIR="$CASE/state" \
    HERDER_GUID=guid-spawned-0000 HERDER_ROLE=worker \
    HCOM_SESSION_ID=sess-spawn \
    MOCK_HCOM_ROWS='[{"name":"spawned-live","session_id":"sess-spawn","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
    "${HEN[@]}" --label spawned-new --json 2>"$RUN_ERR_F")"
  RUN_RC=$?
  check_one reenroll_spawned
}

# TASK-035 AC#1: a reused pane accumulates a stale manual identity per prior
# session; re-enrolling that pane must UNSEAT the prior seated sessions for
# it so a dead session's row stops lingering as LIVE=working. Seed two stale
# manual rows on p_self (the pane the mock reports) plus one on a DIFFERENT
# pane that must be left untouched; enroll fresh and assert both p_self rows
# gain an unseated record while the other-pane session stays seated.
scenario_reenroll_reused_pane() {
  CASE="$ROOT/reenroll_reused_pane"
  mkdir -p "$CASE/home" "$CASE/state"
  cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-01T00:00:00Z"}
{"kind":"session","guid":"guid-stale1-000","event":"seated","recorded_at":"2026-07-01T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"stale-a","role":"manual","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stale-a-bus","hcom_verified":true},"provenance":{"mechanism":"enroll","tag":"manual","cwd":"/mock/cwd","workspace_id":"ws_self"}}
{"kind":"session","guid":"guid-stale2-000","event":"seated","recorded_at":"2026-07-01T00:00:02Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"stale-b","role":"manual","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stale-b-bus","hcom_verified":true},"provenance":{"mechanism":"enroll","tag":"manual","cwd":"/mock/cwd","workspace_id":"ws_self"}}
{"kind":"session","guid":"guid-other-pane0","event":"seated","recorded_at":"2026-07-01T00:00:03Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"other-pane","role":"manual","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_OTHER","pane_id":"p_other","hcom_name":"other-bus","hcom_verified":true}}
JSONL
  printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self \
    HERDER_STATE_DIR="$CASE/state" \
    HERDER_GUID=guid-fresh-0000 \
    MOCK_HCOM_ROWS='[{"name":"fresh-bus","session_id":"sid-fresh","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
    "${HEN[@]}" --label fresh-session --json 2>"$RUN_ERR_F")"
  RUN_RC=$?
  check_one reenroll_reused_pane
}

# TASK-035 P1-b: herdr COMPACTS pane ids, so a still-live session that moved can
# keep an old pane_id that a NEW unrelated session now reuses. Retirement keys on
# pane_id AND the durable terminal_id — a prior row whose terminal_id differs from
# the enrolling pane's is a different session and must NOT be unseated. Seed a row
# on p_self but terminal_id=term_ELSEWHERE; enroll (pane terminal=term_SELF) and
# assert that session is left SEATED (no unseated record) while a same-terminal stale
# row IS retired.
scenario_reenroll_compacted_pane() {
  CASE="$ROOT/reenroll_compacted_pane"
  mkdir -p "$CASE/home" "$CASE/state"
  cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-live-else0","short_guid":"livels","label":"live-elsewhere","role":"manual","agent":"claude","terminal_id":"term_ELSEWHERE","pane_id":"p_self","hcom_name":"live-else-bus","status":"active","provenance":{"mechanism":"enroll","spawned_by":"user","tool_session_id":"","tag":"manual","batch_id":"","cwd":"/mock/cwd","workspace_id":"ws_self","branch":"","ts":"2026-07-01T00:00:00Z"}}
{"guid":"guid-stalehere0","short_guid":"staleh","label":"stale-here","role":"manual","agent":"claude","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stale-here-bus","status":"active","provenance":{"mechanism":"enroll","spawned_by":"user","tool_session_id":"","tag":"manual","batch_id":"","cwd":"/mock/cwd","workspace_id":"ws_self","branch":"","ts":"2026-07-02T00:00:00Z"}}
JSONL
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self \
    HERDER_STATE_DIR="$CASE/state" \
    HERDER_GUID=guid-fresh-0000 \
    "${HEN[@]}" --label fresh-session --json 2>"$RUN_ERR_F")"
  RUN_RC=$?
  check_one reenroll_compacted_pane
}

seed_v2_case() {
  CASE="$ROOT/$1"
  mkdir -p "$CASE/home" "$CASE/state"
  printf '%s\n' '{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-12T00:00:00Z"}' >"$CASE/state/registry.jsonl"
  printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
}

scenario_help() {
  run_case help "${HEN[@]}" --help
  check_one help
}

scenario_refuse_force_fresh_core() {
  seed_v2_case refuse_force_fresh_core
  cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"session","guid":"guid-source-0000","event":"seated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"stable","role":"worker","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stable-bus","hcom_verified":true},"sids":[{"sid":"sid-live","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"spawn","tool_session_id":"sid-live"}}
JSONL
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self HCOM_SESSION_ID=sid-live \
    MOCK_HCOM_ROWS='[{"name":"stable-bus","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
    "${HERDER[@]}" adopt stable --confirm-dead 2>"$RUN_ERR_F")"
  RUN_RC=$?
  check_one refuse_force_fresh_core
}

scenario_refuse_unknown_guid_core() {
  seed_v2_case refuse_unknown_guid_core
  cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"session","guid":"guid-existing-0000","event":"seated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"stable","role":"worker","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stable-bus","hcom_verified":true}}
JSONL
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-unknown-0000 \
    MOCK_HCOM_ROWS='[{"name":"stable-bus","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
    "${HEN[@]}" --label stable 2>"$RUN_ERR_F")"
  RUN_RC=$?
  check_one refuse_unknown_guid_core
}

scenario_refuse_unverified_occupied_seat() {
  seed_v2_case refuse_unverified_occupied_seat
  cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"session","guid":"guid-existing-0000","event":"seated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"stable","role":"worker","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stable-bus","hcom_verified":false}}
JSONL
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self MOCK_HCOM_ROWS='[]' \
    "${HEN[@]}" --label replacement 2>"$RUN_ERR_F")"
  RUN_RC=$?
  check_one refuse_unverified_occupied_seat
}

scenario_refuse_duplicate_sid_batch() {
  seed_v2_case refuse_duplicate_sid_batch
  cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"session","guid":"guid-original-0000","event":"seated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"stable","role":"worker","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stable-bus","hcom_verified":true},"sids":[{"sid":"sid-live","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"spawn","tool_session_id":"sid-live"}}
{"kind":"session","guid":"guid-conflict-000","event":"seated","recorded_at":"2026-07-12T00:00:02Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"copy","role":"manual","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stable-bus","hcom_verified":true},"sids":[{"sid":"sid-other","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"enroll","tool_session_id":"sid-other"}}
JSONL
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-original-0000 HCOM_SESSION_ID=sid-live \
    MOCK_HCOM_ROWS='[{"name":"stable-bus","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
    "${HEN[@]}" --label stable 2>"$RUN_ERR_F")"
  RUN_RC=$?
  check_one refuse_duplicate_sid_batch
}

scenario_refuse_select_sid_conflict() {
  seed_v2_case refuse_select_sid_conflict
  cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"session","guid":"guid-existing-0000","event":"seated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"stable","role":"worker","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stable-bus","hcom_verified":true},"sids":[{"sid":"sid-other","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"spawn","tool_session_id":"sid-other"}}
JSONL
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self HCOM_SESSION_ID=sid-live \
    MOCK_HCOM_ROWS='[{"name":"stable-bus","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
    "${HEN[@]}" 2>"$RUN_ERR_F")"
  RUN_RC=$?
  check_one refuse_select_sid_conflict
}

scenario_refuse_select_ambiguous() {
  seed_v2_case refuse_select_ambiguous
  cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"session","guid":"guid-first-0000","event":"seated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"first","role":"worker","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stable-bus","hcom_verified":true}}
{"kind":"session","guid":"guid-second-000","event":"seated","recorded_at":"2026-07-12T00:00:02Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"second","role":"worker","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stable-bus","hcom_verified":true}}
JSONL
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self HCOM_SESSION_ID=sid-live \
    MOCK_HCOM_ROWS='[{"name":"stable-bus","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
    "${HEN[@]}" 2>"$RUN_ERR_F")"
  RUN_RC=$?
  check_one refuse_select_ambiguous
}

scenario_default "${HEN[@]}" --json
scenario_ambient "${HEN[@]}" --label cli-label --role cli-role --json
scenario_reenroll_spawned
scenario_reenroll_reused_pane
scenario_reenroll_compacted_pane
scenario_help
scenario_refuse_force_fresh_core
scenario_refuse_unknown_guid_core
scenario_refuse_unverified_occupied_seat
scenario_refuse_duplicate_sid_batch
scenario_refuse_select_sid_conflict
scenario_refuse_select_ambiguous

if [[ "$WRITE" -eq 0 ]]; then
	HELP_OUT="$("${HEN[@]}" --help 2>/dev/null | tr '\n' ' ')"
	if grep -q 'stored bus name' <<<"$HELP_OUT" \
	  && grep -q 'exact recorded/live session id match' <<<"$HELP_OUT" \
	  && grep -q 'unchanged terminal and caller-claimed label' <<<"$HELP_OUT" \
	  && grep -q 'may bootstrap it' <<<"$HELP_OUT" \
	  && grep -q 'captures its live name and session id' <<<"$HELP_OUT"; then
		printf 'PASS  help: guid reuse states the exact ownership proof\n'
	else
		printf 'FAIL  help: guid reuse does not state stored-bus AND (session OR terminal+label) proof\n'; fail=1
	fi

	check_pinned_takeover_refusal() {
		local name="$1" stored_bus_fields="$2"
		CASE="$ROOT/$name"
		mkdir -p "$CASE/home" "$CASE/state"
		cat >"$CASE/state/registry.jsonl" <<JSONL
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-12T00:00:00Z"}
{"kind":"session","guid":"guid-existing-0000","event":"seated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"victim-label","role":"reviewer","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_old"$stored_bus_fields},"sids":[{"sid":"sid-victim","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"spawn","tool_session_id":"sid-victim"}}
JSONL
		printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
		cp "$CASE/state/registry.jsonl" "$CASE/before.jsonl"
		RUN_ERR_F="$CASE/stderr"
		HERDR_CALLS="$CASE/herdr.calls"
		HCOM_CALLS="$CASE/hcom.calls"
		RUN_OUT="$(env -i \
		  PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
		  HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-existing-0000 \
		  HCOM_SESSION_ID=sid-attacker \
		  MOCK_HERDR_CALLS="$HERDR_CALLS" MOCK_HCOM_CALLS="$HCOM_CALLS" \
		  MOCK_HCOM_ROWS='[{"name":"attacker-bus","session_id":"sid-attacker","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
		  "${HEN[@]}" --json 2>"$RUN_ERR_F")"
		RUN_RC=$?
		if [[ "$RUN_RC" -eq 1 ]] \
		  && cmp -s "$CASE/before.jsonl" "$CASE/state/registry.jsonl" \
		  && grep -q 'bootstrap ownership proof failed' "$RUN_ERR_F" \
		  && grep -q 'requested label "manual-guid" does not match recorded label "victim-label"' "$RUN_ERR_F" \
		  && [[ "$(cat "$HERDR_CALLS")" == "pane get" ]] \
		  && [[ "$(cat "$HCOM_CALLS")" == "list --json" ]]; then
			printf 'PASS  guard: %s refuses a foreign pinned caller without mutation\n' "$name"
		else
			printf 'FAIL  guard: %s foreign pinned caller — rc=%s err=%s out=%s herdr_calls=%q hcom_calls=%q\n' \
			  "$name" "$RUN_RC" "$(cat "$RUN_ERR_F")" "$RUN_OUT" "$(cat "$HERDR_CALLS" 2>/dev/null)" "$(cat "$HCOM_CALLS" 2>/dev/null)"; fail=1
		fi
	}

	check_pinned_takeover_refusal pinned-no-stored-bus ''
	check_pinned_takeover_refusal pinned-unverified-stored-bus ',"hcom_name":"stored-bus","hcom_verified":false'

	CASE="$ROOT/pinned-ambient-label-proof"
	mkdir -p "$CASE/home" "$CASE/state"
	cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-12T00:00:00Z"}
{"kind":"session","guid":"guid-existing-0000","event":"seated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"worker-name","role":"worker","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_old","hcom_name":"worker-name","hcom_verified":true},"sids":[{"sid":"sid-before","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"spawn","tool_session_id":"sid-before"}}
JSONL
	printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
	RUN_ERR_F="$CASE/stderr"
	HERDR_CALLS="$CASE/herdr.calls"
	HCOM_CALLS="$CASE/hcom.calls"
	RUN_OUT="$(env -i \
	  PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
	  HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-existing-0000 \
	  HERDER_LABEL=worker-name HERDER_ROLE=worker HCOM_SESSION_ID=sid-after \
	  MOCK_HERDR_CALLS="$HERDR_CALLS" MOCK_HCOM_CALLS="$HCOM_CALLS" \
	  MOCK_HCOM_ROWS='[{"name":"worker-name","session_id":"sid-after","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
	  "${HEN[@]}" --json 2>"$RUN_ERR_F")"
	RUN_RC=$?
	if [[ "$RUN_RC" -eq 0 ]] \
	  && tail -n1 "$CASE/state/registry.jsonl" | jq -e '
		.guid == "guid-existing-0000" and .label == "worker-name" and .role == "worker" and
		.provenance.tool_session_id == "sid-after" and .sids[-1].sid == "sid-after"
	  ' >/dev/null \
	  && [[ "$(cat "$HERDR_CALLS")" == "pane get" ]] \
	  && [[ "$(cat "$HCOM_CALLS")" == "list --json" ]]; then
		printf 'PASS  repair: ambient label proves pinned ownership while stored identity survives\n'
	else
		printf 'FAIL  repair: ambient label ownership proof — rc=%s err=%s out=%s herdr_calls=%q hcom_calls=%q\n' \
		  "$RUN_RC" "$(cat "$RUN_ERR_F")" "$RUN_OUT" "$(cat "$HERDR_CALLS" 2>/dev/null)" "$(cat "$HCOM_CALLS" 2>/dev/null)"; fail=1
	fi

	CASE="$ROOT/repair-preserves-identity"
	mkdir -p "$CASE/home" "$CASE/state"
	cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-12T00:00:00Z"}
{"kind":"session","guid":"guid-existing-0000","event":"seated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"renamed-stable","role":"designer","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stable-bus","hcom_verified":true},"sids":[{"sid":"sid-live","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"spawn","tool_session_id":"sid-live"}}
JSONL
	printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
	RUN_ERR_F="$CASE/stderr"
	HERDR_CALLS="$CASE/herdr.calls"
	HCOM_CALLS="$CASE/hcom.calls"
	RUN_OUT="$(env -i \
	  PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
	  HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-existing-0000 \
	  HERDER_LABEL=birth-label HERDER_ROLE=manual HCOM_SESSION_ID=sid-live \
	  MOCK_HERDR_CALLS="$HERDR_CALLS" MOCK_HCOM_CALLS="$HCOM_CALLS" \
	  MOCK_HCOM_ROWS='[{"name":"stable-bus","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
	  "${HEN[@]}" --json 2>"$RUN_ERR_F")"
	RUN_RC=$?
	if [[ "$RUN_RC" -eq 0 ]] \
	  && tail -n1 "$CASE/state/registry.jsonl" | jq -e '
		.guid == "guid-existing-0000" and .label == "renamed-stable" and .role == "designer"
	  ' >/dev/null \
	  && [[ "$(cat "$HERDR_CALLS")" == "pane get" ]] \
	  && [[ "$(cat "$HCOM_CALLS")" == "list --json" ]]; then
		printf 'PASS  repair: stored label and role beat ambient launch defaults\n'
	else
		printf 'FAIL  repair: stored identity preservation — rc=%s err=%s out=%s herdr_calls=%q hcom_calls=%q\n' \
		  "$RUN_RC" "$(cat "$RUN_ERR_F")" "$RUN_OUT" "$(cat "$HERDR_CALLS" 2>/dev/null)" "$(cat "$HCOM_CALLS" 2>/dev/null)"; fail=1
	fi

	CASE="$ROOT/repair-explicit-identity"
	mkdir -p "$CASE/home" "$CASE/state"
	cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-12T00:00:00Z"}
{"kind":"session","guid":"guid-existing-0000","event":"seated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"stored-label","role":"designer","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stable-bus","hcom_verified":true},"sids":[{"sid":"sid-live","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"spawn","tool_session_id":"sid-live"}}
JSONL
	printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
	RUN_ERR_F="$CASE/stderr"
	RUN_OUT="$(env -i \
	  PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
	  HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-existing-0000 \
	  HERDER_LABEL=birth-label HERDER_ROLE=manual HCOM_SESSION_ID=sid-live \
	  MOCK_HCOM_ROWS='[{"name":"stable-bus","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
	  "${HEN[@]}" --label chosen-label --role reviewer --json 2>"$RUN_ERR_F")"
	RUN_RC=$?
	if [[ "$RUN_RC" -eq 0 ]] && tail -n1 "$CASE/state/registry.jsonl" | jq -e '
	  .guid == "guid-existing-0000" and .label == "chosen-label" and .role == "reviewer"
	' >/dev/null; then
		printf 'PASS  repair: explicit label and role override stored identity\n'
	else
		printf 'FAIL  repair: explicit identity override — rc=%s err=%s out=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")" "$RUN_OUT"; fail=1
	fi

	CASE="$ROOT/repair-empty-role-fallback"
	mkdir -p "$CASE/home" "$CASE/state"
	cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-12T00:00:00Z"}
{"kind":"session","guid":"guid-existing-0000","event":"seated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"stable","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stable-bus","hcom_verified":true},"sids":[{"sid":"sid-live","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"spawn","tool_session_id":"sid-live"}}
JSONL
	printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
	RUN_ERR_F="$CASE/stderr"
	RUN_OUT="$(env -i \
	  PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
	  HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-existing-0000 \
	  HERDER_LABEL=birth-label HERDER_ROLE=operator HCOM_SESSION_ID=sid-live \
	  MOCK_HCOM_ROWS='[{"name":"stable-bus","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
	  "${HEN[@]}" --json 2>"$RUN_ERR_F")"
	RUN_RC=$?
	if [[ "$RUN_RC" -eq 0 ]] && tail -n1 "$CASE/state/registry.jsonl" | jq -e '
	  .guid == "guid-existing-0000" and .label == "stable" and .role == "operator"
	' >/dev/null; then
		printf 'PASS  repair: empty stored role falls back to the ambient default\n'
	else
		printf 'FAIL  repair: empty stored role fallback — rc=%s err=%s out=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")" "$RUN_OUT"; fail=1
	fi

	CASE="$ROOT/stale-sid-full-seat"
	mkdir -p "$CASE/home" "$CASE/state"
	cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-12T00:00:00Z"}
{"kind":"session","guid":"guid-existing-0000","event":"seated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"stable","role":"worker","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stable-bus","hcom_verified":true,"namespace":"/hfake/.hcom"},"sids":[{"sid":"sid-recorded","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"clear","spawned_by":"user","tool_session_id":"sid-recorded","tag":"worker","cwd":"/old","workspace_id":"ws_self","ts":"2026-07-12T00:00:01Z"}}
JSONL
	printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
	RUN_ERR_F="$CASE/stderr"
	RUN_OUT="$(env -i \
	  PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
	  HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-existing-0000 HERDER_ROLE=worker \
	  HCOM_SESSION_ID=sid-live HCOM_DIR=/hfake/.hcom HCOM_TAG=worker \
	  MOCK_HCOM_ROWS='[{"name":"stable-bus","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
	  "${HEN[@]}" --label stable --json 2>"$RUN_ERR_F")"
	RUN_RC=$?
	if [[ "$RUN_RC" -eq 0 ]] \
	  && jq -s -e '
		[.[] | select(.kind=="session") | .guid] | unique | length == 1
	  ' "$CASE/state/registry.jsonl" >/dev/null \
	  && tail -n1 "$CASE/state/registry.jsonl" | jq -e '
		.guid == "guid-existing-0000" and .state == "seated" and .label == "stable" and
		.seat.terminal_id == "term_SELF" and .seat.pane_id == "p_self" and
		.seat.hcom_name == "stable-bus" and .seat.hcom_verified == true and
		.provenance.mechanism == "clear" and .provenance.tool_session_id == "sid-live" and
		(.provenance | has("spawned_by") and has("tag") and has("cwd") and has("workspace_id") and has("ts")) and
		.continuity == "confirmed" and (.sids | length == 1 and .[0].sid == "sid-live" and .[0].source == "harvest")
	  ' >/dev/null; then
		printf 'PASS  repair: stale sid accepts full seated proof and records the live sid\n'
	else
		printf 'FAIL  repair: stale sid full-seat proof — rc=%s err=%s out=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")" "$RUN_OUT"; fail=1
	fi

	CASE="$ROOT/preserve-sid-with-empty-live-sid"
	mkdir -p "$CASE/home" "$CASE/state"
	cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-12T00:00:00Z"}
{"kind":"session","guid":"guid-existing-0000","event":"seated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"stable","role":"worker","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stable-bus","hcom_verified":true},"sids":[{"sid":"sid-durable","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"clear","tool_session_id":"sid-durable"}}
{"kind":"session","guid":"guid-existing-0000","event":"reconciled","recorded_at":"2026-07-12T00:00:02Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"stable","role":"worker","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stable-bus","hcom_verified":true},"provenance":{"mechanism":"clear"}}
JSONL
	printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
	RUN_ERR_F="$CASE/enroll.stderr"
	RUN_OUT="$(env -i \
	  PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
	  HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-existing-0000 HERDER_ROLE=worker \
	  MOCK_HCOM_ROWS='[{"name":"stable-bus","session_id":"","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
	  "${HEN[@]}" --label stable --json 2>"$RUN_ERR_F")"
	RUN_RC=$?
	if [[ "$RUN_RC" -eq 0 ]] && tail -n1 "$CASE/state/registry.jsonl" | jq -e '
	  .provenance.tool_session_id == "sid-durable" and
	  (.sids | length == 1 and .[0].sid == "sid-durable")
	' >/dev/null; then
		printf 'PASS  repair: empty live sid preserves the durable recorded sid\n'
	else
		printf 'FAIL  repair: empty live sid preservation — rc=%s err=%s out=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")" "$RUN_OUT"; fail=1
	fi
	RUN_ERR_F="$CASE/resolve.stderr"
	RUN_OUT="$(cd "$CASE" && env -i \
	  PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
	  HERDR_ENV=1 HERDR_PANE_ID=p_self HCOM_SESSION_ID=sid-durable \
	  MOCK_HCOM_ROWS='[]' \
	  "$REPO_ROOT/bin/herder" compact --dry-run --stop 2>"$RUN_ERR_F")"
	RUN_RC=$?
	if [[ "$RUN_RC" -eq 0 ]] && grep -q 'guid guid-existing-0000, resolution: durable-key' "$RUN_ERR_F"; then
		printf 'PASS  repair: preserved sid still resolves the re-enrolled row\n'
	else
		printf 'FAIL  repair: preserved sid resolution — rc=%s err=%s out=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")" "$RUN_OUT"; fail=1
	fi

	CASE="$ROOT/bootstrap-empty-stored-bus"
	mkdir -p "$CASE/home" "$CASE/state"
	cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-12T00:00:00Z"}
{"kind":"session","guid":"guid-existing-0000","event":"seated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"stable","role":"worker","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_verified":false},"provenance":{"mechanism":"clear"}}
JSONL
	printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
	RUN_ERR_F="$CASE/bootstrap.stderr"
	RUN_OUT="$(env -i \
	  PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
	  HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-existing-0000 HERDER_ROLE=worker \
	  HCOM_SESSION_ID=sid-live \
	  MOCK_HCOM_ROWS='[{"name":"bootstrap-bus","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
	  "${HEN[@]}" --label stable --json 2>"$RUN_ERR_F")"
	RUN_RC=$?
	if [[ "$RUN_RC" -eq 0 ]] && tail -n1 "$CASE/state/registry.jsonl" | jq -e '
	  .guid == "guid-existing-0000" and .seat.hcom_name == "bootstrap-bus" and
	  .seat.hcom_verified == true and .provenance.tool_session_id == "sid-live" and
	  (.sids | length == 1 and .[0].sid == "sid-live")
	' >/dev/null; then
		printf 'PASS  bootstrap: empty stored bus captures verified live name and sid\n'
	else
		printf 'FAIL  bootstrap: empty stored bus capture — rc=%s err=%s out=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")" "$RUN_OUT"; fail=1
	fi
	cp "$CASE/state/registry.jsonl" "$CASE/after-bootstrap.jsonl"
	RUN_ERR_F="$CASE/rekey.stderr"
	RUN_OUT="$(env -i \
	  PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
	  HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-existing-0000 HERDER_ROLE=worker \
	  HCOM_SESSION_ID=sid-live \
	  MOCK_HCOM_ROWS='[{"name":"different-bus","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
	  "${HEN[@]}" --label stable 2>"$RUN_ERR_F")"
	RUN_RC=$?
	if [[ "$RUN_RC" -eq 1 ]] \
	  && cmp -s "$CASE/after-bootstrap.jsonl" "$CASE/state/registry.jsonl" \
	  && grep -q 'different-bus' "$RUN_ERR_F" \
	  && grep -q 'stored bus name @bootstrap-bus' "$RUN_ERR_F"; then
		printf 'PASS  bootstrap: captured binding makes a different live name refuse\n'
	else
		printf 'FAIL  bootstrap: captured binding replay guard — rc=%s err=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")"; fail=1
	fi

	CASE="$ROOT/bootstrap-unverified-stored-bus"
	mkdir -p "$CASE/home" "$CASE/state"
	cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-12T00:00:00Z"}
{"kind":"session","guid":"guid-existing-0000","event":"reconciled","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"stable","role":"worker","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stored-bus","hcom_verified":false},"sids":[{"sid":"sid-live","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"spawn","tool_session_id":"sid-live"}}
JSONL
	printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
	RUN_ERR_F="$CASE/stderr"
	RUN_OUT="$(env -i \
	  PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
	  HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-existing-0000 HERDER_ROLE=worker \
	  HCOM_SESSION_ID=sid-live \
	  MOCK_HCOM_ROWS='[{"name":"live-bus","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
	  "${HEN[@]}" --label stable --json 2>"$RUN_ERR_F")"
	RUN_RC=$?
	if [[ "$RUN_RC" -eq 0 ]] \
	  && jq -s -e '[.[] | select(.kind=="session") | .guid] | unique == ["guid-existing-0000"]' "$CASE/state/registry.jsonl" >/dev/null \
	  && tail -n1 "$CASE/state/registry.jsonl" | jq -e '
		.guid == "guid-existing-0000" and .event == "seated" and
		.seat.hcom_name == "live-bus" and .seat.hcom_verified == true and
		.provenance.tool_session_id == "sid-live"
	  ' >/dev/null; then
		printf 'PASS  bootstrap: explicit unverified stored name captures verified live identity\n'
	else
		printf 'FAIL  bootstrap: explicit unverified stored name repair — rc=%s err=%s out=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")" "$RUN_OUT"; fail=1
	fi

	CASE="$ROOT/core-rebind-preserves-identity"
	mkdir -p "$CASE/home" "$CASE/state"
	cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-12T00:00:00Z"}
{"kind":"session","guid":"guid-existing-0000","event":"reconciled","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"renamed-stable","role":"designer","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stable-bus","hcom_verified":false},"sids":[{"sid":"sid-live","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"spawn","tool_session_id":"sid-live"}}
JSONL
	printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
	RUN_ERR_F="$CASE/stderr"
	RUN_OUT="$(env -i \
	  PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
	  HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_LABEL=birth-label HERDER_ROLE=manual HCOM_SESSION_ID=sid-live \
	  MOCK_HCOM_ROWS='[{"name":"stable-bus","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
	  "${HEN[@]}" --json 2>"$RUN_ERR_F")"
	RUN_RC=$?
	if [[ "$RUN_RC" -eq 0 ]] \
	  && jq -s -e '[.[] | select(.kind=="session") | .guid] | unique == ["guid-existing-0000"]' "$CASE/state/registry.jsonl" >/dev/null \
	  && tail -n1 "$CASE/state/registry.jsonl" | jq -e '
		.guid == "guid-existing-0000" and .state == "seated" and
		.label == "renamed-stable" and .role == "designer" and
		.seat.hcom_name == "stable-bus" and .seat.hcom_verified == true
	  ' >/dev/null; then
		printf 'PASS  rebind: core match repairs without minting and preserves stored identity\n'
	else
		printf 'FAIL  rebind: core match identity preservation — rc=%s err=%s out=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")" "$RUN_OUT"; fail=1
	fi

	CASE="$ROOT/duplicate-seat-cleanup"
	mkdir -p "$CASE/home" "$CASE/state"
	cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-12T00:00:00Z"}
{"kind":"session","guid":"guid-original-0000","event":"reconciled","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"stable","role":"worker","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stable-bus"},"sids":[{"sid":"sid-live","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"spawn","tool_session_id":"sid-live"}}
{"kind":"session","guid":"guid-duplicate-000","event":"seated","recorded_at":"2026-07-12T00:00:02Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"manual-copy","role":"manual","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"stable-bus","hcom_verified":true},"sids":[{"sid":"sid-live","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"enroll","tool_session_id":"sid-live"}}
JSONL
	printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
	RUN_ERR_F="$CASE/stderr"
	HERDR_CALLS="$CASE/herdr.calls"
	HCOM_CALLS="$CASE/hcom.calls"
	RUN_OUT="$(env -i \
	  PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
	  HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-original-0000 HERDER_ROLE=worker \
	  HCOM_SESSION_ID=sid-live \
	  MOCK_HERDR_CALLS="$HERDR_CALLS" MOCK_HCOM_CALLS="$HCOM_CALLS" \
	  MOCK_HCOM_ROWS='[{"name":"stable-bus","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
	  "${HEN[@]}" --label stable --json 2>"$RUN_ERR_F")"
	RUN_RC=$?
	if [[ "$RUN_RC" -eq 0 ]] \
	  && tail -n2 "$CASE/state/registry.jsonl" | jq -s -e '
		.[0].guid == "guid-original-0000" and .[0].event == "seated" and
		.[0].seat.hcom_verified == true and
		.[1].guid == "guid-duplicate-000" and .[1].event == "unseated" and
		.[1].state == "unseated" and (.[1] | has("seat") | not) and
		.[1].close_result == "duplicate_detached" and
		(.[1].close_reason | startswith("source=enroll-repair; shared live seat retained by repaired guid "))
	  ' >/dev/null \
	  && jq -s -e '
		reduce (.[] | select(.kind=="session")) as $row ({}; .[$row.guid]=$row)
		| [.[] | select(.state=="seated" and .seat.terminal_id=="term_SELF" and .seat.pane_id=="p_self" and .seat.hcom_name=="stable-bus")] | length == 1
	  ' "$CASE/state/registry.jsonl" >/dev/null \
	  && [[ "$(cat "$HERDR_CALLS")" == "pane get" ]] \
	  && [[ "$(cat "$HCOM_CALLS")" == "list --json" ]]; then
		printf 'PASS  cleanup: original repairs before duplicate detaches without closing the pane\n'
	else
		printf 'FAIL  cleanup: repair-first duplicate detach — rc=%s err=%s out=%s herdr_calls=%q hcom_calls=%q\n' \
		  "$RUN_RC" "$(cat "$RUN_ERR_F")" "$RUN_OUT" "$(cat "$HERDR_CALLS" 2>/dev/null)" "$(cat "$HCOM_CALLS" 2>/dev/null)"; fail=1
	fi

	check_guid_reuse_refusal() {
		local name="$1" terminal="$2" label="$3" rows="$4"
		CASE="$ROOT/$name"
		mkdir -p "$CASE/home" "$CASE/state"
		cat >"$CASE/state/registry.jsonl" <<JSONL
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-12T00:00:00Z"}
{"kind":"session","guid":"guid-existing-0000","event":"seated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"stable","role":"worker","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"$terminal","pane_id":"p_self","hcom_name":"stable-bus","hcom_verified":true},"sids":[{"sid":"sid-recorded","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"clear","tool_session_id":"sid-recorded"}}
JSONL
		printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
		cp "$CASE/state/registry.jsonl" "$CASE/before.jsonl"
		RUN_ERR_F="$CASE/stderr"
		RUN_OUT="$(env -i \
		  PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
		  HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-existing-0000 HERDER_ROLE=worker \
		  HCOM_SESSION_ID=sid-live MOCK_HCOM_ROWS="$rows" \
		  "${HEN[@]}" --label "$label" 2>"$RUN_ERR_F")"
		RUN_RC=$?
		if [[ "$RUN_RC" -eq 1 ]] \
		  && cmp -s "$CASE/before.jsonl" "$CASE/state/registry.jsonl" \
		  && grep -q 'refused to re-enroll' "$RUN_ERR_F" \
		  && grep -q 'retry' "$RUN_ERR_F" \
		  && ! grep -q 'under its own guid' "$RUN_ERR_F"; then
			printf 'PASS  guard: %s refuses without mutating or minting identity\n' "$name"
		else
			printf 'FAIL  guard: %s — rc=%s err=%s\n' "$name" "$RUN_RC" "$(cat "$RUN_ERR_F")"; fail=1
		fi
	}

	check_guid_reuse_refusal inherited-guid term_OTHER stable '[]'
	check_guid_reuse_refusal different-live-bus term_SELF stable '[{"name":"other-bus","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]'
	check_guid_reuse_refusal changed-label term_SELF changed '[{"name":"stable-bus","session_id":"sid-live","joined":true,"launch_context":{"pane_id":"p_self"}}]'

	check_terminal_state_refusal() {
		local state="$1"
		CASE="$ROOT/refuse-$state-guid"
		mkdir -p "$CASE/home" "$CASE/state"
		cat >"$CASE/state/registry.jsonl" <<JSONL
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-12T00:00:00Z"}
{"kind":"session","guid":"guid-terminal-0000","event":"$state","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"$state","role":"worker","tool":"claude","provenance":{"mechanism":"clear","tool_session_id":"sid-recorded"}}
JSONL
		printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
		cp "$CASE/state/registry.jsonl" "$CASE/before.jsonl"
		RUN_ERR_F="$CASE/stderr"
		RUN_OUT="$(env -i \
		  PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
		  HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-terminal-0000 HERDER_ROLE=worker \
		  HCOM_SESSION_ID=sid-recorded \
		  MOCK_HCOM_ROWS='[{"name":"terminal-bus","session_id":"sid-recorded","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
		  "${HEN[@]}" --label stable 2>"$RUN_ERR_F")"
		RUN_RC=$?
		if [[ "$RUN_RC" -eq 1 ]] \
		  && cmp -s "$CASE/before.jsonl" "$CASE/state/registry.jsonl" \
		  && grep -q "existing identity is $state" "$RUN_ERR_F" \
		  && { [[ "$state" != retired ]] || grep -q 'herder reopen guid-terminal-0000' "$RUN_ERR_F"; }; then
			printf 'PASS  guard: %s guid refuses through enroll without registry mutation\n' "$state"
		else
			printf 'FAIL  guard: %s guid real-path refusal — rc=%s err=%s\n' "$state" "$RUN_RC" "$(cat "$RUN_ERR_F")"; fail=1
		fi
	}

	check_terminal_state_refusal retired
	check_terminal_state_refusal lost

  RUN_ERR_F="$ROOT/outside-stderr"
  RUN_OUT="$(env -i PATH="$PATH_HERMETIC" HOME="$ROOT/home" HERDER_STATE_DIR="$ROOT/state" "${HEN[@]}" 2>"$RUN_ERR_F")"
  RUN_RC=$?
  if [[ "$RUN_RC" -eq 1 ]] && grep -q 'HERDR_ENV/HERDR_PANE_ID required' "$RUN_ERR_F"; then
    printf 'PASS  guard: requires herdr pane\n'
  else
    printf 'FAIL  guard: requires herdr pane — rc=%s err=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")"; fail=1
  fi

  CASE="$ROOT/collision"
  mkdir -p "$CASE/home" "$CASE/state"
  cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-12T00:00:00Z"}
{"kind":"session","guid":"guid-other-0000","event":"registered","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"taken","role":"worker","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","terminal_id":"term_OTHER","pane_id":"p_other"}}
JSONL
  printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self \
    HERDER_STATE_DIR="$CASE/state" \
    "${HEN[@]}" --label taken 2>"$RUN_ERR_F")"
  RUN_RC=$?
  if [[ "$RUN_RC" -eq 1 ]] && grep -q 'label "taken" already belongs to seated session guid-other-0000' "$RUN_ERR_F"; then
    printf 'PASS  guard: active label collision refused\n'
  else
    printf 'FAIL  guard: active label collision refused — rc=%s err=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")"; fail=1
  fi

  CASE="$ROOT/dead-collision"
  mkdir -p "$CASE/home" "$CASE/state"
  cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-12T00:00:00Z"}
{"kind":"session","guid":"guid-dormant-0000","event":"unseated","recorded_at":"2026-07-12T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"unseated","label":"taken","role":"worker","tool":"claude"}
JSONL
  printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self \
    "${HEN[@]}" --label taken 2>"$RUN_ERR_F")"
  RUN_RC=$?
  if [[ "$RUN_RC" -eq 1 ]] \
    && grep -q 'state unseated (dead/unseated)' "$RUN_ERR_F" \
    && grep -q 'herder adopt guid-dormant-0000' "$RUN_ERR_F" \
    && grep -q 'herder retire guid-dormant-0000' "$RUN_ERR_F" \
    && grep -q 'herder rename <target> taken' "$RUN_ERR_F"; then
    printf 'PASS  guard: dead label collision names state and recovery\n'
  else
    printf 'FAIL  guard: dead label collision names state and recovery — rc=%s err=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")"; fail=1
  fi

  CASE="$ROOT/guid-mismatch"
  mkdir -p "$CASE/home" "$CASE/state"
  cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-existing-0000","short_guid":"existing","label":"existing","role":"worker","agent":"claude","terminal_id":"term_OTHER","pane_id":"p_other","hcom_name":"old-bus","status":"active","provenance":{"mechanism":"enroll","tool_session_id":"sess-old"}}
JSONL
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$CASE/state" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-existing-0000 \
    HCOM_SESSION_ID=sess-new \
    MOCK_HCOM_ROWS='[{"name":"new-bus","session_id":"sess-new","joined":true,"launch_context":{"pane_id":"p_self"}}]' \
    "${HEN[@]}" --label existing 2>"$RUN_ERR_F")"
  RUN_RC=$?
  if [[ "$RUN_RC" -eq 1 ]] && grep -q 'refused to re-enroll guid-existing-0000' "$RUN_ERR_F"; then
    printf 'PASS  guard: inherited guid cannot re-key another session\n'
  else
    printf 'FAIL  guard: inherited guid cannot re-key another session — rc=%s err=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")"; fail=1
  fi
fi

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "${HEN[*]}"; exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — enroll contract matches goldens.\n'; exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'; exit 1
fi
