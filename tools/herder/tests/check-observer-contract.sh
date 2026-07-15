#!/usr/bin/env bash
# check-observer-contract.sh — hermetic observer phase-1a contract.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"
HERDER=("$REPO_ROOT/bin/herder")

toolchain_fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN"
trap 'for p in ${SOCKET_PIDS:-}; do kill "$p" 2>/dev/null || true; done; rm -rf "$ROOT"' EXIT

cat >"$MOCKBIN/herdr" <<'MOCK_HERDR'
#!/usr/bin/env bash
set -euo pipefail
STATE="${MOCK_HERDR_STATE:?}"
case "${1:-} ${2:-}" in
  "status server")
    printf 'status: running\nversion: mock\nprotocol: %s\ncompatible: %s\nsocket: %s/herdr.sock\n' "${MOCK_HERDR_PROTOCOL:-16}" "${MOCK_HERDR_COMPATIBLE:-yes}" "$STATE";;
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
    jq '{result:{agents:[(.result.snapshot.agents // .result.agents // [])[]?]}}' "$STATE/snapshot.json";;
  "pane list")
    jq '{result:{panes:[(.result.snapshot.panes // .result.panes // [])[]? | {pane_id,terminal_id}]}}' "$STATE/snapshot.json";;
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

GO_MOD="$REPO_ROOT/tools/herder/go.mod"
GO_VERSION="$(awk '$1 == "go" {print $2; exit}' "$GO_MOD")"
[ -n "$GO_VERSION" ] || toolchain_fail "cannot read the toolchain pin ('go X.Y.Z') from $GO_MOD"
TOOLCHAIN="$(awk '$1 == "toolchain" {print $2; exit}' "$GO_MOD")"
[ -z "$TOOLCHAIN" ] || [ "$TOOLCHAIN" = "go$GO_VERSION" ] ||
  toolchain_fail "go.mod declares toolchain ${TOOLCHAIN} but pins go ${GO_VERSION}; the go directive is the authority — align or drop the toolchain directive"
GO_ROOT="$(mise where "go@$GO_VERSION")" ||
  toolchain_fail "could not resolve go ${GO_VERSION} via mise (not installed, or mise unavailable); fix: mise install go@${GO_VERSION}"
GO_BIN="$GO_ROOT/bin"
GO_HAVE="$(env -u GOROOT GOTOOLCHAIN=local "$GO_BIN/go" env GOVERSION 2>/dev/null)" ||
  toolchain_fail "cannot execute the pinned go toolchain at $GO_BIN/go"
GO_HAVE="${GO_HAVE#go}"
[ "$GO_HAVE" = "$GO_VERSION" ] ||
  toolchain_fail "go toolchain resolves to ${GO_HAVE:-unknown}, but go.mod pins go ${GO_VERSION}"
PATH_HERMETIC="$GO_BIN:$MOCKBIN:/usr/bin:/bin:/usr/local/bin:$HOME/.local/bin"
fail=0
SELECTOR="${1:-${OBSERVER_CONTRACT_STEP:-all}}"
case "$SELECTOR" in
  all|step1|step2|step3) ;;
  *) printf 'usage: %s [all|step1|step2|step3]\n' "$0" >&2; exit 2;;
esac

pass() { printf 'PASS  %s\n' "$1"; }
fail_case() { printf 'FAIL  %s: %s\n' "$1" "$2"; fail=1; }

case_dir() {
  CASE="$ROOT/$1"
  STATE="$CASE/state"
  HDR="$CASE/herdr"
  HCOM="$CASE/hcom"
  mkdir -p "$STATE" "$HDR" "$HCOM"
  : >"$HCOM/hcom.jsonl"
  start_socket_server
}

