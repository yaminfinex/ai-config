#!/usr/bin/env bash
# Hermetic end-to-end contract for herder serve, fleet JSON, and first SSE frames.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
HERDER_ROOT="$REPO_ROOT/tools/herder"
WEB_ROOT="$HERDER_ROOT/web"
FIXTURE="$TESTS_DIR/fixtures/list-live"
ROOT="$(mktemp -d)"
server_pid=""
socket_pid=""
cleanup() {
  if [ -n "$server_pid" ]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  if [ -n "$socket_pid" ]; then
    kill "$socket_pid" 2>/dev/null || true
    wait "$socket_pid" 2>/dev/null || true
  fi
  rm -rf "$ROOT"
}
trap cleanup EXIT

fail=0
pass() { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=$((fail + 1)); }

if command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1; then
  # Build outside the worktree: comparing complete directories catches stale
  # source and direct dist tampering without silently repairing either one.
  if npm --prefix "$WEB_ROOT" ci &&
    npm --prefix "$WEB_ROOT" run lint &&
    npm --prefix "$WEB_ROOT" run test &&
    npm --prefix "$WEB_ROOT" run typecheck &&
    npm --prefix "$WEB_ROOT" run build:check -- --outDir "$ROOT/web-dist" --emptyOutDir &&
    diff -qr "$ROOT/web-dist" "$HERDER_ROOT/internal/webui/dist"; then
    pass "web lint, typecheck, production build, and committed artifact drift check pass"
  else
    bad "web build gates" "npm UI checks failed or committed dist differs from a clean production build"
  fi
else
  pass "web build gates skipped because node/npm are absent (committed dist remains buildable by Go)"
fi

mkdir -p "$ROOT/bin" "$ROOT/home" "$ROOT/cache"
if ! (cd "$HERDER_ROOT" && go build -o "$ROOT/herder" ./cmd/herder); then
  printf 'FAIL  could not build herder\n'
  exit 1
fi

cat >"$ROOT/bin/hcom" <<'HCOM'
#!/usr/bin/env bash
if [ "${1:-}" = list ] && [ "${2:-}" = --json ]; then
  exec /bin/cat "$WEB_SERVE_ROSTER"
fi
if [ "${1:-}" = send ]; then
  printf '<send>\n' >>"$WEB_SEND_CALLS"
  for arg in "$@"; do printf '<%s>\n' "$arg"; done >"$WEB_SEND_LOG"
  exit 0
fi
if [ "${1:-}" = events ]; then
  case " $* " in
    *" events --last 500 --full --type message "*)
      printf '%s\n' \
        '{"id":97,"ts":"2099-01-01T00:00:00.000001+00:00","type":"message","data":{"from":"web-owner","delivered_to":["vile"],"intent":"request","text":"[HERDER_WEB_OPERATOR_NOTE_BEGIN]\n[This message came from a web operator named web-owner via the fleet web view. They cannot receive hcom messages; do not reply with `hcom send`. Answer in your normal chat turn; they are watching the session transcript live.]\n[HERDER_WEB_OPERATOR_NOTE_END]\n\noperator question"}}' \
        '{"id":98,"ts":"2099-01-01T00:00:00.000002+00:00","type":"message","data":{"from":"vile","delivered_to":["vile"],"intent":"inform","text":"idle agent note"}}'
      exit 0
      ;;
    *" events --last 1 --full --type message "*) exit 0 ;;
    *" events --wait 30 --full --type message --sql id > 0 "*|*" events --last 10000 --full --type message --sql id > 0 "*)
      printf '%s\n' '{"id":99,"ts":"2099-01-01T00:00:00.000001+00:00","type":"message","data":{"from":"vile","delivered_to":["dore"],"thread":"web-serve","text":"fixture message"}}'
      exit 0
      ;;
    *" events --wait 30 --full --type message --sql id > 99 "*)
      printf '%s\n' '{"timed_out":true}'
      exit 1
      ;;
  esac
fi
printf 'unexpected hcom args: %s\n' "$*" >&2
exit 2
HCOM
chmod +x "$ROOT/bin/hcom"

