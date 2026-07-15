#!/usr/bin/env bash
# check-retire-reopen-contract.sh — lock retire/reopen state and label-release contracts.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN"
trap 'rm -rf "$ROOT"' EXIT

cat >"$MOCKBIN/herdr" <<'MOCK_HERDR'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-} ${2:-}" in
  "pane get")
    jq -n --arg pane "${3:-p_enroll}" '{result:{pane:{pane_id:$pane,terminal_id:"term_enroll",cwd:"/repo",workspace_id:"ws_1"}}}';;
  "agent rename")
    jq -n '{result:{type:"ok"}}';;
  "agent list")
    jq -n '{result:{agents:[]}}';;
  "pane list")
    jq -n '{result:{panes:[]}}';;
  *)
    printf 'mock herdr (retire suite): unhandled: %s\n' "$*" >&2
    exit 64;;
esac
MOCK_HERDR
chmod +x "$MOCKBIN/herdr"

cat >"$MOCKBIN/hcom" <<'MOCK_HCOM'
#!/usr/bin/env bash
set -euo pipefail
state="${MOCK_HCOM_STATE:?}"
case "${1:-} ${2:-}" in
  "list --json")
    cat "$state";;
  "start --as")
    name="${3:?}"
    pane="${HERDR_PANE_ID:-}"
    if [[ "${MOCK_HCOM_NO_CONTEXT:-}" == "1" ]]; then
      pane=""
    fi
    jq -n \
      --arg name "$name" \
      --arg sid "${HCOM_SESSION_ID:-}" \
      --arg pane "$pane" \
      '[{name:$name,session_id:$sid,joined:true,launch_context:{pane_id:$pane}}]' >"$state";;
  *)
    exit 64;;
esac
MOCK_HCOM
chmod +x "$MOCKBIN/hcom"

PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"
fail=0