start_socket_server() {
  python3 - "$HDR" >"$CASE/socket.log" 2>&1 <<'PY' &
import json, os, socket, sys, threading, time

state = sys.argv[1]
sock_path = os.path.join(state, "herdr.sock")
try:
    os.unlink(sock_path)
except FileNotFoundError:
    pass

server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
server.bind(sock_path)
server.listen(20)

def load_json(path, default):
    try:
        with open(path) as f:
            return json.load(f)
    except FileNotFoundError:
        return default

def response(req):
    mid = req.get("id")
    method = req.get("method")
    params = req.get("params") or {}
    if method == "session.snapshot":
        snap = load_json(os.path.join(state, "snapshot.json"), {"result": {"type": "session_snapshot", "snapshot": {"protocol": 16, "version": "mock", "panes": [], "agents": []}}})
        return {"id": mid, "result": snap.get("result", snap)}
    if method == "pane.process_info":
        pane_id = params.get("pane_id") or ""
        default = {"result": {"process_info": {"foreground_processes": [{"pid": 1234, "argv": ["claude"], "cwd": "/mock/cwd"}]}}}
        proc = load_json(os.path.join(state, "proc-" + pane_id + ".json"), default)
        return {"id": mid, "result": proc.get("result", proc)}
    if method == "events.subscribe":
        with open(os.path.join(state, "subscribed"), "w") as f:
            f.write(json.dumps(params, separators=(",", ":")))
        return {"id": mid, "result": {"type": "subscription_started"}}
    return {"id": mid, "error": {"code": "unknown_method", "message": method or ""}}

def handle(conn):
    with conn:
        with open(os.path.join(state, "connections"), "a") as f:
            f.write("1\n")
        f = conn.makefile("rwb")
        for raw in f:
            try:
                req = json.loads(raw)
                resp = response(req)
            except Exception as exc:
                req = {}
                resp = {"id": None, "error": {"code": "mock_error", "message": str(exc)}}
            f.write((json.dumps(resp, separators=(",", ":")) + "\n").encode())
            f.flush()
            if os.path.exists(os.path.join(state, "close_after_first_request")):
                return
            if os.path.exists(os.path.join(state, "close_rpc_after_first_request")) and req.get("method") != "events.subscribe":
                return
            if os.path.exists(os.path.join(state, "close_rpc_after_first_request")) and req.get("method") == "events.subscribe":
                while True:
                    time.sleep(60)

while True:
    conn, _ = server.accept()
    threading.Thread(target=handle, args=(conn,), daemon=True).start()
PY
  local pid=$!
  SOCKET_PIDS="${SOCKET_PIDS:-} $pid"
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    [[ -S "$HDR/herdr.sock" ]] && return
    sleep 0.1
  done
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
  jq -cn --argjson panes "$1" --argjson agents "$2" '{result:{type:"session_snapshot",snapshot:{protocol:16,version:"mock",panes:$panes,agents:$agents}}}' >"$HDR/snapshot.json"
}

proc_empty() {
  jq -n '{result:{process_info:{foreground_processes:[]}}}' >"$HDR/proc-$1.json"
}

run_herder() {
  env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$STATE" MOCK_HERDR_STATE="$HDR" MOCK_HCOM_STATE="$HCOM" MOCK_HERDR_PROTOCOL="${MOCK_HERDR_PROTOCOL:-16}" MOCK_HERDR_COMPATIBLE="${MOCK_HERDR_COMPATIBLE:-yes}" HERDER_OBSERVER_ALLOW_CLI_FALLBACK="${HERDER_OBSERVER_ALLOW_CLI_FALLBACK:-}" GOTOOLCHAIN=local "$@"
}

run_sweep_json() {
  run_herder "${HERDER[@]}" observer sweep --json
}

latest_count() {
  jq -s --arg guid "$1" --arg event "$2" '[.[] | select(.kind!="node" and .guid==$guid and .event==$event)] | length' "$STATE/registry.jsonl"
}

connection_count() {
  if [[ -f "$HDR/connections" ]]; then
    wc -l <"$HDR/connections"
  else
    printf '0\n'
  fi
}

