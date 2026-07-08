#!/usr/bin/env bash
# check-observer-contract.sh — hermetic observer phase-1a contract.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"
HERDER=("$REPO_ROOT/bin/herder")

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN"
trap 'rm -rf "$ROOT"' EXIT

cat >"$MOCKBIN/herdr" <<'MOCK_HERDR'
#!/usr/bin/env bash
set -euo pipefail
STATE="${MOCK_HERDR_STATE:?}"
case "${1:-} ${2:-}" in
  "session snapshot")
    cat "$STATE/snapshot.json";;
  "pane process_info")
    id="${3:-}"
    if [[ -f "$STATE/proc-$id.json" ]]; then
      cat "$STATE/proc-$id.json"
    else
      jq -n '{result:{process_info:{foreground_processes:[{pid:1234,argv:["claude"],cwd:"/mock/cwd"}]}}}'
    fi;;
  "agent list")
    jq '{result:{agents:[.result.agents[]?]}}' "$STATE/snapshot.json";;
  "pane list")
    jq '{result:{panes:[.result.panes[]? | {pane_id,terminal_id}]}}' "$STATE/snapshot.json";;
  "pane get")
    jq -n '{result:{pane:{pane_id:"p_self", terminal_id:"t_self", workspace_id:"ws", cwd:"/mock/cwd"}}}';;
  *)
    printf 'mock herdr(observer): unhandled: %s\n' "$*" >&2
    exit 64;;
esac
MOCK_HERDR
chmod +x "$MOCKBIN/herdr"

cat >"$MOCKBIN/hcom" <<'MOCK_HCOM'
#!/usr/bin/env bash
set -euo pipefail
STATE="${MOCK_HCOM_STATE:?}"
case "${1:-}" in
  list)
    if [[ "${2:-}" == "--json" ]]; then
      cat "$STATE/hcom.jsonl" 2>/dev/null || true
      exit 0
    fi
    name="${2:-}"
    if grep -q "\"name\":\"$name\"" "$STATE/hcom.jsonl" 2>/dev/null; then
      jq -cn --arg n "$name" '[{name:$n,joined:true}]'
      exit 0
    fi
    exit 1;;
  *)
    printf 'mock hcom(observer): unhandled: %s\n' "$*" >&2
    exit 64;;
esac
MOCK_HCOM
chmod +x "$MOCKBIN/hcom"

PATH_HERMETIC="/home/grace/.local/share/mise/installs/go/1.26.4/bin:$MOCKBIN:/usr/bin:/bin:/usr/local/bin:$HOME/.local/bin"
fail=0

pass() { printf 'PASS  %s\n' "$1"; }
fail_case() { printf 'FAIL  %s: %s\n' "$1" "$2"; fail=1; }

case_dir() {
  CASE="$ROOT/$1"
  STATE="$CASE/state"
  HDR="$CASE/herdr"
  HCOM="$CASE/hcom"
  mkdir -p "$STATE" "$HDR" "$HCOM"
  : >"$HCOM/hcom.jsonl"
}

write_registry() {
  cat >"$STATE/registry.jsonl"
  if grep -q '"kind":"node"' "$STATE/registry.jsonl"; then
    jq -r 'select(.kind=="node") | .node_id' "$STATE/registry.jsonl" | head -n1 >"$STATE/node_id"
  fi
}

node_id='00000000-0000-4000-8000-000000000001'
node_row='{"kind":"node","event":"node_registered","node_id":"00000000-0000-4000-8000-000000000001","recorded_at":"2026-07-08T00:00:00Z"}'

session_row() {
  local guid="$1" state="$2" label="$3" term="$4" pane="$5" hcom="$6" mech="${7:-enroll}" sid="${8:-}"
  jq -cn \
    --arg guid "$guid" --arg state "$state" --arg label "$label" --arg term "$term" --arg pane "$pane" --arg hcom "$hcom" --arg mech "$mech" --arg sid "$sid" \
    '{
      kind:"session", guid:$guid, event:(if $state=="seated" then "seated" else "unseated" end),
      recorded_at:"2026-07-08T00:00:00Z", node:"00000000-0000-4000-8000-000000000001", state:$state, label:$label, role:"worker", tool:"claude",
      continuity:(if $sid=="" then "assumed" else "confirmed" end),
      provenance:{mechanism:$mech, spawned_by:"user", cwd:"/mock/cwd", ts:"2026-07-08T00:00:00Z"}
    }
    + (if $state=="seated" then {seat:{kind:"herdr",node:"00000000-0000-4000-8000-000000000001",terminal_id:$term,pane_id:$pane,hcom_name:$hcom,namespace:"ns",confirmed_at:"2026-07-08T00:00:00Z"}} else {} end)
    + (if $sid!="" then {sids:[{sid:$sid,observed_at:"2026-07-08T00:00:00Z",source:"harvest"}]} else {} end)'
}