new_case() {
  CASE="$ROOT/$1"
  mkdir -p "$CASE/state" "$CASE/home"
  cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-08T00:00:00Z"}
{"guid":"guid-old-0000","event":"migrated_v1","recorded_at":"2026-07-08T00:00:01Z","node":"11111111-1111-1111-1111-111111111111","state":"unseated","label":"trap","role":"worker","tool":"codex"}
{"guid":"guid-other-0000","event":"registered","recorded_at":"2026-07-08T00:00:02Z","node":"11111111-1111-1111-1111-111111111111","state":"unseated","label":"other","role":"worker","tool":"claude"}
{"guid":"guid-seated-0000","event":"registered","recorded_at":"2026-07-08T00:00:03Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"busy","role":"worker","tool":"codex","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","pane_id":"p_busy","terminal_id":"term_busy"}}
{"guid":"guid-lost-0000","event":"lost","recorded_at":"2026-07-08T00:00:04Z","node":"11111111-1111-1111-1111-111111111111","state":"lost","label":"gone","role":"worker","tool":"codex"}
JSONL
  printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
  printf '[{"name":"replacement-temp","session_id":"sess-replacement","joined":true,"launch_context":{"pane_id":"p_enroll"}}]\n' >"$CASE/hcom.json"
}

run_hr() {
  env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    HERDER_STATE_DIR="$CASE/state" \
    HERDR_ENV=1 \
    HERDR_PANE_ID=p_enroll \
    HCOM_SESSION_ID=sess-replacement \
    MOCK_HCOM_STATE="$CASE/hcom.json" \
    HCOM_INSTANCE_NAME=enrolled-bus \
    "$REPO_ROOT/bin/herder" "$@"
}

assert() {
  local name="$1"; shift
  if "$@"; then
    printf 'PASS  %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"
    fail=1
  fi
}

new_case label_release
retire_out="$(run_hr retire trap --json 2>"$CASE/retire.err")"; retire_rc=$?
assert "retire unseated exits 0" test "$retire_rc" -eq 0
assert "retire json is unlabelled" bash -c 'grep -q "\"event\":\"retired\"" <<<"$1" && ! grep -q "\"label\"" <<<"$1"' bash "$retire_out"

run_hr rename guid-old-0000 should-refuse >/dev/null 2>"$CASE/rename-retired.err"; rename_retired_rc=$?
assert "rename retired refuses with reopen guidance" bash -c 'test "$1" -ne 0 && grep -q "herder reopen guid-old-0000" "$2" && ! test -s "$3"' bash "$rename_retired_rc" "$CASE/rename-retired.err" "$CASE/probe/herdr_rename_argv"

rename_out="$(run_hr rename other trap 2>"$CASE/rename.err")"; rename_rc=$?
assert "rename reuses retired label" test "$rename_rc" -eq 0

new_case enroll_release
run_hr retire trap >/dev/null 2>"$CASE/retire.err"; retire_rc=$?
enroll_out="$(run_hr enroll --label trap --json 2>"$CASE/enroll.err")"; enroll_rc=$?
assert "enroll reuses retired label" test "$retire_rc" -eq 0 -a "$enroll_rc" -eq 0
assert "enroll wrote trap label" bash -c 'grep -q "\"label\":\"trap\"" <<<"$1"' bash "$enroll_out"

new_case noops
run_hr retire trap >/dev/null 2>"$CASE/retire1.err"; first_rc=$?
rows_before="$(grep -c '"event":"retired"' "$CASE/state/registry.jsonl")"
run_hr retire guid-old-0000 >/dev/null 2>"$CASE/retire2.err"; second_rc=$?
rows_after="$(grep -c '"event":"retired"' "$CASE/state/registry.jsonl")"
assert "retire twice succeeds once" test "$first_rc" -eq 0 -a "$second_rc" -eq 0 -a "$rows_before" -eq 1 -a "$rows_after" -eq 1

new_case refusals
run_hr retire busy >/dev/null 2>"$CASE/seated.err"; seated_rc=$?
run_hr retire gone >/dev/null 2>"$CASE/lost.err"; lost_rc=$?
run_hr rename gone should-refuse >/dev/null 2>"$CASE/rename-lost.err"; rename_lost_rc=$?
assert "retire seated refuses with cull guidance" bash -c 'test "$1" -ne 0 && grep -qi "cull first" "$2"' bash "$seated_rc" "$CASE/seated.err"
assert "retire lost refuses" bash -c 'test "$1" -ne 0 && grep -q "LOST sessions cannot be retired" "$2"' bash "$lost_rc" "$CASE/lost.err"
assert "rename lost refuses" bash -c 'test "$1" -ne 0 && grep -q "lost sessions cannot be renamed" "$2"' bash "$rename_lost_rc" "$CASE/rename-lost.err"

new_case same_pane_successor
cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-old-pane-0000","event":"registered","recorded_at":"2026-07-08T00:00:05Z","node":"11111111-1111-1111-1111-111111111111","state":"unseated","label":"old-pane","role":"worker","tool":"codex"}
{"guid":"guid-new-pane-0000","event":"registered","recorded_at":"2026-07-08T00:00:06Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"new-pane","role":"worker","tool":"codex","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","pane_id":"p_reused","terminal_id":"term_reused"}}
JSONL
run_hr retire p_reused >/dev/null 2>"$CASE/retire-pane.err"; retire_pane_rc=$?
assert "retire by pane_id hits seated successor guard" bash -c 'test "$1" -ne 0 && grep -qi "cull first" "$2" && grep -q "guid-new-pane-0000" "$2"' bash "$retire_pane_rc" "$CASE/retire-pane.err"

new_case reopen
run_hr retire trap >/dev/null 2>"$CASE/retire.err"; retire_rc=$?
reopen_out="$(run_hr reopen guid-old-0000 --json 2>"$CASE/reopen.err")"; reopen_rc=$?
run_hr reopen other >/dev/null 2>"$CASE/reopen-open.err"; reopen_open_rc=$?
rename_after_reopen_out="$(run_hr rename guid-old-0000 trap 2>"$CASE/rename-after-reopen.err")"; rename_after_reopen_rc=$?
assert "reopen retired exits 0" test "$retire_rc" -eq 0 -a "$reopen_rc" -eq 0
assert "reopen is unseated unlabelled" bash -c 'grep -q "\"event\":\"reopened\"" <<<"$1" && grep -q "\"state\":\"unseated\"" <<<"$1" && ! grep -q "\"label\"" <<<"$1"' bash "$reopen_out"
assert "reopen non-retired refuses" test "$reopen_open_rc" -ne 0
assert "reopen then rename claims freed label" bash -c 'test "$1" -eq 0 && grep -q "renamed  -> trap (guid-old-0000)" "$2" && grep -q "\"event\":\"labelled\"" "$3" && grep -q "\"label\":\"trap\"" "$3"' bash "$rename_after_reopen_rc" "$CASE/rename-after-reopen.err" "$CASE/state/registry.jsonl"

new_case adopt_happy
adopt_out="$(run_hr adopt trap 2>"$CASE/adopt.err")"; adopt_rc=$?
assert "adopt replacement exits 0" test "$adopt_rc" -eq 0
assert "adopt reports every applied leg" bash -c 'grep -q "adopt: enroll applied" "$1" && grep -q "adopt: label-transfer applied" "$1" && grep -q "adopt: retire applied" "$1" && grep -q "adopt: bus-name verified" "$1"' bash "$CASE/adopt.err"
assert "adopt mints new guid, retires old, and moves label" jq -se '
  reduce (.[] | select(.kind=="session")) as $row ({}; .[$row.guid]=$row)
  | .["guid-old-0000"].state == "retired"
    and .["guid-old-0000"].label == null
    and ([to_entries[] | select(.key != "guid-old-0000" and .value.label == "trap" and .value.state == "seated")] | length) == 1
' "$CASE/state/registry.jsonl"
assert "adopt reclaims bus name" jq -e 'length == 1 and .[0].name == "trap" and .[0].session_id == "sess-replacement"' "$CASE/hcom.json"
assert "adopt binds the replacement row after reclaim" jq -se '
  reduce (.[] | select(.kind=="session")) as $row ({}; .[$row.guid]=$row)
  | [to_entries[] | select(.key != "guid-old-0000" and .value.label == "trap")] as $replacement
  | $replacement | length == 1
    and .[0].value.seat.hcom_name == "trap"
    and .[0].value.seat.hcom_verified == true
    and .[0].value.provenance.tool_session_id == "sess-replacement"
    and .[0].value.sids[-1].sid == "sess-replacement"
' "$CASE/state/registry.jsonl"

new_case adopt_without_ambient_identity
printf '[{"name":"replacement-temp","session_id":"","joined":true,"launch_context":{}}]\n' >"$CASE/hcom.json"
env -i \
  PATH="$PATH_HERMETIC" \
  HOME="$CASE/home" \
  HERDER_STATE_DIR="$CASE/state" \
  HERDR_ENV=1 \
  HERDR_PANE_ID=p_enroll \
  MOCK_HCOM_STATE="$CASE/hcom.json" \
  MOCK_HCOM_NO_CONTEXT=1 \
  "$REPO_ROOT/bin/herder" adopt trap >/dev/null 2>"$CASE/adopt.err"
adopt_no_identity_rc=$?
assert "adopt binds a newly reclaimed name without ambient bus correlates" bash -c '
  test "$1" -eq 0 && jq -se '\''
    reduce (.[] | select(.kind=="session")) as $row ({}; .[$row.guid]=$row)
    | [to_entries[] | select(.key != "guid-old-0000" and .value.label == "trap")] as $replacement
    | $replacement | length == 1
      and .[0].value.seat.hcom_name == "trap"
      and .[0].value.seat.hcom_verified == true
  '\'' "$2"
' bash "$adopt_no_identity_rc" "$CASE/state/registry.jsonl"

new_case adopt_resumed_session
cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-old-0000","event":"unseated","recorded_at":"2026-07-08T00:00:05Z","node":"11111111-1111-1111-1111-111111111111","state":"unseated","label":"trap","role":"worker","tool":"codex","sids":[{"sid":"sess-resumed","observed_at":"2026-07-08T00:00:05Z","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"spawn","tool_session_id":"sess-resumed"}}
JSONL
printf '[{"name":"restored-live","session_id":"sess-resumed","joined":true,"launch_context":{}}]\n' >"$CASE/hcom.json"
env -i \
  PATH="$PATH_HERMETIC" \
  HOME="$CASE/home" \
  HERDER_STATE_DIR="$CASE/state" \
  HERDR_ENV=1 \
  HERDR_PANE_ID=p_enroll \
  MOCK_HCOM_STATE="$CASE/hcom.json" \
  MOCK_HCOM_NO_CONTEXT=1 \
  "$REPO_ROOT/bin/herder" adopt trap >/dev/null 2>"$CASE/adopt.err"