mtime_ns() {
  python3 - "$1" <<'PY'
import os, sys
print(os.stat(sys.argv[1]).st_mtime_ns)
PY
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
  jq -cn '{name:"bus-alpha",status:"idle",session_id:"new-sid",process_bound:true,status_age:1,launch_context:{pane_id:"p_old"}}' >"$HCOM/hcom.jsonl"
  run_sweep_json >"$CASE/out1.json" || fail_case "T-2 sweep" "command failed"
  if jq -s -e '[.[] | select(.event=="registered" or .event=="unseated")] | (.[-2].event=="registered" and .[-1].event=="unseated")' "$STATE/registry.jsonl" >/dev/null; then
    pass "T-2 child-first turnover pair"
  else
    fail_case "T-2 child-first turnover pair" "$(cat "$STATE/registry.jsonl")"
  fi
  assert_jq "T-2 child has cleared_from and new sid" 'select(.event=="registered" and .lineage.cleared_from=="guid-old" and .sids[-1].sid=="new-sid")' "$STATE/registry.jsonl"
  assert_jq "T-2 child bus identity reverified" 'select(.event=="registered" and .lineage.cleared_from=="guid-old" and .seat.hcom_name=="bus-alpha" and .seat.hcom_verified==true)' "$STATE/registry.jsonl"
  run_sweep_json >"$CASE/out2.json" || fail_case "T-2 rerun" "command failed"
  [[ "$(jq -s '[.[] | select(.lineage.cleared_from=="guid-old")] | length' "$STATE/registry.jsonl")" == "1" ]] && pass "T-2 rerun idempotent" || fail_case "T-2 rerun idempotent" "$(cat "$STATE/registry.jsonl")"

  case_dir t2_first_sid
  write_registry <<JSONL
$node_row
$(session_row guid-first seated alpha t_first p_first bus-first enroll)
JSONL
  snapshot '[{"pane_id":"p_first","terminal_id":"t_first","label":"alpha"}]' '[{"pane_id":"p_first","terminal_id":"t_first","agent":"claude","agent_status":"idle","name":"alpha"}]'
  jq -cn '{name:"bus-first",status:"idle",session_id:"first-sid",process_bound:true,status_age:1,launch_context:{pane_id:"p_first"}}' >"$HCOM/hcom.jsonl"
  run_sweep_json >"$CASE/out-first.json" || fail_case "T-2 first sid sweep" "command failed"
  assert_jq "T-2 first sid recognises same GUID" 'select(.guid=="guid-first" and .event=="recognised" and .state=="seated" and .sids[-1].sid=="first-sid" and .observed_via=="observer sid enrichment")' "$STATE/registry.jsonl"
  [[ "$(jq -s '[.[] | select(.lineage.cleared_from=="guid-first")] | length' "$STATE/registry.jsonl")" == "0" ]] && pass "T-2 first sid does not turnover" || fail_case "T-2 first sid child minted" "$(cat "$STATE/registry.jsonl")"

  case_dir t2_concurrent
  write_registry <<JSONL
$node_row
$(session_row guid-race seated alpha t_race p_race bus-race enroll old-sid)
JSONL
  snapshot '[{"pane_id":"p_race","terminal_id":"t_race","label":"alpha"}]' '[{"pane_id":"p_race","terminal_id":"t_race","agent":"claude","agent_status":"idle","name":"alpha"}]'
  jq -cn '{name:"bus-race",status:"idle",session_id:"new-sid",process_bound:true,status_age:1,launch_context:{pane_id:"p_race"}}' >"$HCOM/hcom.jsonl"
  run_sweep_json >"$CASE/race1.json" &
  race1=$!
  run_sweep_json >"$CASE/race2.json" &
  race2=$!
  wait "$race1" || fail_case "T-2 concurrent sweep 1" "command failed"
  wait "$race2" || fail_case "T-2 concurrent sweep 2" "command failed"
  [[ "$(jq -s '[.[] | select(.lineage.cleared_from=="guid-race" and .sids[-1].sid=="new-sid")] | length' "$STATE/registry.jsonl")" == "1" ]] && pass "T-2 concurrent turnover dedupes under lock" || fail_case "T-2 concurrent dedupe" "$(cat "$STATE/registry.jsonl")"
}