process_row() {
  jq -cn '{
    kind:"session", guid:"guid-proc", event:"seated", recorded_at:"2026-07-08T00:00:00Z", node:"00000000-0000-4000-8000-000000000001",
    state:"seated", label:"proc", role:"worker", tool:"bash", continuity:"assumed",
    seat:{kind:"process",node:"00000000-0000-4000-8000-000000000001",pid:999999,hcom_name:"proc-bus",namespace:"ns",confirmed_at:"2026-07-08T00:00:00Z"},
    provenance:{mechanism:"spawn",spawned_by:"user",cwd:"/mock/cwd",ts:"2026-07-08T00:00:00Z"}
  }'
}

snapshot() {
  jq -cn --argjson panes "$1" --argjson agents "$2" '{result:{protocol:16,version:"mock",panes:$panes,agents:$agents}}' >"$HDR/snapshot.json"
}

proc_empty() {
  jq -n '{result:{process_info:{foreground_processes:[]}}}' >"$HDR/proc-$1.json"
}

run_herder() {
  env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$STATE" MOCK_HERDR_STATE="$HDR" MOCK_HCOM_STATE="$HCOM" GOTOOLCHAIN=local "$@"
}

run_sweep_json() {
  run_herder "${HERDER[@]}" observer sweep --json
}

latest_count() {
  jq -s --arg guid "$1" --arg event "$2" '[.[] | select(.kind!="node" and .guid==$guid and .event==$event)] | length' "$STATE/registry.jsonl"
}

assert_jq() {
  local name="$1" expr="$2" file="$3"
  if jq -e "$expr" "$file" >/dev/null; then pass "$name"; else fail_case "$name" "jq assertion failed: $expr"; fi
}

t1_enrolled_crash_and_noop() {
  case_dir t1
  write_registry <<JSONL
$node_row
$(session_row guid-dead seated alpha t_dead p_dead bus-alpha enroll old-sid)
JSONL
  snapshot '[{"pane_id":"p_dead","terminal_id":"t_dead","label":"alpha"}]' '[{"pane_id":"p_dead","terminal_id":"t_dead","agent":"claude","agent_status":"idle","name":"alpha"}]'
  proc_empty p_dead
  run_sweep_json >"$CASE/out1.json" || fail_case "T-1 sweep" "command failed"
  assert_jq "T-1 unseats enrolled dead occupant" 'select(.status.last_sweep_summary.applied==1)' "$CASE/out1.json"
  [[ "$(latest_count guid-dead unseated)" == "1" ]] && pass "T-1 exactly one unseated row" || fail_case "T-1 exactly one unseated row" "$(cat "$STATE/registry.jsonl")"
  assert_jq "T-1 close_result/evidence" 'select(.guid=="guid-dead" and .event=="unseated" and .close_result=="observed_dead" and (.close_reason|length>0))' "$STATE/registry.jsonl"
  run_sweep_json >"$CASE/out2.json" || fail_case "T-1 rerun" "command failed"
  assert_jq "T-1 rerun typed noop" 'select(.status.last_sweep_summary.noop==1)' "$CASE/out2.json"
  [[ "$(latest_count guid-dead unseated)" == "1" ]] && pass "T-1 rerun appends no duplicate" || fail_case "T-1 rerun duplicate" "$(cat "$STATE/registry.jsonl")"
}