adopt_resumed_rc=$?
assert "adopt harvests the resumed transcript identity without minting a husk" bash -c '
  test "$1" -eq 0 &&
  jq -e '\''length == 1 and .[0].name == "restored-live" and .[0].session_id == "sess-resumed"'\'' "$2" >/dev/null &&
  jq -se '\''
    reduce (.[] | select(.kind=="session")) as $row ({}; .[$row.guid]=$row)
    | [to_entries[] | select(.key != "guid-old-0000" and .value.label == "trap")] as $replacement
    | $replacement | length == 1
      and .[0].value.seat.hcom_name == "restored-live"
      and .[0].value.seat.hcom_verified == true
      and .[0].value.provenance.tool_session_id == "sess-resumed"
      and .[0].value.sids[-1].sid == "sess-resumed"
  '\'' "$3" >/dev/null
' bash "$adopt_resumed_rc" "$CASE/hcom.json" "$CASE/state/registry.jsonl"

new_case repair_unbound
cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-unbound-0000","event":"registered","recorded_at":"2026-07-08T00:00:05Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"restored","role":"designer","tool":"claude","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","pane_id":"p_enroll","terminal_id":"term_enroll","hcom_verified":false,"namespace":"/hcom"}}
JSONL
printf '[{"name":"restored-bus","session_id":"sess-replacement","joined":true,"launch_context":{}}]\n' >"$CASE/hcom.json"
env -i \
  PATH="$PATH_HERMETIC" \
  HOME="$CASE/home" \
  HERDER_STATE_DIR="$CASE/state" \
  HERDR_ENV=1 \
  HERDR_PANE_ID=p_enroll \
  HCOM_SESSION_ID=sess-replacement \
  HERDER_GUID=guid-unbound-0000 \
  HERDER_LABEL=restored \
  HERDER_ROLE=designer \
  MOCK_HCOM_STATE="$CASE/hcom.json" \
  "$REPO_ROOT/bin/herder" enroll >/dev/null 2>"$CASE/repair.err"