cat >"$ROOT/bin/herdr" <<'HERDR'
#!/usr/bin/env bash
if [ "$*" = "worktree list --workspace w2" ]; then
  printf '%s\n' '{"result":{"source":{"source_workspace_id":"w1"},"type":"worktree_list","worktrees":[{"is_linked_worktree":false,"open_workspace_id":"w1"},{"is_linked_worktree":true,"open_workspace_id":"w2"}]}}'
  exit 0
fi
printf 'unexpected herdr args: %s\n' "$*" >&2
exit 2
HERDR
chmod +x "$ROOT/bin/herdr"

mkdir -p "$ROOT/action-root/tools/fleet"
cat >"$ROOT/action-root/tools/fleet/spawn.sh" <<'SPAWN'
#!/usr/bin/env bash
for arg in "$@"; do printf '<%s>\n' "$arg"; done >"$WEB_SPAWN_LOG"
jq '. + [{"name":"spawn-vava","base_name":"spawn-vava","tool":"codex","status":"listening","joined":true,"session_id":"session-spawn","launch_context":{"pane_id":"w1:p9"}}]' "$WEB_SERVE_ROSTER" >"$WEB_SERVE_ROSTER.tmp"
mv "$WEB_SERVE_ROSTER.tmp" "$WEB_SERVE_ROSTER"
jq '.panes += [{"pane_id":"w1:p9","workspace_id":"w1","tab_id":"t1","agent":"codex","agent_status":"active","agent_session":"session-spawn"}]' "$WEB_SERVE_SNAPSHOT" >"$WEB_SERVE_SNAPSHOT.tmp"
mv "$WEB_SERVE_SNAPSHOT.tmp" "$WEB_SERVE_SNAPSHOT"
printf '%s\n' 'Started the launch process' 'Names: spawn-vava' 'Batch id: batch-spawn' >&2
printf '%s\n' 'name=spawn-vava' 'pane=w1:p9' 'cwd=/repo' 'placement=split-pane'
SPAWN
chmod +x "$ROOT/action-root/tools/fleet/spawn.sh"

cat >"$ROOT/bin/tailscale" <<'TAILSCALE'
#!/usr/bin/env bash
if [ " $* " != " whois --json 127.0.0.1 " ]; then
  printf 'unexpected tailscale args: %s\n' "$*" >&2
  exit 2
fi
mode=ok
[ ! -f "$WEB_WHOIS_MODE" ] || mode="$(/bin/cat "$WEB_WHOIS_MODE")"
case "$mode" in
  ok) login='Alice@Example.com' ;;
  existing) login='vile' ;;
  reserved) login='bigboss@example.com' ;;
  unresolved) printf 'peer not found\n' >&2; exit 1 ;;
  *) printf 'unknown whois fixture mode: %s\n' "$mode" >&2; exit 2 ;;
esac
printf '{"UserProfile":{"LoginName":"%s"}}\n' "$login"
TAILSCALE
chmod +x "$ROOT/bin/tailscale"

jq 'map(if .name == "vile" then .session_id = "73100000-0000-4000-8000-000000000731" | .directory = "/invented/violet" else . end) + [{"name":"vile_general_purpose_1","base_name":"vile_general_purpose_1","parent_name":"vile","agent_id":"a35b593a6be7a9ba5","tool":"claude","status":"active","directory":"/invented/violet","launch_context":{}},{"name":"web-vile","base_name":"web-vile","tool":"codex","status":"listening","joined":true,"session_id":"session-web-vile","launch_context":{}}]' \
  "$FIXTURE/roster.json" >"$ROOT/roster.json"