t2_turnover() {
  case_dir t2
  write_registry <<JSONL
$node_row
$(session_row guid-old seated alpha t_old p_old bus-alpha enroll old-sid)
JSONL
  snapshot '[{"pane_id":"p_old","terminal_id":"t_old","label":"alpha"}]' '[{"pane_id":"p_old","terminal_id":"t_old","agent":"claude","agent_status":"idle","name":"alpha"}]'
  jq -cn '{name:"bus-alpha",status:"idle",session_id:"new-sid",process_bound:true,status_age:1}' >"$HCOM/hcom.jsonl"
  run_sweep_json >"$CASE/out1.json" || fail_case "T-2 sweep" "command failed"
  if jq -s -e '[.[] | select(.event=="registered" or .event=="unseated")] | (.[-2].event=="registered" and .[-1].event=="unseated")' "$STATE/registry.jsonl" >/dev/null; then
    pass "T-2 child-first turnover pair"
  else
    fail_case "T-2 child-first turnover pair" "$(cat "$STATE/registry.jsonl")"
  fi
  assert_jq "T-2 child has cleared_from and new sid" 'select(.event=="registered" and .lineage.cleared_from=="guid-old" and .sids[-1].sid=="new-sid")' "$STATE/registry.jsonl"
  run_sweep_json >"$CASE/out2.json" || fail_case "T-2 rerun" "command failed"
  [[ "$(jq -s '[.[] | select(.lineage.cleared_from=="guid-old")] | length' "$STATE/registry.jsonl")" == "1" ]] && pass "T-2 rerun idempotent" || fail_case "T-2 rerun idempotent" "$(cat "$STATE/registry.jsonl")"
}

t4_socket_down_process_continues() {
  case_dir t4
  write_registry <<JSONL
$node_row
$(session_row guid-herdr seated alpha t_missing p_missing bus-alpha enroll old-sid)
$(process_row)
JSONL
  rm -f "$HDR/snapshot.json"
  run_sweep_json >"$CASE/out.json" || fail_case "T-4 sweep" "command failed"
  assert_jq "T-4 protocol incompatible surfaced" 'select(.status.protocol_compatible==false)' "$CASE/out.json"
  [[ "$(latest_count guid-herdr unseated)" == "0" ]] && pass "T-4 herdr verdict paused" || fail_case "T-4 herdr verdict paused" "$(cat "$STATE/registry.jsonl")"
  [[ "$(latest_count guid-proc unseated)" == "1" ]] && pass "T-4 process verdict continues" || fail_case "T-4 process verdict continues" "$(cat "$STATE/registry.jsonl")"
}

t5_t6_t7_advice_and_coexistence() {
  case_dir t567
  write_registry <<JSONL
$node_row
$(session_row guid-side seated newname t_live p_live bus-side spawn)
$(session_row guid-dorm unseated alpha "" "" "" enroll)
$(session_row guid-amb unseated dup "" "" "" enroll)
JSONL
  snapshot '[{"pane_id":"p_live","terminal_id":"t_live","label":"newname"},{"pane_id":"p_alpha","terminal_id":"t_alpha","label":"alpha"},{"pane_id":"p_dup1","terminal_id":"t_dup1","label":"dup"},{"pane_id":"p_dup2","terminal_id":"t_dup2","label":"dup"}]' '[{"pane_id":"p_live","terminal_id":"t_live","agent":"claude","agent_status":"idle","name":"newname"},{"pane_id":"p_alpha","terminal_id":"t_alpha","agent":"claude","agent_status":"idle","name":"alpha"},{"pane_id":"p_dup1","terminal_id":"t_dup1","agent":"claude","agent_status":"idle","name":"dup"},{"pane_id":"p_dup2","terminal_id":"t_dup2","agent":"claude","agent_status":"idle","name":"dup"}]'
  run_sweep_json >"$CASE/out.json" || fail_case "T-5/6/7 sweep" "command failed"
  assert_jq "T-5 sidecar rename not reverted" 'select(.guid=="guid-side" and .label=="newname" and .state=="seated")' "$STATE/registry.jsonl"
  assert_jq "T-6 dormant-live flagged" 'select(any(.status.flags[]?; .guid=="guid-dorm" and .type=="dormant-live"))' "$CASE/out.json"
  run_herder "${HERDER[@]}" list >"$CASE/list.txt" 2>/dev/null
  grep -q 'observer advice: live occupant observed' "$CASE/list.txt" && pass "T-6 list annotates observer advice" || fail_case "T-6 list advice" "$(cat "$CASE/list.txt")"
  assert_jq "T-7 ambiguity flagged" 'select(any(.status.flags[]?; .guid=="guid-amb" and .type=="ambiguous-dormant-live"))' "$CASE/out.json"
}

t9_grep_gate() {
  local gate='registry\.Record|registry\.Status'
  if rg -n "$gate" "$REPO_ROOT/tools/herder/internal/observercmd" >/tmp/observer-grep.$$ 2>&1; then
    fail_case "T-9 v2-only grep gate" "$(cat /tmp/observer-grep.$$)"
  else
    pass "T-9 v2-only grep gate"
  fi
  local tmp="$ROOT/observer-negative.go"
  printf 'package observercmd\nimport "ai-config/tools/herder/internal/registry"\nvar _ registry.Record\n' >"$tmp"
  if rg -n "$gate" "$tmp" >/dev/null 2>&1; then
    pass "T-9 grep gate negative demo trips"
  else
    fail_case "T-9 grep gate negative demo trips" "injected legacy registry.Record was not detected"
  fi
  rm -f /tmp/observer-grep.$$
}

