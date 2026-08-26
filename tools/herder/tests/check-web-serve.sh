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
if [ "${1:-}" = f ] && [ "${2:-}" = mavu ]; then
  for arg in "$@"; do printf '<%s>\n' "$arg"; done >"$WEB_FORK_LOG"
  jq '. + [{"name":"fork-vava","base_name":"fork-vava","tool":"codex","status":"listening","joined":true,"session_id":"session-fork","launch_context":{"pane_id":"w1:p8"}}]' "$WEB_SERVE_ROSTER" >"$WEB_SERVE_ROSTER.tmp"
  mv "$WEB_SERVE_ROSTER.tmp" "$WEB_SERVE_ROSTER"
  jq '.panes += [{"pane_id":"w1:p8","workspace_id":"w1","tab_id":"t1","agent":"codex","agent_status":"active","agent_session":"session-fork"}]' "$WEB_SERVE_SNAPSHOT" >"$WEB_SERVE_SNAPSHOT.tmp"
  mv "$WEB_SERVE_SNAPSHOT.tmp" "$WEB_SERVE_SNAPSHOT"
  printf '%s\n' 'Started the fork process for 1 Codex agent' 'Names: fork-vava' 'Batch id: batch-fork'
  exit 0
fi
if [ "${1:-}" = transcript ]; then
  printf '%s\n' "$*" >>"$WEB_TRANSCRIPT_LOG"
  if [[ "${3:-}" =~ ^([0-9]+)-([0-9]+)$ ]] &&
    { [ "${4:-}" = --json ] || { [ "${4:-}" = --detailed ] && [ "${5:-}" = --json ]; }; }; then
    start="${BASH_REMATCH[1]}"
    end="${BASH_REMATCH[2]}"
    detail=''
    [ "${4:-}" != --detailed ] || detail=',"tools":["read"]'
    printf '['
    separator=''
    for ((position=start; position<=end; position++)); do
      printf '%s{"position":%d,"user":"stream %d","action":"reply %d"%s}' "$separator" "$position" "$position" "$position" "$detail"
      separator=','
    done
    printf ']\n'
    exit 0
  fi
  case " $* " in
    *" transcript mavu --last 2 --json "*)
      printf '%s\n' '[{"position":3,"user":"three","action":"reply three"},{"position":4,"user":"four","action":"reply four"}]'
      exit 0 ;;
    *" transcript mavu --last 2 --detailed --json "*)
      printf '%s\n' '[{"position":3,"user":"three","action":"reply three","tools":["read"]},{"position":4,"user":"four","action":"reply four","tools":["write"]}]'
      exit 0 ;;
    *" transcript mavu 1-2 --json "*)
      printf '%s\n' '[{"position":1,"user":"one","action":"reply one"},{"position":2,"user":"two","action":"reply two"}]'
      exit 0 ;;
    *" transcript mavu --last 1 --json "*|*" transcript mavu --last 1 --detailed --json "*)
      count=0
      [ ! -f "$WEB_STREAM_STATE" ] || count="$(/bin/cat "$WEB_STREAM_STATE")"
      position=$((4 + count))
      printf '%s\n' "$((count + 1))" >"$WEB_STREAM_STATE"
      detail=''
      [[ " $* " != *" --detailed "* ]] || detail=',"tools":["read"]'
      printf '[{"position":%d,"user":"stream %d","action":"reply %d"%s}]\n' "$position" "$position" "$position" "$detail"
      exit 0 ;;
  esac
fi
if [ "${1:-}" = send ]; then
  printf '<send>\n' >>"$WEB_SEND_CALLS"
  for arg in "$@"; do printf '<%s>\n' "$arg"; done >"$WEB_SEND_LOG"
  exit 0
fi
if [ "${1:-}" = events ]; then
  case " $* " in
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

jq 'map(if .name == "vile" then .session_id = "73100000-0000-4000-8000-000000000731" | .directory = "/invented/violet" else . end) + [{"name":"web-vile","base_name":"web-vile","tool":"codex","status":"listening","joined":true,"session_id":"session-web-vile","launch_context":{}}]' \
  "$FIXTURE/roster.json" >"$ROOT/roster.json"
