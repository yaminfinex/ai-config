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
  if npm --prefix "$WEB_ROOT" ci &&
    npm --prefix "$WEB_ROOT" run lint &&
    npm --prefix "$WEB_ROOT" run typecheck &&
    npm --prefix "$WEB_ROOT" run build; then
    pass "web dependencies install cleanly and lint, typecheck, and production build pass"
  else
    bad "web build gates" "npm UI checks failed"
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

socket="$ROOT/herdr.sock"
python3 - "$socket" "$FIXTURE/snapshot.json" <<'PY' &
import json
import socket
import sys

socket_path, fixture_path = sys.argv[1:]
with open(fixture_path) as source:
    snapshot = json.load(source)
server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
server.bind(socket_path)
server.listen()
while True:
    connection, _ = server.accept()
    with connection:
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
WEB_SERVE_ROSTER="$FIXTURE/roster.json" \
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
  [ -n "$asset" ] && curl -fsS "http://127.0.0.1:$port$asset" | grep -qF '/api/events'; then
  pass "serve delivers the embedded production UI and its hashed JavaScript asset"
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
assert board["unplaced"][0]["agent"] == "vile"
PY
then
  pass "fleet JSON preserves hierarchy, exact-pane join, and unplaced gaps"
else
  bad "fleet JSON contract" "body=$(cat "$ROOT/fleet.json")"
fi

events="$(curl --max-time 3 -Ns "http://127.0.0.1:$port/api/events" 2>"$ROOT/events.err" | awk '
  /^event:/ { print }
  /^data:/ { print; seen++; if (seen == 2) exit }
')"
if grep -qF 'event: fleet' <<<"$events" && grep -qF 'event: message' <<<"$events" && grep -qF '"text":"fixture message"' <<<"$events"; then
  pass "events SSE begins with fleet snapshot then carries an hcom message"
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

printf '\nSUMMARY web-serve: PASS=%d FAIL=%d\n' "$((7 - fail))" "$fail"
if [ "$fail" -ne 0 ]; then
  exit 1
fi