t4_socket_down_process_continues() {
  case_dir t4
  write_registry <<JSONL
$node_row
$(session_row guid-herdr seated alpha t_missing p_missing bus-alpha enroll old-sid)
$(process_row)
JSONL
  rm -f "$HDR/herdr.sock" "$HDR/snapshot.json"
  jq -cn '{name:"proc-bus",status:"stopped",process_bound:false,status_age:1}' >"$HCOM/hcom.jsonl"
  run_sweep_json >"$CASE/out.json" || fail_case "T-4 sweep" "command failed"
  assert_jq "T-4 protocol incompatible surfaced" 'select(.status.protocol_compatible==false)' "$CASE/out.json"
  [[ "$(latest_count guid-herdr unseated)" == "0" ]] && pass "T-4 herdr verdict paused" || fail_case "T-4 herdr verdict paused" "$(cat "$STATE/registry.jsonl")"
  [[ "$(latest_count guid-proc unseated)" == "1" ]] && pass "T-4 process verdict continues" || fail_case "T-4 process verdict continues" "$(cat "$STATE/registry.jsonl")"

  case_dir t4_protocol
  write_registry <<JSONL
$node_row
$(session_row guid-proto seated alpha t_proto p_proto bus-alpha enroll old-sid)
JSONL
  snapshot '[]' '[]'
  jq -cn '{name:"bus-alpha",status:"stopped",process_bound:false,status_age:1}' >"$HCOM/hcom.jsonl"
  HERDER_OBSERVER_ALLOW_CLI_FALLBACK=1 MOCK_HERDR_PROTOCOL=17 run_sweep_json >"$CASE/proto.json" || fail_case "T-4 protocol fallback sweep" "command failed"
  assert_jq "T-4 protocol pin does not trust server compatible flag" 'select(.status.protocol_detail | contains("cli-fallback"))' "$CASE/proto.json"
  [[ "$(latest_count guid-proto unseated)" == "1" ]] && pass "T-4 CLI fallback only on protocol mismatch" || fail_case "T-4 protocol fallback" "$(cat "$STATE/registry.jsonl")"
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
  observer_legacy_gate() {
    local scan_dir="$1"
    python3 - "$scan_dir" <<'PY'
import pathlib, re, sys

root = pathlib.Path(sys.argv[1])
bad = []
for path in root.rglob("*.go"):
    text = path.read_text()
    # The observer must import internal/registry for the ratified UpdateLocked
    # writer path. The forbidden legacy view is registry.Record/Status, including
    # through alias imports such as `import reg ".../registry"; reg.Record`.
    aliases = {"registry"}
    for match in re.finditer(r'import\s+(?:\((?P<block>.*?)\)|(?P<line>[^\n]+))', text, re.S):
        imports = (match.group("block") or match.group("line") or "").splitlines()
        for item in imports:
            if '"ai-config/tools/herder/internal/registry"' not in item:
                continue
            stripped = item.strip()
            alias = stripped.split()[0] if len(stripped.split()) > 1 else "registry"
            if alias not in {".", "_"} and not alias.startswith('"'):
                aliases.add(alias)
            else:
                aliases.add("registry")
    for alias in aliases:
        pattern = re.compile(rf'\b{re.escape(alias)}\.(Record|Status)\b')
        for lineno, line in enumerate(text.splitlines(), 1):
            if pattern.search(line):
                bad.append(f"{path}:{lineno}:{line}")
if bad:
    print("\n".join(bad))
    sys.exit(1)
PY
  }
  if observer_legacy_gate "$REPO_ROOT/tools/herder/internal/observercmd" >/tmp/observer-grep.$$ 2>&1; then
    pass "T-9 v2-only grep gate"
  else
    fail_case "T-9 v2-only grep gate" "$(cat /tmp/observer-grep.$$)"
  fi
  local neg="$ROOT/t9-negative/observercmd"
  mkdir -p "$neg"
  printf 'package observercmd\nimport reg "ai-config/tools/herder/internal/registry"\nvar _ reg.Record\n' >"$neg/observer-negative.go"
  if observer_legacy_gate "$neg" >/dev/null 2>&1; then
    fail_case "T-9 grep gate negative demo trips through scan function" "injected legacy registry import was not detected"
  else
    pass "T-9 grep gate negative demo trips through scan function"
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
  run_herder "${HERDER[@]}" list >"$CASE/list.txt" 2>/dev/null
  grep -q 'observer advice: epoch doubt' "$CASE/list.txt" && pass "T-11a list surfaces epoch-wide advice" || fail_case "T-11a list epoch advice" "$(cat "$CASE/list.txt")"

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
  run_sweep_json >"$CASE/out2.json" || fail_case "T-11c absent bus sweep" "command failed"
  [[ "$(latest_count guid-lone unseated)" == "0" ]] && pass "T-11c absent bus row alone does not unseat" || fail_case "T-11c absent bus row" "$(cat "$STATE/registry.jsonl")"
  jq -cn '{name:"bus-lone",status:"stopped",session_id:"old-lone",process_bound:false,status_age:1}' >"$HCOM/hcom.jsonl"
  run_sweep_json >"$CASE/out3.json" || fail_case "T-11c dead bus sweep" "command failed"
  [[ "$(latest_count guid-lone unseated)" == "1" ]] && pass "T-11c present dead bus row unseats" || fail_case "T-11c dead bus" "$(cat "$STATE/registry.jsonl")"

  case_dir t11d
  write_registry <<JSONL
$node_row
$(session_row guid-a seated a t1 p1 bus-a enroll old-a)
$(session_row guid-b seated b t2 p2 bus-b enroll old-b)
$(session_row guid-c seated c t3 p3 bus-c enroll old-c)
JSONL
  snapshot '[{"pane_id":"p1","terminal_id":"t1","label":"a"},{"pane_id":"p2","terminal_id":"t2","label":"b"},{"pane_id":"p3","terminal_id":"t3","label":"c"}]' '[]'
  env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$STATE" MOCK_HERDR_STATE="$HDR" MOCK_HCOM_STATE="$HCOM" GOTOOLCHAIN=local HERDER_OBSERVER_SWEEP_INTERVAL=1s "${HERDER[@]}" observer run >"$CASE/run.out" 2>"$CASE/run.err" &
  pid=$!
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    [[ -f "$STATE/observer.status.json" && -f "$HDR/subscribed" ]] && break
    sleep 0.2
  done
  [[ -f "$HDR/subscribed" ]] && pass "T-11d persistent run subscribes to pane events" || fail_case "T-11d subscribe" "$(cat "$CASE/run.err" 2>/dev/null)"
  snapshot '[]' '[]'
  for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
    [[ "$(jq -s '[.[] | select(.event=="unseated" and .close_result=="observed_dead")] | length' "$STATE/registry.jsonl")" == "3" ]] && break
    sleep 0.3
  done
  [[ "$(jq -s '[.[] | select(.event=="unseated" and .close_result=="observed_dead")] | length' "$STATE/registry.jsonl")" == "3" ]] && pass "T-11d uninterrupted socket absence unseats all previously seen terms" || fail_case "T-11d same-epoch absence" "$(cat "$STATE/registry.jsonl")"
  run_herder "${HERDER[@]}" observer stop >/dev/null 2>&1 || true
  wait "$pid" 2>/dev/null || true
}