cp "$FIXTURE/snapshot.json" "$ROOT/snapshot.json"
mkdir -p "$ROOT/home/.claude/projects/-invented-violet"
session_path="$ROOT/home/.claude/projects/-invented-violet/73100000-0000-4000-8000-000000000731.jsonl"
cat >"$session_path" <<'SESSION'
{"type":"user","uuid":"invented-web-human","timestamp":"2026-01-02T03:04:05.000Z","origin":{"kind":"human"},"promptSource":"typed","message":{"role":"user","content":"Invented web endpoint prompt."}}
{"type":"assistant","uuid":"invented-web-answer","timestamp":"2026-01-02T03:04:06.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Invented web endpoint answer."}]}}
SESSION

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
WEB_TRANSCRIPT_LOG="$ROOT/transcript.log" \
WEB_STREAM_STATE="$ROOT/stream.state" \
WEB_SEND_LOG="$ROOT/send.log" \
WEB_SEND_CALLS="$ROOT/send.calls" \
WEB_SPAWN_LOG="$ROOT/spawn.log" \
WEB_FORK_LOG="$ROOT/fork.log" \
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
  grep -qF 'show system entries' "$ROOT/app.js" &&
  grep -qF 'react-markdown' "$WEB_ROOT/package-lock.json" &&
  grep -qF 'remark-gfm' "$WEB_ROOT/package-lock.json" &&
  grep -qF 'Live stream timed out' "$ROOT/app.js" &&
  ! grep -qF '/transcript/stream' "$ROOT/app.js" &&
  grep -qF '/message' "$ROOT/app.js" &&
  grep -qF '/api/spawn' "$ROOT/app.js" &&
  grep -qF '/fork' "$ROOT/app.js" &&
  grep -qF 'placement pending' "$ROOT/app.js" &&
  grep -qF '/agents/' "$ROOT/app.js" &&
  grep -qF 'herder.web.layout.v1' "$ROOT/app.js" &&
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
  pass "serve delivers built shell/sidebar with keyboard activation, unseen-workspace expansion, viewer layout persistence, valid tree levels, board/agent routes, and direct SPA navigation"
else
  bad "embedded UI" "index=$(cat "$ROOT/index.html" 2>/dev/null || true)"
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
PY
then
  pass "fleet JSON preserves hierarchy, explicit worktree parent, exact-pane join, and unplaced gaps"
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

curl -sS -o "$ROOT/unknown-agent.json" -w '%{http_code}' "http://127.0.0.1:$port/api/agents/missing" >"$ROOT/unknown-agent.status"
if [ "$(cat "$ROOT/unknown-agent.status")" = 404 ] &&
  jq -e '.error == "unknown agent" and (.detail | contains("not on the hcom bus"))' "$ROOT/unknown-agent.json" >/dev/null; then
  pass "unknown bus name refuses agent detail with structured 404"
else
  bad "unknown agent refusal" "status=$(cat "$ROOT/unknown-agent.status") body=$(cat "$ROOT/unknown-agent.json")"
fi

if curl -fsS "http://127.0.0.1:$port/api/agents/mavu/transcript?limit=2" >"$ROOT/transcript-new.json" &&
  cursor="$(jq -r '.cursor' "$ROOT/transcript-new.json")" &&
  curl -fsS "http://127.0.0.1:$port/api/agents/mavu/transcript?limit=2&before=$cursor" >"$ROOT/transcript-old.json" &&
  curl -fsS "http://127.0.0.1:$port/api/agents/mavu/transcript?limit=2&detail=full" >"$ROOT/transcript-full.json" &&
  python3 - "$ROOT/transcript-new.json" "$ROOT/transcript-old.json" "$ROOT/transcript-full.json" <<'PY'