repair_rc=$?
assert "pinned re-enroll repairs an existing unbound row" bash -c '
  test "$1" -eq 0 && jq -se '\''
    reduce (.[] | select(.kind=="session")) as $row ({}; .[$row.guid]=$row)
    | .["guid-unbound-0000"].seat.hcom_name == "restored-bus"
      and .["guid-unbound-0000"].seat.hcom_verified == true
      and .["guid-unbound-0000"].provenance.tool_session_id == "sess-replacement"
      and .["guid-unbound-0000"].sids[-1].sid == "sess-replacement"
      and .["guid-unbound-0000"].continuity == "confirmed"
  '\'' "$2"
' bash "$repair_rc" "$CASE/state/registry.jsonl"

new_case adopt_partial
cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-live-old-0000","event":"registered","recorded_at":"2026-07-08T00:00:05Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"live-label","role":"worker","tool":"codex","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","pane_id":"p_elsewhere","terminal_id":"term_elsewhere"}}
JSONL
adopt_partial_before="$(cksum "$CASE/state/registry.jsonl")"
run_hr adopt live-label >/dev/null 2>"$CASE/adopt-partial.err"; adopt_partial_rc=$?
adopt_partial_after="$(cksum "$CASE/state/registry.jsonl")"
assert "adopt different-pane target refuses before enrollment" bash -c 'test "$1" -ne 0 && grep -q "seated on pane p_elsewhere" "$2" && grep -q "caller occupies pane p_enroll" "$2" && grep -q "refusing before enrollment" "$2" && grep -q "herder adopt guid-live-old-0000 --confirm-dead" "$2" && ! grep -q "herder cull" "$2"' bash "$adopt_partial_rc" "$CASE/adopt-partial.err"
assert "adopt seated pre-refusal writes no replacement row" test "$adopt_partial_before" = "$adopt_partial_after"
run_hr adopt live-label --confirm-dead >/dev/null 2>"$CASE/adopt-confirmed.err"; adopt_confirmed_rc=$?
assert "adopt confirm-dead remedy runs to completion" bash -c 'test "$1" -eq 0 && grep -q "adopt: retire applied" "$2" && grep -q "adopt: bus-name verified" "$2"' bash "$adopt_confirmed_rc" "$CASE/adopt-confirmed.err"
assert "adopt confirm-dead records atomic source unseat" jq -se '
  [.[] | select(.guid=="guid-live-old-0000" and .event=="adoption_source_released" and .state=="unseated" and .close_result=="adopted" and .close_reason=="operator confirmed old transcript dead" and (.seat|not) and (.label|not))] | length == 1