t10_accounting_fresh_node_mint() {
  case_dir t10
  cat >"$STATE/registry.jsonl" <<JSONL
$(session_row guid-fresh seated alpha t_dead p_dead bus-alpha enroll old-sid | jq -c 'del(.node,.seat.node)')
JSONL
  snapshot '[{"pane_id":"p_dead","terminal_id":"t_dead","label":"alpha"}]' '[{"pane_id":"p_dead","terminal_id":"t_dead","agent":"claude","agent_status":"idle","name":"alpha"}]'
  proc_empty p_dead
  run_sweep_json >"$CASE/out.json" || fail_case "T-10 sweep" "command failed"
  assert_jq "T-10 counts observer row not node mint" 'select(.status.last_sweep_summary.applied==1)' "$CASE/out.json"
  [[ "$(jq -s '[.[] | select(.kind=="node")] | length' "$STATE/registry.jsonl")" == "1" ]] && pass "T-10 node mint present but uncounted" || fail_case "T-10 node mint" "$(cat "$STATE/registry.jsonl")"
}

t11_epoch_discrimination() {
  case_dir t11a
  write_registry <<JSONL
$node_row
$(session_row guid-a seated a t1 p1 bus-a enroll old-a)
$(session_row guid-b seated b t2 p2 bus-b enroll old-b)
$(session_row guid-c seated c t3 p3 bus-c enroll old-c)
JSONL
  snapshot '[{"pane_id":"pX","terminal_id":"tX","label":"x"},{"pane_id":"pY","terminal_id":"tY","label":"y"}]' '[]'
  run_sweep_json >"$CASE/out.json" || fail_case "T-11a sweep" "command failed"
  [[ "$(jq -s '[.[] | select(.event=="unseated")] | length' "$STATE/registry.jsonl")" == "0" ]] && pass "T-11a wholesale reissue unseats zero" || fail_case "T-11a wholesale" "$(cat "$STATE/registry.jsonl")"
  assert_jq "T-11a epoch doubt flagged" 'select(any(.status.flags[]?; .type=="epoch-doubt"))' "$CASE/out.json"

  case_dir t11b
  write_registry <<JSONL
$node_row
$(session_row guid-a seated a t1 p1 bus-a enroll old-a)
$(session_row guid-b seated b t2 p2 bus-b enroll old-b)
$(session_row guid-c seated c t3 p3 bus-c enroll old-c)
JSONL
  snapshot '[{"pane_id":"p1","terminal_id":"t1","label":"a"},{"pane_id":"p2","terminal_id":"t2","label":"b"}]' '[]'
  run_sweep_json >"$CASE/out.json" || fail_case "T-11b sweep" "command failed"
  [[ "$(latest_count guid-c unseated)" == "1" && "$(latest_count guid-a unseated)" == "0" ]] && pass "T-11b partial overlap only absent unseats" || fail_case "T-11b partial" "$(cat "$STATE/registry.jsonl")"

  case_dir t11c
  write_registry <<JSONL
$node_row
$(session_row guid-lone seated lone t_lone p_lone bus-lone enroll old-lone)
JSONL
  snapshot '[]' '[]'
  jq -cn '{name:"bus-lone",status:"idle",session_id:"old-lone",process_bound:true,status_age:1}' >"$HCOM/hcom.jsonl"
  run_sweep_json >"$CASE/out.json" || fail_case "T-11c sweep" "command failed"
  [[ "$(latest_count guid-lone unseated)" == "0" ]] && pass "T-11c lone live bus does not unseat" || fail_case "T-11c live bus" "$(cat "$STATE/registry.jsonl")"
  assert_jq "T-11c lone absence flagged" 'select(any(.status.flags[]?; .guid=="guid-lone" and .type=="epoch-doubt"))' "$CASE/out.json"
  : >"$HCOM/hcom.jsonl"
  run_sweep_json >"$CASE/out2.json" || fail_case "T-11c dead bus sweep" "command failed"
  [[ "$(latest_count guid-lone unseated)" == "1" ]] && pass "T-11c lone dead bus unseats" || fail_case "T-11c dead bus" "$(cat "$STATE/registry.jsonl")"
}