import json, sys
new, old, full = [json.load(open(path)) for path in sys.argv[1:]]
assert [item["position"] for item in new["exchanges"]] == [3, 4]
assert [item["position"] for item in old["exchanges"]] == [1, 2]
assert new["cursor"] and old["cursor"]
assert full["exchanges"][0]["tools"] == ["read"]
PY
then
  pass "transcript pages backward by exchange with opaque cursors and both detail levels"
else
  bad "transcript windows" "new=$(cat "$ROOT/transcript-new.json" 2>/dev/null || true) old=$(cat "$ROOT/transcript-old.json" 2>/dev/null || true) full=$(cat "$ROOT/transcript-full.json" 2>/dev/null || true)"
fi

stream_one="$(curl --max-time 5 -Ns "http://127.0.0.1:$port/api/agents/mavu/transcript/stream" 2>"$ROOT/transcript-stream.err" | awk '
  /^id:/ { id=$2 }
  /^event: exchange/ { exchange=1 }
  /^data:/ { if (exchange) { print id; print; exit } }
')"
stream_id="$(sed -n '1p' <<<"$stream_one")"
stream_data="$(sed -n '2p' <<<"$stream_one")"
stream_two="$(curl --max-time 3 -Ns -H "Last-Event-ID: $stream_id" "http://127.0.0.1:$port/api/agents/mavu/transcript/stream" 2>>"$ROOT/transcript-stream.err" | awk '
  /^event: exchange/ { exchange=1 }
  /^data:/ { if (exchange) { print; exit } }
')"
if grep -qF '"position":5' <<<"$stream_data" && grep -qF '"position":6' <<<"$stream_two" && [ -n "$stream_id" ]; then
  pass "per-agent transcript SSE emits incrementally and Last-Event-ID resumes"
else
  bad "transcript stream resume" "first=$stream_one second=$stream_two stderr=$(cat "$ROOT/transcript-stream.err")"
fi

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

if curl -fsS -X POST -H 'Content-Type: application/json' --data '{"prompt":"continue safely"}' \
  "http://127.0.0.1:$port/api/agents/mavu/fork" >"$ROOT/fork.json" &&
  jq -e '. == {name:"fork-vava",pane:"w1:p8"}' "$ROOT/fork.json" >/dev/null &&
  grep -qxF '<f>' "$ROOT/fork.log" && grep -qxF '<mavu>' "$ROOT/fork.log" && grep -qxF '<--hcom-prompt>' "$ROOT/fork.log" &&
  curl -fsS "http://127.0.0.1:$port/api/fleet" | jq -e '.workspaces[].tabs[].panes[] | select(.pane_id == "w1:p8" and .agent == "fork-vava")' >/dev/null; then
  pass "fork wraps hcom with prompt and returns live placement visible in fleet"
else
  bad "fork" "body=$(cat "$ROOT/fork.json" 2>/dev/null || true) args=$(cat "$ROOT/fork.log" 2>/dev/null || true)"
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

if ! grep -Eq '^transcript mavu (--last (1|2)|[1-6]-[1-6])' "$ROOT/transcript.log" || grep -Eq 'transcript mavu (--last [0-9]{4,}|--json$)' "$ROOT/transcript.log"; then
  bad "bounded transcript reads" "calls=$(cat "$ROOT/transcript.log")"
else
  pass "transcript substrate reads stay bounded; no whole-session invocation occurs"
fi

curl --max-time 6 -Ns "http://127.0.0.1:$port/api/events?agents=vile" >"$ROOT/events.out" 2>"$ROOT/events.err" &
events_pid=$!
for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
  grep -qF 'event: fleet' "$ROOT/events.out" 2>/dev/null && break
  sleep 0.05
done
printf '%s\n' '{"type":"assistant","uuid":"invented-live-answer","timestamp":"2026-01-02T03:04:07.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Invented live endpoint answer."}]}}' >>"$session_path"
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

printf '\nSUMMARY web-serve: PASS=%d FAIL=%d\n' "$((17 - fail))" "$fail"
if [ "$fail" -ne 0 ]; then
  exit 1
fi