' "$CASE/state/registry.jsonl"

new_case adopt_same_pane
cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-same-pane-0000","event":"registered","recorded_at":"2026-07-08T00:00:05Z","node":"11111111-1111-1111-1111-111111111111","state":"seated","label":"same-label","role":"worker","tool":"codex","seat":{"kind":"herdr","node":"11111111-1111-1111-1111-111111111111","pane_id":"p_enroll","terminal_id":"term_previous"}}
JSONL
run_hr adopt same-label >/dev/null 2>"$CASE/adopt-same.err"; adopt_same_rc=$?
assert "adopt same-pane replacement needs no flag" bash -c 'test "$1" -eq 0 && grep -q "adopt: retire applied" "$2" && grep -q "adopt: bus-name verified" "$2"' bash "$adopt_same_rc" "$CASE/adopt-same.err"
assert "adopt same-pane atomically unseats source with evidence" jq -se '
  [.[] | select(.guid=="guid-same-pane-0000" and .event=="adoption_source_released" and .state=="unseated" and .close_reason=="seat superseded by replacement process in the same pane" and (.seat|not) and (.label|not))] | length == 1
' "$CASE/state/registry.jsonl"

new_case adopt_bus_conflict
printf '%s\n' '[{"name":"replacement-temp","session_id":"sess-replacement","joined":true,"launch_context":{"pane_id":"p_enroll"}},{"name":"trap","session_id":"sess-other","joined":true,"launch_context":{"pane_id":"p_other"}}]' >"$CASE/hcom.json"
run_hr adopt trap >/dev/null 2>"$CASE/adopt-bus.err"; adopt_bus_rc=$?
assert "adopt refuses bus name held by another live session" bash -c 'test "$1" -ne 0 && grep -q "bus-name leg failed" "$2" && grep -q "held by a live different session" "$2" && grep -q "refusing to steal" "$2"' bash "$adopt_bus_rc" "$CASE/adopt-bus.err"
assert "adopt bus refusal leaves the live holder untouched" jq -e 'map(select(.name=="trap" and .session_id=="sess-other")) | length == 1' "$CASE/hcom.json"
assert "adopt bus refusal reports completed and remaining legs" bash -c 'grep -q "applied: enroll applied" "$1" && grep -q "applied: label-transfer applied" "$1" && grep -q "applied: retire applied" "$1" && grep -q "remaining manual steps" "$1" && grep -q "hcom start --as trap" "$1"' bash "$CASE/adopt-bus.err"

if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — retire/reopen/adopt contract holds.\n'
  exit 0
else
  printf '\nRETIRE/REOPEN CONTRACT FAILED — see failures above.\n'
  exit 1
fi
