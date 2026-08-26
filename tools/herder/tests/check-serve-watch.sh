#!/usr/bin/env bash
# check-serve-watch.sh - real process coverage for both herder serve --watch
# deployment planes: the repository's source-building wrapper and a directly
# copied binary that is atomically replaced.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$TESTS_DIR/../../.." && pwd -P)"
ROOT="$(mktemp -d)"
server_pid=""

cleanup() {
  if [ -n "$server_pid" ]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf "$ROOT"
}
trap cleanup EXIT

fail=0
pass() { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=$((fail + 1)); }

free_port() {
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
}

api_answers() {
  local port="$1" body="$2" status
  status="$(curl -sS -o "$body" -w '%{http_code}' "http://127.0.0.1:$port/api/watch-probe" 2>/dev/null || true)"
  [ "$status" = 404 ] && grep -qF '"error":"not found"' "$body"
}

wait_for_api() {
  local port="$1" body="$2"
  for _ in {1..160}; do
    api_answers "$port" "$body" && return 0
    sleep 0.1
  done
  return 1
}

wait_for_reexec() {
  local log="$1" count
  for _ in {1..160}; do
    count="$(grep -cF 'herder serve: watch re-exec via ' "$log" 2>/dev/null || true)"
    [ "$count" -ge 1 ] && return 0
    sleep 0.1
  done
  return 1
}

if ! command -v go >/dev/null 2>&1 || ! command -v curl >/dev/null 2>&1 || ! command -v python3 >/dev/null 2>&1; then
  printf 'SKIP  go, curl, and python3 are required for serve watch scenarios\n'
  exit 0
fi

# Repository plane: copy the real launcher and module, start --watch through
# the launcher, then change one of its real hash inputs. The same PID re-enters
# the wrapper, which builds/selects a new content-addressed binary.
SOURCE_ROOT="$ROOT/source-plane"
mkdir -p "$SOURCE_ROOT/bin" "$SOURCE_ROOT/lib" "$SOURCE_ROOT/tools"
cp "$REPO/bin/herder" "$SOURCE_ROOT/bin/herder"
cp "$REPO/lib/common.sh" "$SOURCE_ROOT/lib/common.sh"
cp -R "$REPO/tools/herder" "$SOURCE_ROOT/tools/herder"
chmod +x "$SOURCE_ROOT/bin/herder"
source_port="$(free_port)"
mkdir -p "$SOURCE_ROOT/home" "$SOURCE_ROOT/cache" "$SOURCE_ROOT/tmp"
HOME="$SOURCE_ROOT/home" XDG_CACHE_HOME="$SOURCE_ROOT/cache" TMPDIR="$SOURCE_ROOT/tmp" \
  AI_CONFIG_ROOT="$SOURCE_ROOT" HERDER_SERVE_TEST_LOOPBACK_ONLY=1 \
  "$SOURCE_ROOT/bin/herder" serve --watch --port "$source_port" \
  >"$SOURCE_ROOT/serve.out" 2>"$SOURCE_ROOT/serve.err" &
server_pid=$!

if wait_for_api "$source_port" "$SOURCE_ROOT/before.json"; then
  pass "wrapper plane: API answers before source change"
else
  bad "wrapper plane startup" "stdout=$(cat "$SOURCE_ROOT/serve.out") stderr=$(cat "$SOURCE_ROOT/serve.err")"
fi
before_exe=""
[ ! -e "/proc/$server_pid/exe" ] || before_exe="$(readlink "/proc/$server_pid/exe")"
cp "$SOURCE_ROOT/tools/herder/cmd/herder/main.go" "$SOURCE_ROOT/main.go.good"
printf '\nthis is deliberately invalid Go\n' >>"$SOURCE_ROOT/tools/herder/cmd/herder/main.go"

if wait_for_reexec "$SOURCE_ROOT/serve.err" && wait_for_api "$source_port" "$SOURCE_ROOT/after.json"; then
	if grep -qF 'herder: rebuild failed, serving last-good ' "$SOURCE_ROOT/serve.err"; then
		pass "wrapper plane: failed watched rebuild returns on the last-good binary"
	else
		bad "wrapper plane last-good" "$(cat "$SOURCE_ROOT/serve.err")"
	fi
