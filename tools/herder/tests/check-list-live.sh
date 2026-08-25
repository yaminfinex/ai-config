#!/usr/bin/env bash
# Hermetic contract for the live herdr-socket + hcom-roster join.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
HERDER_ROOT="$REPO_ROOT/tools/herder"
FIXTURE="$TESTS_DIR/fixtures/list-live"
GOLDEN="$TESTS_DIR/goldens/list-live.txt"
GO_MOD="$HERDER_ROOT/go.mod"
GO_VERSION="$(awk '$1 == "go" {print $2; exit}' "$GO_MOD")"
TOOLCHAIN="$(awk '$1 == "toolchain" {print $2; exit}' "$GO_MOD")"
ROOT="$(mktemp -d)"
server_pid=""
cleanup() {
  [ -z "$server_pid" ] || kill "$server_pid" 2>/dev/null || true
  rm -rf "$ROOT"
}
trap cleanup EXIT

gate_fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

[ -n "$GO_VERSION" ] || gate_fail "cannot read Go version from $GO_MOD"
[ -z "$TOOLCHAIN" ] || [ "$TOOLCHAIN" = "go$GO_VERSION" ] ||
  gate_fail "go.mod declares toolchain $TOOLCHAIN but pins go $GO_VERSION"
GO_ROOT="$(mise where "go@$GO_VERSION")" || gate_fail "go@$GO_VERSION is unavailable through mise"
GO_BIN="$GO_ROOT/bin/go"
GO_HAVE="$(env -u GOROOT GOTOOLCHAIN=local "$GO_BIN" env GOVERSION 2>/dev/null)" ||
  gate_fail "cannot execute pinned go@$GO_VERSION"
GO_HAVE="${GO_HAVE#go}"
[ "$GO_HAVE" = "$GO_VERSION" ] || gate_fail "resolved Go $GO_HAVE, want $GO_VERSION"

fail=0
pass() { printf 'PASS  %s\n' "$1"; }
bad()  { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=$((fail + 1)); }

mkdir -p "$ROOT/bin" "$ROOT/home" "$ROOT/cache"
if ! (cd "$HERDER_ROOT" && env -u GOROOT GOTOOLCHAIN=local "$GO_BIN" build -o "$ROOT/herder" ./cmd/herder); then
  printf 'FAIL  could not build herder\n'
  exit 1
fi

cat >"$ROOT/bin/hcom" <<'HCOM'
#!/usr/bin/env bash
if [ "${1:-}" = list ] && [ "${2:-}" = --json ]; then
  exec /bin/cat "$LIST_LIVE_ROSTER"
fi
printf 'unexpected hcom args: %s\n' "$*" >&2
exit 2
HCOM
chmod +x "$ROOT/bin/hcom"

socket="$ROOT/herdr.sock"
python3 - "$socket" "$FIXTURE/snapshot.json" <<'PY' &
import json
import os
import socket
import sys

socket_path, fixture_path = sys.argv[1:]
with open(fixture_path) as source:
    snapshot = json.load(source)
server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
server.bind(socket_path)
server.listen(1)
connection, _ = server.accept()
with connection:
    request = json.loads(connection.makefile("rb").readline())
    if request.get("method") != "session.snapshot":
        raise SystemExit("unexpected method")
    response = {
        "id": request.get("id"),
        "result": {"type": "session_snapshot", "snapshot": snapshot},
    }
    connection.sendall((json.dumps(response, separators=(",", ":")) + "\n").encode())
server.close()
PY
server_pid=$!

for _ in 1 2 3 4 5 6 7 8 9 10; do
  [ -S "$socket" ] && break
  sleep 0.05
done
if [ ! -S "$socket" ]; then
  printf 'FAIL  fake herdr socket did not start\n'
  exit 1
fi

PATH="$ROOT/bin:/usr/bin:/bin" \
HOME="$ROOT/home" \
XDG_CACHE_HOME="$ROOT/cache" \
LIST_LIVE_ROSTER="$FIXTURE/roster.json" \
HERDER_HERDR_SOCKET="$socket" \
  "$ROOT/herder" list >"$ROOT/actual" 2>"$ROOT/list.err"
rc=$?
wait "$server_pid"
server_pid=""

if [ "$rc" -eq 0 ] && [ ! -s "$ROOT/list.err" ]; then
  pass "live list reads fake herdr socket and hcom roster"
else
  bad "live list reads fake inputs" "rc=$rc stderr=$(cat "$ROOT/list.err")"
fi

if diff -u "$GOLDEN" "$ROOT/actual" >"$ROOT/list.diff"; then
  pass "exact-pane join output matches golden"
else
  bad "exact-pane join output" "$(cat "$ROOT/list.diff")"
fi

if grep -qF 'no bus row' "$ROOT/actual" && grep -qF 'no visible pane' "$ROOT/actual"; then
  pass "both placement gap directions remain visible"
else
  bad "placement gaps" "one or both gap labels are missing"
fi

PATH="$ROOT/bin:/usr/bin:/bin" \
HOME="$ROOT/home" \
LIST_LIVE_ROSTER="$FIXTURE/roster.json" \
HERDER_HERDR_SOCKET="$ROOT/missing.sock" \
  "$ROOT/herder" list >"$ROOT/fail.out" 2>"$ROOT/fail.err"
failure_rc=$?
if [ "$failure_rc" -ne 0 ] && grep -qF 'cannot read live herdr snapshot' "$ROOT/fail.err"; then
  pass "unreachable herdr socket fails with honest diagnostic"
else
  bad "unreachable herdr socket diagnostic" "rc=$failure_rc stderr=$(cat "$ROOT/fail.err")"
fi

printf '\nSUMMARY list-live: PASS=%d FAIL=%d\n' "$((4 - fail))" "$fail"
[ "$fail" -eq 0 ]