cp "$FIXTURE/snapshot.json" "$ROOT/snapshot.json"
mkdir -p "$ROOT/home/.claude/projects/-invented-violet"
session_path="$ROOT/home/.claude/projects/-invented-violet/73100000-0000-4000-8000-000000000731.jsonl"
cat >"$session_path" <<'SESSION'
{"type":"user","uuid":"invented-web-human","timestamp":"2026-01-02T03:04:05.000Z","origin":{"kind":"human"},"promptSource":"typed","message":{"role":"user","content":"Invented web endpoint prompt."}}
{"type":"assistant","uuid":"invented-web-answer","timestamp":"2026-01-02T03:04:06.000Z","message":{"role":"assistant","model":"invented-claude-model","content":[{"type":"text","text":"Invented web endpoint answer."}],"usage":{"input_tokens":11,"cache_creation_input_tokens":101,"cache_read_input_tokens":1009,"output_tokens":19}}}
SESSION
subagent_path="$ROOT/home/.claude/projects/-invented-violet/73100000-0000-4000-8000-000000000731/subagents/agent-a35b593a6be7a9ba5.jsonl"
mkdir -p "$(dirname "$subagent_path")"
cat >"$subagent_path" <<'SUBAGENT'
{"parentUuid":null,"isSidechain":true,"agentId":"a35b593a6be7a9ba5","type":"user","uuid":"invented-subagent-prompt","message":{"role":"user","content":"Invented real-shape Task prompt."}}
{"parentUuid":"invented-subagent-prompt","isSidechain":true,"agentId":"a35b593a6be7a9ba5","type":"assistant","uuid":"invented-subagent-answer","message":{"role":"assistant","content":[{"type":"text","text":"Invented real-shape Task answer."}]}}
SUBAGENT

socket="$ROOT/herdr.sock"
python3 - "$socket" "$ROOT/snapshot.json" <<'PY' &
import json
import socket
import sys

socket_path, fixture_path = sys.argv[1:]
server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
server.bind(socket_path)
server.listen()
while True:
    connection, _ = server.accept()
    with connection:
        with open(fixture_path) as source:
            snapshot = json.load(source)
        request = json.loads(connection.makefile("rb").readline())
        response = {"id": request.get("id"), "result": {"type": "session_snapshot", "snapshot": snapshot}}
        connection.sendall((json.dumps(response, separators=(",", ":")) + "\n").encode())
PY
socket_pid=$!

for _ in 1 2 3 4 5 6 7 8 9 10; do
  [ -S "$socket" ] && break
  sleep 0.05
done

port="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
PATH="$ROOT/bin:/usr/bin:/bin" \
HOME="$ROOT/home" \
XDG_CACHE_HOME="$ROOT/cache" \
WEB_SERVE_ROSTER="$ROOT/roster.json" \
WEB_SEND_LOG="$ROOT/send.log" \
WEB_SEND_CALLS="$ROOT/send.calls" \
WEB_SPAWN_LOG="$ROOT/spawn.log" \
WEB_SERVE_SNAPSHOT="$ROOT/snapshot.json" \
WEB_WHOIS_MODE="$ROOT/whois.mode" \
AI_CONFIG_ROOT="$ROOT/action-root" \
HERDER_HERDR_SOCKET="$socket" \
HERDER_SERVE_TEST_LOOPBACK_ONLY=1 \
  "$ROOT/herder" serve --port "$port" >"$ROOT/serve.out" 2>"$ROOT/serve.err" &
server_pid=$!

ready=0
for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
  if curl -fsS "http://127.0.0.1:$port/api/fleet" >"$ROOT/fleet.json"; then
    ready=1
    break
  fi
  sleep 0.05
done
if [ "$ready" -eq 1 ]; then
  pass "serve starts test-scoped on loopback and GET /api/fleet responds"
else
  bad "serve startup" "stdout=$(cat "$ROOT/serve.out") stderr=$(cat "$ROOT/serve.err")"
fi