t8_status_stop() {
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
  [[ "$(latest_count guid-run unseated)" == "1" ]] && pass "T-8 registry converges before restart" || fail_case "T-8 pre-restart convergence" "$(cat "$STATE/registry.jsonl")"
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
  [[ "$(latest_count guid-run unseated)" == "1" ]] && pass "T-8 kill9 restart preserves registry convergence" || fail_case "T-8 restart convergence" "$(cat "$STATE/registry.jsonl")"
  run_herder "${HERDER[@]}" observer stop >"$CASE/stop.txt" || fail_case "T-8 stop" "command failed"
  grep -q 'signalled pid' "$CASE/stop.txt" && pass "T-8 stop signals pid" || fail_case "T-8 stop output" "$(cat "$CASE/stop.txt")"
  wait "$pid" 2>/dev/null || true
}

t12_close_after_first_rpc_transport() {
  case_dir t12split
  : >"$HDR/close_rpc_after_first_request"
  write_registry <<JSONL
$node_row
$(session_row guid-a seated a t1 p1 bus-a enroll old-a)
$(session_row guid-b seated b t2 p2 bus-b enroll old-b)
JSONL
  snapshot '[{"pane_id":"p1","terminal_id":"t1","label":"a"},{"pane_id":"p2","terminal_id":"t2","label":"b"}]' '[]'
  env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$STATE" MOCK_HERDR_STATE="$HDR" MOCK_HCOM_STATE="$HCOM" GOTOOLCHAIN=local HERDER_OBSERVER_SWEEP_INTERVAL=1s "${HERDER[@]}" observer run >"$CASE/run.out" 2>"$CASE/run.err" &
  pid=$!
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    jq -e '.protocol_compatible == true' "$STATE/observer.status.json" >/dev/null 2>&1 && [[ -f "$HDR/subscribed" ]] && break
    sleep 0.2
  done
  jq -e '.protocol_compatible == true' "$STATE/observer.status.json" >/dev/null 2>&1 && pass "T-12 split transport sweeps through close-after-first RPC server" || fail_case "T-12 split transport status" "$(cat "$CASE/run.err" 2>/dev/null)"
  [[ -f "$HDR/subscribed" ]] && pass "T-12 split transport keeps subscribe stream separate" || fail_case "T-12 split transport subscribe" "$(cat "$CASE/run.err" 2>/dev/null)"
  snapshot '[]' '[]'
  for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
    [[ "$(jq -s '[.[] | select(.event=="unseated" and .close_result=="observed_dead")] | length' "$STATE/registry.jsonl")" == "2" ]] && break
    sleep 0.3
  done
  [[ "$(jq -s '[.[] | select(.event=="unseated" and .close_result=="observed_dead")] | length' "$STATE/registry.jsonl")" == "2" ]] && pass "T-12 split transport preserves same-epoch absence semantics" || fail_case "T-12 split transport absence" "$(cat "$STATE/registry.jsonl")"
  run_herder "${HERDER[@]}" observer stop >/dev/null 2>&1 || true
  wait "$pid" 2>/dev/null || true
}