else
	bad "wrapper plane last-good reload" "stdout=$(cat "$SOURCE_ROOT/serve.out") stderr=$(cat "$SOURCE_ROOT/serve.err")"
fi

cp "$SOURCE_ROOT/main.go.good" "$SOURCE_ROOT/tools/herder/cmd/herder/main.go"
printf '\n// serve-watch fixture change\n' >>"$SOURCE_ROOT/tools/herder/cmd/herder/main.go"
for _ in {1..160}; do
	[ "$(grep -cF 'herder serve: watch re-exec via ' "$SOURCE_ROOT/serve.err" 2>/dev/null || true)" -ge 2 ] && break
	sleep 0.1
done
if [ "$(grep -cF 'herder serve: watch re-exec via ' "$SOURCE_ROOT/serve.err" 2>/dev/null || true)" -ge 2 ] &&
	wait_for_api "$source_port" "$SOURCE_ROOT/after-valid.json"; then
  pass "wrapper plane: stable source change re-execs and API answers afterward"
else
  bad "wrapper plane reload" "stdout=$(cat "$SOURCE_ROOT/serve.out") stderr=$(cat "$SOURCE_ROOT/serve.err")"
fi
after_exe=""
[ ! -e "/proc/$server_pid/exe" ] || after_exe="$(readlink "/proc/$server_pid/exe")"
if [ -z "$before_exe" ] || [ -z "$after_exe" ] || [ "$before_exe" != "$after_exe" ]; then
  pass "wrapper plane: running executable advances to the new content hash"
else
  bad "wrapper plane executable" "still $after_exe"
fi
if grep -qF 'watch started on source tree' "$SOURCE_ROOT/serve.err" &&
  grep -qF 'watch change detected' "$SOURCE_ROOT/serve.err" &&
  grep -qF 'watch re-exec via' "$SOURCE_ROOT/serve.err"; then
  pass "wrapper plane: terminal log tells the watch/reload story"
else
  bad "wrapper plane logs" "$(cat "$SOURCE_ROOT/serve.err")"
fi
kill "$server_pid" 2>/dev/null || true
wait "$server_pid" 2>/dev/null || true
server_pid=""

# Direct plane: with no wrapper marker, watch the resolved executable itself.
# Atomic rename models how installed binaries are normally replaced.
DIRECT_ROOT="$ROOT/direct-plane"
mkdir -p "$DIRECT_ROOT"
(cd "$REPO/tools/herder" && go build -o "$DIRECT_ROOT/herder" ./cmd/herder)
cp "$DIRECT_ROOT/herder" "$DIRECT_ROOT/herder.next"
direct_port="$(free_port)"
env -u HERDER_WATCH_SOURCE_DIR -u HERDER_WATCH_LAUNCHER \
  HERDER_SERVE_TEST_LOOPBACK_ONLY=1 "$DIRECT_ROOT/herder" serve --watch --port "$direct_port" \
  >"$DIRECT_ROOT/serve.out" 2>"$DIRECT_ROOT/serve.err" &
server_pid=$!
if wait_for_api "$direct_port" "$DIRECT_ROOT/before.json"; then
  mv "$DIRECT_ROOT/herder.next" "$DIRECT_ROOT/herder"
  if wait_for_reexec "$DIRECT_ROOT/serve.err" && wait_for_api "$direct_port" "$DIRECT_ROOT/after.json"; then
    pass "direct plane: replaced copied binary re-execs and API answers afterward"
  else
    bad "direct plane reload" "stdout=$(cat "$DIRECT_ROOT/serve.out") stderr=$(cat "$DIRECT_ROOT/serve.err")"
  fi
else
  bad "direct plane startup" "stdout=$(cat "$DIRECT_ROOT/serve.out") stderr=$(cat "$DIRECT_ROOT/serve.err")"
fi
kill "$server_pid" 2>/dev/null || true
wait "$server_pid" 2>/dev/null || true
server_pid=""

printf '\nSUMMARY serve-watch: PASS=%d FAIL=%d\n' "$((6 - fail))" "$fail"
[ "$fail" -eq 0 ]