if curl -fsS "http://127.0.0.1:$port/" >"$ROOT/index.html" &&
  grep -qF '<title>Herder fleet</title>' "$ROOT/index.html" &&
  asset="$(sed -n 's|.*src="\(/assets/[^\"]*\.js\)".*|\1|p' "$ROOT/index.html")" &&
  [ -n "$asset" ] && curl -fsS "http://127.0.0.1:$port$asset" >"$ROOT/app.js" &&
  grep -qF '/api/events' "$ROOT/app.js" &&
  grep -qF 'entry:' "$ROOT/app.js" &&
  ! grep -qF 'exchange:' "$ROOT/app.js" &&
  grep -qF '/entries' "$ROOT/app.js" &&
  grep -qF 'nextOffset' "$ROOT/app.js" &&
  grep -qF '% left' "$ROOT/app.js" &&
  grep -qF 'waiting for the agent’s next turn' "$ROOT/app.js" &&
  grep -qF 'queued-messages' "$ROOT/app.js" &&
  grep -qF 'clean view' "$ROOT/app.js" &&
  grep -qF 'herder.web.cleanView.v1:' "$ROOT/app.js" &&
  grep -qF 'show system entries' "$ROOT/app.js" &&
  grep -qF 'react-markdown' "$WEB_ROOT/package-lock.json" &&
  grep -qF 'remark-gfm' "$WEB_ROOT/package-lock.json" &&
  grep -qF 'Live stream timed out' "$ROOT/app.js" &&
  ! grep -qF '/transcript/stream' "$ROOT/app.js" &&
  grep -qF '/message' "$ROOT/app.js" &&
  grep -qF '/api/spawn' "$ROOT/app.js" &&
  ! grep -qF '/fork' "$ROOT/app.js" &&
  ! grep -qF 'Fork agent' "$ROOT/app.js" &&
  grep -qF 'placement pending' "$ROOT/app.js" &&
  grep -qF '/agents/' "$ROOT/app.js" &&
  grep -qF 'herder.web.layout.v1' "$ROOT/app.js" &&
  grep -qF 'Preview — double-click to pin' "$ROOT/app.js" &&
  grep -qF 'Fleet sidebar' "$ROOT/app.js" &&
  grep -qF '@headless-tree/react' "$WEB_ROOT/package-lock.json" &&
  grep -qF 'customPrimaryActionEnter' "$ROOT/app.js" &&
  grep -qF 'customPrimaryActionSpace' "$ROOT/app.js" &&
  grep -qF 'knownWorkspaceItems' "$ROOT/app.js" &&
  grep -qF 'Shortcuts: Ctrl/Cmd+W close tab' "$ROOT/app.js" &&
  grep -qF 'Collapse' "$ROOT/app.js" &&
  grep -qF 'aria-level' "$ROOT/app.js" &&
  grep -qF 'aria-expanded' "$ROOT/app.js" &&
  ! grep -qF 'tree-tab-separator' "$ROOT/app.js" &&
  curl -fsS "http://127.0.0.1:$port/agents/mavu" | grep -qF '<title>Herder fleet</title>'; then
  pass "serve delivers built shell/sidebar with ephemeral preview tabs, pin affordance, keyboard activation, unseen-workspace expansion, pinned layout persistence, valid tree levels, board/agent routes, and direct SPA navigation"
else
  bad "embedded UI" "index=$(cat "$ROOT/index.html" 2>/dev/null || true)"
fi

if curl -fsS "http://127.0.0.1:$port/api/agents/vile" >"$ROOT/vitals.json" && python3 - "$ROOT/vitals.json" <<'PY'
import json, sys
agent = json.load(open(sys.argv[1]))
assert agent["model"] == "invented-claude-model"
assert agent["context_usage"] == {
    "used_tokens": 1121,
    "input_tokens": 11,
    "cache_creation_input_tokens": 101,
    "cache_read_input_tokens": 1009,
    "output_tokens": 19,
}
PY
then
  pass "agent detail derives latest Claude model and raw context usage without a guessed window"
else
  bad "agent vitals detail" "body=$(cat "$ROOT/vitals.json" 2>/dev/null || true)"
fi

if python3 - "$ROOT/fleet.json" <<'PY'
import json, sys
board = json.load(open(sys.argv[1]))
pane = board["workspaces"][0]["tabs"][0]["panes"][0]
assert pane["pane_id"] == "w1:p1"
assert pane["agent"] == "mavu"
assert pane["tool"] == "codex"
assert pane["gap"] == "-"
assert "worktree_of" not in board["workspaces"][0]
assert board["workspaces"][1]["worktree_of"] == "w1"
assert board["workspaces"][1]["tabs"][0]["panes"][0]["agent"] == "zira"
assert board["unplaced"][0]["agent"] == "vile"
assert [child["agent"] for child in board["unplaced"][0]["subagents"]] == ["vile_general_purpose_1"]
assert board["unplaced"][0]["subagents"][0]["parent_agent"] == "vile"
assert all(row["agent"] != "vile_general_purpose_1" for row in board["unplaced"])
PY
then
  pass "fleet JSON preserves hierarchy, exact placement, and explicit subagent families"
else
  bad "fleet JSON contract" "body=$(cat "$ROOT/fleet.json")"
fi

if curl -fsS "http://127.0.0.1:$port/api/agents/mavu" >"$ROOT/agent.json" && python3 - "$ROOT/agent.json" <<'PY'
import json, sys
agent = json.load(open(sys.argv[1]))
assert agent["name"] == "mavu"
assert agent["tool"] == "codex"
assert agent["herdr_status"] != "-"
assert agent["bus_status"] == "listening"
assert agent["gap"] == "-"
assert agent["pane"] == {"workspace_id":"w1", "tab_id":"t1", "pane_id":"w1:p1"}
assert agent["launch_context"]["pane_id"] == "w1:p1"
PY
then
  pass "agent detail returns pane coordinate, tool, statuses, launch context, and gap"
else
  bad "agent detail" "body=$(cat "$ROOT/agent.json" 2>/dev/null || true)"
fi

if curl -fsS "http://127.0.0.1:$port/api/viewer" >"$ROOT/viewer.json" &&
  jq -e '. == {viewer:"web-alice-example-com"}' "$ROOT/viewer.json" >/dev/null &&
  [ ! -e "$ROOT/send.calls" ]; then
  pass "viewer identity resolves on read without sending or retaining state"
else
  bad "viewer identity read" "body=$(cat "$ROOT/viewer.json" 2>/dev/null || true) send_calls=$(cat "$ROOT/send.calls" 2>/dev/null || true)"
fi

if curl -fsS "http://127.0.0.1:$port/api/agents/vile/entries?limit=1" >"$ROOT/entries.json" && python3 - "$ROOT/entries.json" <<'PY'
import json, sys
page = json.load(open(sys.argv[1]))
assert page["sessionId"] == "73100000-0000-4000-8000-000000000731"
assert page["window"]["mode"] == "tail"
assert page["window"]["limit"] == 1
assert page["window"]["from"] == page["entries"][0]["byteOffset"]
assert page["entries"][0]["uuid"] == "invented-web-answer"
assert page["entries"][0]["kind"] == "assistant_text"
assert page["nextOffset"] > page["window"]["from"]
PY
then
  pass "entries route serves a classified tail window with a session-bound byte cursor"
else
  bad "entries route" "body=$(cat "$ROOT/entries.json" 2>/dev/null || true)"
fi

if curl -fsS "http://127.0.0.1:$port/api/agents/vile_general_purpose_1/entries?limit=10" >"$ROOT/subagent-entries.json" && python3 - "$ROOT/subagent-entries.json" <<'PY'
import json, sys
page = json.load(open(sys.argv[1]))
assert page["sessionId"] == "subagent:a35b593a6be7a9ba5"
assert [entry["uuid"] for entry in page["entries"]] == ["invented-subagent-prompt", "invented-subagent-answer"]
assert page["stats"]["sidechainSkipped"] == 0
PY
then
  pass "proven subagent serves its dedicated sidechain transcript through the canonical entries route"
else
  bad "subagent entries route" "body=$(cat "$ROOT/subagent-entries.json" 2>/dev/null || true)"
fi

if curl -fsS "http://127.0.0.1:$port/api/agents/vile" >"$ROOT/queued-before.json" && python3 - "$ROOT/queued-before.json" <<'PY'
import json, sys
agent = json.load(open(sys.argv[1]))
queued = agent["queued"]
assert [item["id"] for item in queued] == [97]
assert queued[0] == {
    "id": 97,
    "sender": "web-owner",
    "intent": "request",
    "preview": "operator question",
    "sent_at": "2099-01-01T00:00:00.000001+00:00",
    "operator": True,
}
PY
then
  pass "agent detail exposes only sent-but-not-injected operator messages from bus IDs"
else
  bad "queued detail before injection" "body=$(cat "$ROOT/queued-before.json" 2>/dev/null || true)"
fi

printf '%s\n' '{"type":"attachment","attachment":{"type":"hook_additional_context","hookName":"PostToolUse:Bash","hookEvent":"PostToolUse","content":["<hcom>[request #97] web-owner → vile: operator question</hcom>"]},"uuid":"invented-queued-delivery","timestamp":"2026-01-02T03:04:06.500Z"}' >>"$session_path"
if curl -fsS "http://127.0.0.1:$port/api/agents/vile" >"$ROOT/queued-after.json" && python3 - "$ROOT/queued-after.json" <<'PY'
import json, sys
agent = json.load(open(sys.argv[1]))
assert agent.get("queued") is None
PY
then
  pass "matching authenticated transcript delivery removes the operator message; nonoperator traffic never queues"
else
  bad "queued detail after injection" "body=$(cat "$ROOT/queued-after.json" 2>/dev/null || true)"
fi

curl -sS -o "$ROOT/unknown-agent.json" -w '%{http_code}' "http://127.0.0.1:$port/api/agents/missing" >"$ROOT/unknown-agent.status"
if [ "$(cat "$ROOT/unknown-agent.status")" = 404 ] &&
  jq -e '.error == "unknown agent" and (.detail | contains("not on the hcom bus"))' "$ROOT/unknown-agent.json" >/dev/null; then
  pass "unknown bus name refuses agent detail with structured 404"
else
  bad "unknown agent refusal" "status=$(cat "$ROOT/unknown-agent.status") body=$(cat "$ROOT/unknown-agent.json")"
fi

for legacy_path in /api/agents/mavu/transcript /api/agents/mavu/transcript/stream; do
  legacy_name="$(basename "$legacy_path")"
  curl -sS -o "$ROOT/legacy-$legacy_name.json" -w '%{http_code}' \
    "http://127.0.0.1:$port$legacy_path" >"$ROOT/legacy-$legacy_name.status"
  if [ "$(cat "$ROOT/legacy-$legacy_name.status")" = 404 ] &&
    jq -e '. == {error:"not found",detail:"unknown endpoint"}' "$ROOT/legacy-$legacy_name.json" >/dev/null; then
    pass "legacy exchange endpoint $legacy_path is a structured 404"
  else
    bad "legacy exchange endpoint removal" "$legacy_path status=$(cat "$ROOT/legacy-$legacy_name.status") body=$(cat "$ROOT/legacy-$legacy_name.json")"
  fi
done

message_text="please inspect --flag 'quotes'
second line"
message_body="$(jq -cn --arg text "$message_text" '{text:$text}')"
if curl -fsS -X POST -H 'Content-Type: application/json' --data "$message_body" \
  "http://127.0.0.1:$port/api/agents/mavu/message" >"$ROOT/message.json" &&
  jq -e '. == {sent:true,to:"mavu",from:"web-alice-example-com",intent:"request"}' "$ROOT/message.json" >/dev/null &&
  [ "$(wc -l <"$ROOT/send.calls")" -eq 1 ] &&
  python3 - "$ROOT/send.log" "$message_text" <<'PY'
import sys

log_path, original = sys.argv[1:]
note = "[HERDER_WEB_OPERATOR_NOTE_BEGIN]\n[This message came from a web operator named web-alice-example-com via the fleet web view. They cannot receive hcom messages; do not reply with `hcom send`. Answer in your normal chat turn; they are watching the session transcript live.]\n[HERDER_WEB_OPERATOR_NOTE_END]"
expected = ["send", "@mavu", "--intent", "request", "--from", "web-alice-example-com", "--", note + "\n\n" + original]
raw = open(log_path).read()
actual = []
position = 0
while position < len(raw):
    assert raw[position] == "<", raw
    end = raw.index(">\n", position)
    actual.append(raw[position + 1:end])
    position = end + 2
assert actual == expected, (actual, expected)
PY
then
  pass "message write sends exactly once with web context, byte-intact text, attribution, request intent, and unchanged confirmation"
else
  bad "message write" "body=$(cat "$ROOT/message.json" 2>/dev/null || true) calls=$(cat "$ROOT/send.calls" 2>/dev/null || true) args=$(cat "$ROOT/send.log" 2>/dev/null || true)"
fi

if curl -fsS -X POST -H 'Content-Type: application/json' \
  --data '{"from_pane":"w1:p1","shape":"pane","tool":"codex","tag":"web","prompt":"quote '\'' and\n--dash"}' \
  "http://127.0.0.1:$port/api/spawn" >"$ROOT/spawn.json" &&
  jq -e '. == {name:"spawn-vava",pane:"w1:p9"}' "$ROOT/spawn.json" >/dev/null &&
  grep -qxF '<--split-from>' "$ROOT/spawn.log" && grep -qxF '<w1:p1>' "$ROOT/spawn.log" &&
  curl -fsS "http://127.0.0.1:$port/api/fleet" | jq -e '.workspaces[].tabs[].panes[] | select(.pane_id == "w1:p9" and .agent == "spawn-vava")' >/dev/null; then
  pass "contextual spawn maps same-tab to split-from, preserves argv, and appears in fleet"
else
  bad "contextual spawn" "body=$(cat "$ROOT/spawn.json" 2>/dev/null || true) args=$(cat "$ROOT/spawn.log" 2>/dev/null || true)"
fi

if [ "$(curl -sS -o "$ROOT/fork-removed.json" -w '%{http_code}' -X POST -H 'Content-Type: application/json' --data '{"prompt":"continue safely"}' \
  "http://127.0.0.1:$port/api/agents/mavu/fork")" = 404 ] &&
  jq -e '.error == "not found" and .detail == "unknown endpoint"' "$ROOT/fork-removed.json" >/dev/null; then
  pass "removed fork endpoint fails closed as an unknown endpoint"
else
  bad "fork removal" "body=$(cat "$ROOT/fork-removed.json" 2>/dev/null || true)"
fi

printf '%s\n' unresolved >"$ROOT/whois.mode"
curl -sS -o "$ROOT/unresolved.json" -w '%{http_code}' -X POST -H 'Content-Type: application/json' --data '{"text":"blocked"}' \
  "http://127.0.0.1:$port/api/agents/mavu/message" >"$ROOT/unresolved.status"
printf '%s\n' existing >"$ROOT/whois.mode"
curl -sS -o "$ROOT/existing.json" -w '%{http_code}' -X POST -H 'Content-Type: application/json' --data '{"text":"blocked"}' \
  "http://127.0.0.1:$port/api/agents/mavu/message" >"$ROOT/existing.status"
printf '%s\n' reserved >"$ROOT/whois.mode"
curl -sS -o "$ROOT/reserved.json" -w '%{http_code}' -X POST -H 'Content-Type: application/json' --data '{"text":"blocked"}' \
  "http://127.0.0.1:$port/api/agents/mavu/message" >"$ROOT/reserved.status"
if [ "$(cat "$ROOT/unresolved.status")" = 409 ] && jq -e '.error == "attribution required"' "$ROOT/unresolved.json" >/dev/null &&
  [ "$(cat "$ROOT/existing.status")" = 409 ] && jq -e '.error == "sender refused"' "$ROOT/existing.json" >/dev/null &&
  [ "$(cat "$ROOT/reserved.status")" = 409 ] && jq -e '.error == "attribution required" and (.detail | contains("reserved"))' "$ROOT/reserved.json" >/dev/null; then
  pass "unresolved, existing-agent, and reserved web senders are loudly refused"
else
  bad "sender refusals" "unresolved=$(cat "$ROOT/unresolved.status")/$(cat "$ROOT/unresolved.json") existing=$(cat "$ROOT/existing.status")/$(cat "$ROOT/existing.json") reserved=$(cat "$ROOT/reserved.status")/$(cat "$ROOT/reserved.json")"
fi

curl --max-time 6 -Ns "http://127.0.0.1:$port/api/events?agents=vile" >"$ROOT/events.out" 2>"$ROOT/events.err" &
events_pid=$!
for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
  grep -qF 'event: fleet' "$ROOT/events.out" 2>/dev/null && break
  sleep 0.05
done
printf '%s\n' '{"type":"assistant","uuid":"invented-live-answer","timestamp":"2026-01-02T03:04:07.000Z","message":{"role":"assistant","model":"invented-claude-model-latest","content":[{"type":"text","text":"Invented live endpoint answer."}],"usage":{"input_tokens":13,"cache_creation_input_tokens":103,"cache_read_input_tokens":1013,"output_tokens":23}}}' >>"$session_path"
for _ in {1..80}; do
  grep -qF 'event: entry:vile' "$ROOT/events.out" 2>/dev/null && break
  sleep 0.05
done
kill "$events_pid" 2>/dev/null || true
wait "$events_pid" 2>/dev/null || true
events="$(cat "$ROOT/events.out")"
if grep -qF 'event: fleet' <<<"$events" && grep -qF '"worktree_of":"w1"' <<<"$events" && grep -qF 'event: message' <<<"$events" && grep -qF '"text":"fixture message"' <<<"$events" &&
  grep -qF 'event: entry:vile' <<<"$events" && grep -qF '"uuid":"invented-live-answer"' <<<"$events" && grep -qF '"byteOffset":' <<<"$events" && grep -qF '"kind":"assistant_text"' <<<"$events"; then
  pass "multiplexed events SSE live-tails an endpoint-shaped immutable entry beside fleet and hcom events"
else
  bad "events SSE frames" "frames=$events stderr=$(cat "$ROOT/events.err")"
fi

if curl -fsS "http://127.0.0.1:$port/api/agents/vile" >"$ROOT/vitals-live.json" && python3 - "$ROOT/vitals-live.json" <<'PY'
import json, sys
agent = json.load(open(sys.argv[1]))
assert agent["model"] == "invented-claude-model-latest"
assert agent["context_usage"]["used_tokens"] == 1129
assert "window_tokens" not in agent["context_usage"]
assert "used_percent" not in agent["context_usage"]
PY
then
  pass "entry-cadence wake exposes moved model and context facts from the detail endpoint"
else
  bad "live agent vitals" "body=$(cat "$ROOT/vitals-live.json" 2>/dev/null || true)"
fi

kill "$socket_pid" 2>/dev/null || true
wait "$socket_pid" 2>/dev/null || true
socket_pid=""
curl -sS -o "$ROOT/refusal.json" -w '%{http_code}' "http://127.0.0.1:$port/api/fleet" >"$ROOT/refusal.status"
if [ "$(cat "$ROOT/refusal.status")" = 502 ] && python3 - "$ROOT/refusal.json" <<'PY'
import json, sys
body = json.load(open(sys.argv[1]))
assert set(body) == {"error", "detail"}
assert body["error"] == "substrate unreachable"
assert "herdr" in body["detail"] or "socket" in body["detail"]
PY
then
  pass "dead herdr socket refuses with 502 structured error, never an empty board"
else
  bad "dead-socket honesty" "status=$(cat "$ROOT/refusal.status") body=$(cat "$ROOT/refusal.json")"
fi

kill "$server_pid" 2>/dev/null || true
wait "$server_pid"
serve_rc=$?
server_pid=""
if [ "$serve_rc" -eq 0 ]; then
  pass "SIGTERM stops the test-scoped server cleanly"
else
  bad "server shutdown" "rc=$serve_rc"
fi

printf '\nSUMMARY web-serve: PASS=%d FAIL=%d\n' "$((22 - fail))" "$fail"
if [ "$fail" -ne 0 ]; then
  exit 1
fi