t13_post_death_backoff_and_failed_heartbeat() {
  case_dir t13backoff
  : >"$HDR/close_after_first_request"
  write_registry <<JSONL
$node_row
JSONL
  snapshot '[]' '[]'
  env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$STATE" MOCK_HERDR_STATE="$HDR" MOCK_HCOM_STATE="$HCOM" GOTOOLCHAIN=local HERDER_OBSERVER_SWEEP_INTERVAL=2s "${HERDER[@]}" observer run >"$CASE/run.out" 2>"$CASE/run.err" &
  pid=$!
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    grep -q 'reconnecting after' "$CASE/run.err" 2>/dev/null && break
    sleep 0.2
  done
  before="$(connection_count)"
  sleep 0.7
  after="$(connection_count)"
  if (( after <= before + 1 )); then
    pass "T-13 post-death reconnect backs off"
  else
    fail_case "T-13 post-death reconnect backs off" "connections before=$before after=$after"
  fi
  grep -q 'reconnecting after herdr socket closed' "$CASE/run.err" && pass "T-13 reconnect transition logs cause" || fail_case "T-13 reconnect transition log" "$(cat "$CASE/run.err" 2>/dev/null)"
  run_herder "${HERDER[@]}" observer stop >/dev/null 2>&1 || true
  wait "$pid" 2>/dev/null || true

  case_dir t13heartbeat
  write_registry <<JSONL
$node_row
JSONL
  snapshot '[]' '[]'
  env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$STATE" MOCK_HERDR_STATE="$HDR" MOCK_HCOM_STATE="$HCOM" MOCK_HERDR_PROTOCOL=17 GOTOOLCHAIN=local HERDER_OBSERVER_SWEEP_INTERVAL=1s "${HERDER[@]}" observer run >"$CASE/run.out" 2>"$CASE/run.err" &
  pid=$!
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    [[ -f "$STATE/observer.lock" && -f "$STATE/observer.status.json" ]] && jq -e '.protocol_compatible == false' "$STATE/observer.status.json" >/dev/null 2>&1 && break
    sleep 0.2
  done
  m1="$(mtime_ns "$STATE/observer.lock")"
  sleep 1.3
  m2="$(mtime_ns "$STATE/observer.lock")"
  [[ "$m1" == "$m2" ]] && pass "T-13 failed sweeps do not advance heartbeat file" || fail_case "T-13 failed heartbeat stalls" "mtime before=$m1 after=$m2"
  grep -q 'sweep transport unhealthy' "$CASE/run.err" && pass "T-13 failed sweep logs transport cause" || fail_case "T-13 failed sweep log" "$(cat "$CASE/run.err" 2>/dev/null)"
  run_herder "${HERDER[@]}" observer stop >/dev/null 2>&1 || true
  wait "$pid" 2>/dev/null || true
}