t8_status_stop_and_nudge() {
  case_dir t8
  write_registry <<JSONL
$node_row
$(session_row guid-run seated alpha t_dead p_dead bus-alpha enroll old-sid)
JSONL
  snapshot '[{"pane_id":"p_dead","terminal_id":"t_dead","label":"alpha"}]' '[]'
  proc_empty p_dead
  env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$STATE" MOCK_HERDR_STATE="$HDR" MOCK_HCOM_STATE="$HCOM" GOTOOLCHAIN=local "${HERDER[@]}" observer run >"$CASE/run.out" 2>"$CASE/run.err" &
  pid=$!
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    [[ -f "$STATE/observer.status.json" ]] && break
    sleep 0.2
  done
  [[ -f "$STATE/observer.status.json" ]] && pass "T-8 run writes status" || fail_case "T-8 run writes status" "$(cat "$CASE/run.err" 2>/dev/null)"
  run_herder "${HERDER[@]}" observer status >"$CASE/status.txt" || fail_case "T-8 status" "command failed"
  grep -q 'observer status:' "$CASE/status.txt" && pass "T-8 status reports" || fail_case "T-8 status reports" "$(cat "$CASE/status.txt")"
  env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$STATE" MOCK_HERDR_STATE="$HDR" MOCK_HCOM_STATE="$HCOM" GOTOOLCHAIN=local "${HERDER[@]}" observer run >"$CASE/run2.out" 2>"$CASE/run2.err"
  [[ "$?" == "0" ]] && pass "T-8 second singleton exits 0" || fail_case "T-8 singleton" "$(cat "$CASE/run2.err")"
  kill -9 "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  rm -f "$STATE/observer.status.json"
  env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$STATE" MOCK_HERDR_STATE="$HDR" MOCK_HCOM_STATE="$HCOM" GOTOOLCHAIN=local "${HERDER[@]}" observer run >"$CASE/run3.out" 2>"$CASE/run3.err" &
  pid=$!
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    [[ -f "$STATE/observer.status.json" ]] && break
    sleep 0.2
  done
  [[ -f "$STATE/observer.status.json" ]] && pass "T-8 kill9 restart rewrites status" || fail_case "T-8 restart" "$(cat "$CASE/run3.err" 2>/dev/null)"
  run_herder "${HERDER[@]}" observer stop >"$CASE/stop.txt" || fail_case "T-8 stop" "command failed"
  grep -q 'signalled pid' "$CASE/stop.txt" && pass "T-8 stop signals pid" || fail_case "T-8 stop output" "$(cat "$CASE/stop.txt")"
  wait "$pid" 2>/dev/null || true

  case_dir nudge
  write_registry <<JSONL
$node_row
JSONL
  snapshot '[]' '[]'
  env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$STATE" MOCK_HERDR_STATE="$HDR" MOCK_HCOM_STATE="$HCOM" GOTOOLCHAIN=local HERDR_ENV=1 HERDR_PANE_ID=p_self "${HERDER[@]}" enroll --label nudged >/tmp/nudge-default.out 2>"$CASE/nudge-default.err"
  [[ ! -f "$STATE/observer.lock" ]] && pass "nudge default off" || fail_case "nudge default off" "observer.lock exists"
  env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$STATE" MOCK_HERDR_STATE="$HDR" MOCK_HCOM_STATE="$HCOM" GOTOOLCHAIN=local HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-nudge2 HERDER_OBSERVER_AUTOSTART=1 "${HERDER[@]}" enroll --label nudged2 >/tmp/nudge-on.out 2>"$CASE/nudge-on.err"
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    [[ -f "$STATE/observer.lock" ]] && break
    sleep 0.2
  done
  [[ -f "$STATE/observer.lock" ]] && pass "nudge autostart starts observer" || fail_case "nudge autostart starts observer" "$(cat "$CASE/nudge-on.err")"
  run_herder "${HERDER[@]}" observer stop >/dev/null 2>&1 || true
}

t1_enrolled_crash_and_noop
t2_turnover
t4_socket_down_process_continues
t5_t6_t7_advice_and_coexistence
t9_grep_gate
t10_accounting_fresh_node_mint
t11_epoch_discrimination
t8_status_stop_and_nudge

if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN - observer contract holds.\n'
  exit 0
else
  printf '\nOBSERVER CONTRACT DRIFT.\n'
  exit 1
fi
