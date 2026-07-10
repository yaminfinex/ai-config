#!/usr/bin/env bash
# Verify the just install surface, including comparison with a running shipper.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

preflight
command -v just >/dev/null 2>&1 || fail "harness dependency missing: just"
setup_workspace
build_binaries

step "versions reports the executable behind a running user service"
STUB_BIN="$WORK/stub-bin"
mkdir -p "$STUB_BIN"
printf '#!/usr/bin/env sh\nprintf "%%s\\n" "$TASK_PID"\n' >"$STUB_BIN/systemctl"
chmod +x "$STUB_BIN/systemctl"

HOME="$HOME_DIR" SESH_STATE_DIR="$WORK/state" SESH_STORE_URL=http://127.0.0.1:9 \
  "$BIN/sesh" ship >"$WORK/ship.log" 2>&1 &
SHIP_PID=$!
cleanup_ship() {
  kill "$SHIP_PID" 2>/dev/null || true
  wait "$SHIP_PID" 2>/dev/null || true
  cleanup_workspace
}
trap cleanup_ship EXIT
kill -0 "$SHIP_PID" 2>/dev/null || fail "scratch shipper did not remain running"

PATH="$STUB_BIN:$PATH" TASK_PID="$SHIP_PID" just --justfile "$SESH_MODULE_DIR/justfile" \
  --working-directory "$SESH_MODULE_DIR" versions >"$WORK/versions.out"
RUNNING_VERSION=$("$BIN/sesh" version)
grep -q "^running  $RUNNING_VERSION$" "$WORK/versions.out" ||
  fail "versions missed the running binary: $(cat "$WORK/versions.out")"
ok "versions resolved /proc/$SHIP_PID/exe and printed $RUNNING_VERSION"

all_green