t14_rebound_socket_starts_new_epoch() {
  case_dir t14epoch
  write_registry <<JSONL
$node_row
$(session_row guid-a seated a t1 p1 bus-a enroll old-a)
$(session_row guid-b seated b t2 p2 bus-b enroll old-b)
JSONL
  snapshot '[{"pane_id":"p1","terminal_id":"t1","label":"a"},{"pane_id":"p2","terminal_id":"t2","label":"b"}]' '[]'
  env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$STATE" MOCK_HERDR_STATE="$HDR" MOCK_HCOM_STATE="$HCOM" GOTOOLCHAIN=local HERDER_OBSERVER_SWEEP_INTERVAL=1s "${HERDER[@]}" observer run >"$CASE/run.out" 2>"$CASE/run.err" &
  pid=$!
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    jq -e '.protocol_compatible == true' "$STATE/observer.status.json" >/dev/null 2>&1 && [[ -f "$HDR/subscribed" ]] && break
    sleep 0.2
  done
  sleep 1.4
  before="$(jq -s '[.[] | select(.event=="unseated" and .close_result=="observed_dead")] | length' "$STATE/registry.jsonl")"
  snapshot '[{"pane_id":"pZ","terminal_id":"tZ","label":"z"}]' '[]'
  start_socket_server
  sleep 4
  after="$(jq -s '[.[] | select(.event=="unseated" and .close_result=="observed_dead")] | length' "$STATE/registry.jsonl")"
  [[ "$before" == "0" && "$after" == "0" ]] && pass "T-14 rebound socket starts new epoch without same-epoch unseats" || fail_case "T-14 rebound socket epoch guard" "$(cat "$STATE/registry.jsonl")"
  grep -q 'herdr socket incarnation changed' "$CASE/run.err" && pass "T-14 rebound socket reconnects with incarnation-change cause" || fail_case "T-14 rebound socket reconnect log" "$(cat "$CASE/run.err" 2>/dev/null)"
  run_herder "${HERDER[@]}" observer stop >/dev/null 2>&1 || true
  wait "$pid" 2>/dev/null || true
}

tnudge_autostart() {
  case_dir nudge
  write_registry <<JSONL
$node_row
JSONL
  snapshot '[]' '[]'
  env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$STATE" MOCK_HERDR_STATE="$HDR" MOCK_HCOM_STATE="$HCOM" GOTOOLCHAIN=local HERDR_ENV=1 HERDR_PANE_ID=p_self "${HERDER[@]}" enroll --label nudged >/tmp/nudge-default.out 2>"$CASE/nudge-default.err"
  [[ ! -f "$STATE/observer.lock" ]] && pass "nudge default off" || fail_case "nudge default off" "observer.lock exists"
  printf '[{"name":"nudged-bus","session_id":"sid-nudge","joined":true,"launch_context":{"pane_id":"p_self"}}]\n' >"$HCOM/hcom.jsonl"
  env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" HERDER_STATE_DIR="$STATE" MOCK_HERDR_STATE="$HDR" MOCK_HCOM_STATE="$HCOM" GOTOOLCHAIN=local HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_GUID=guid-nudge2 HCOM_SESSION_ID=sid-nudge HERDER_OBSERVER_AUTOSTART=1 "${HERDER[@]}" enroll --label nudged2 >/tmp/nudge-on.out 2>"$CASE/nudge-on.err"
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    [[ -f "$STATE/observer.lock" ]] && break
    sleep 0.2
  done
  [[ -f "$STATE/observer.lock" ]] && pass "nudge autostart starts observer" || fail_case "nudge autostart starts observer" "$(cat "$CASE/nudge-on.err")"
  run_herder "${HERDER[@]}" observer stop >/dev/null 2>&1 || true
}

run_step1() {
  t1_enrolled_crash_and_noop
  t2_turnover
  t4_socket_down_process_continues
  t5_t6_t7_advice_and_coexistence
  t9_grep_gate
  t10_accounting_fresh_node_mint
  t11_epoch_discrimination
}

run_step2() {
  t8_status_stop
  t12_close_after_first_rpc_transport
  t13_post_death_backoff_and_failed_heartbeat
  t14_rebound_socket_starts_new_epoch
}

run_step3() {
  tnudge_autostart
}

case "$SELECTOR" in
  all)
    run_step1
    run_step2
    run_step3
    ;;
  step1) run_step1 ;;
  step2) run_step2 ;;
  step3) run_step3 ;;
esac

if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN - observer contract holds.\n'
  exit 0
else
  printf '\nOBSERVER CONTRACT DRIFT.\n'
  exit 1
fi
