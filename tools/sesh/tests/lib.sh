#!/usr/bin/env bash
# Shared library for the sesh scenario gate harnesses (U5, spec §6).
#
# House contract: every check-*.sh sources this file, runs hermetically
# (mktemp workspace, fixture session trees, REAL `sesh serve` on an ephemeral
# loopback port, REAL `sesh ship` run to quiescence), asserts by byte-compare
# and store-DB queries, and prints ALL GREEN on success. These are the
# permanent regression gate, not one-off demos.
#
# Linux-oriented (uses coreutils `truncate`, GNU stat); the M1 gate machine
# is Linux. Darwin-specific gates arrive with U9.
set -euo pipefail

SESH_TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SESH_MODULE_DIR="$(cd "$SESH_TESTS_DIR/.." && pwd)"
FIXTURES="$SESH_TESTS_DIR/fixtures"

export GOTOOLCHAIN=local

fail() {
  echo "FAIL: $*" >&2
  for log in "${WORK:-/nonexistent}"/store.log "${WORK:-/nonexistent}"/ship.log; do
    if [ -s "$log" ]; then
      echo "--- tail $log" >&2
      tail -n 15 "$log" >&2
    fi
  done
  exit 1
}

ok() { echo "ok: $*"; }
step() { echo "--- $*"; }

# --- preflight ---------------------------------------------------------------

# Toolchain preflight (check-surface-fixtures.sh precedent): GOTOOLCHAIN=local
# is deliberate — the gate must not silently download a different toolchain —
# so the go on PATH must itself satisfy go.mod. Fail with the fix instead of a
# confusing compile error.
preflight() {
  local PINNED_EXPORT='export PATH=/home/grace/.local/share/mise/installs/go/1.26.4/bin:$PATH && export GOTOOLCHAIN=local'
  local need have
  need=$(awk '/^go /{print $2; exit}' "$SESH_MODULE_DIR/go.mod")
  command -v go >/dev/null 2>&1 ||
    fail "no 'go' on PATH; this module needs go >= ${need}. Playbook-pinned toolchain: ${PINNED_EXPORT}"
  have=$(go env GOVERSION); have=${have#go}
  if [ "$(printf '%s\n' "$need" "$have" | sort -V | head -n1)" != "$need" ]; then
    fail "go ${have} on PATH is older than the go.mod requirement (${need}) and GOTOOLCHAIN=local forbids auto-download. Playbook-pinned toolchain: ${PINNED_EXPORT}"
  fi
  local dep
  for dep in curl jq python3 cmp truncate; do
    command -v "$dep" >/dev/null 2>&1 || fail "harness dependency missing: $dep"
  done
}

# --- workspace ---------------------------------------------------------------

# Workspace layout: $WORK/{bin,home,state,store,logs}. HOME is overridden so
# the REAL shipper discovers only the harness fixture tree; SESH_STATE_DIR
# pins the cursor registry inside the workspace. Nothing outside $WORK is
# read or written by the system under test.
setup_workspace() {
  WORK=$(mktemp -d "${TMPDIR:-/tmp}/sesh-gate.XXXXXX")
  HOME_DIR="$WORK/home"
  STATE_DIR="$WORK/state"
  STORE_DIR="$WORK/store"
  BIN="$WORK/bin"
  mkdir -p "$HOME_DIR" "$STATE_DIR" "$STORE_DIR" "$BIN"
  STORE_PID=""
  SHIP_PID=""
  trap cleanup_workspace EXIT
}

cleanup_workspace() {
  [ -n "${SHIP_PID}" ] && kill "$SHIP_PID" 2>/dev/null || true
  [ -n "${STORE_PID}" ] && kill "$STORE_PID" 2>/dev/null || true
  wait 2>/dev/null || true
  if [ "${SESH_GATE_KEEP:-0}" = "1" ]; then
    echo "workspace kept: $WORK" >&2
  else
    rm -rf "$WORK"
  fi
}

build_binaries() {
  (cd "$SESH_MODULE_DIR" && go build -o "$BIN/sesh" ./cmd/sesh && go build -o "$BIN/dbq" ./tests/dbq) ||
    fail "go build of sesh/dbq"
}

free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}

# --- store lifecycle ---------------------------------------------------------

# start_store [port] — real `sesh serve` on an ephemeral loopback port against
# $STORE_DIR. Reusing the port across a restart is what S9 needs; first start
# picks a free one. The surface listener (M2) always gets an ephemeral port so
# harness runs never collide on the well-known default.
start_store() {
  STORE_PORT="${1:-$(free_port)}"
  STORE_URL="http://127.0.0.1:$STORE_PORT"
  "$BIN/sesh" serve --addr "127.0.0.1:$STORE_PORT" --surface-addr "127.0.0.1:$(free_port)" \
    --data-dir "$STORE_DIR" >>"$WORK/store.log" 2>&1 &
  STORE_PID=$!
  wait_for "store to accept connections" 10 store_up
}

store_up() {
  local code
  code=$(curl -s -o /dev/null -w '%{http_code}' -H 'X-Sesh-Wire-Version: 1' \
    "$STORE_URL/v1/files/claude/00000000-0000-0000-0000-000000000000/00000000-0000-0000-0000-000000000000" || true)
  [ "$code" != "000" ] && [ -n "$code" ]
}

stop_store() {
  kill -TERM "$STORE_PID" 2>/dev/null || true
  wait "$STORE_PID" 2>/dev/null || true
  STORE_PID=""
}

kill9_store() {
  kill -9 "$STORE_PID" 2>/dev/null || true
  wait "$STORE_PID" 2>/dev/null || true
  STORE_PID=""
}

# --- shipper lifecycle -------------------------------------------------------

start_shipper() {
  HOME="$HOME_DIR" SESH_STATE_DIR="$STATE_DIR" SESH_STORE_URL="$STORE_URL" \
    "$BIN/sesh" ship >>"$WORK/ship.log" 2>&1 &
  SHIP_PID=$!
}

stop_shipper() {
  kill -TERM "$SHIP_PID" 2>/dev/null || true
  wait "$SHIP_PID" 2>/dev/null || true
  SHIP_PID=""
}

kill9_shipper() {
  kill -9 "$SHIP_PID" 2>/dev/null || true
  wait "$SHIP_PID" 2>/dev/null || true
  SHIP_PID=""
}

# --- fixture trees -----------------------------------------------------------

# claude_tree <slug> — project dir under the harness HOME; echoes its path.
claude_tree() {
  local dir="$HOME_DIR/.claude/projects/$1"
  mkdir -p "$dir"
  echo "$dir"
}

# codex_tree — dated sessions dir matching the discovery layout.
codex_tree() {
  local dir="$HOME_DIR/.codex/sessions/2026/06/26"
  mkdir -p "$dir"
  echo "$dir"
}

# fresh_uuid — filename plumbing for harness-created source files (file
# CONTENT always comes from the real fixture corpus; the name is not content).
fresh_uuid() {
  python3 -c 'import uuid; print(uuid.uuid4())'
}

# --- wire helpers ------------------------------------------------------------

wait_for() { # <description> <timeout_seconds> <predicate...>
  local desc=$1 timeout=$2
  shift 2
  local deadline=$((SECONDS + timeout))
  until "$@"; do
    [ "$SECONDS" -lt "$deadline" ] || fail "timeout (${timeout}s) waiting for: $desc"
    sleep 0.1
  done
}

recovery_json() { # <tool> <sid> <uuid>  (non-2xx → nonzero exit)
  curl -sf -H 'X-Sesh-Wire-Version: 1' "$STORE_URL/v1/files/$1/$2/$3"
}

# active_high_water <tool> <sid> <uuid> — highest generation's high_water, or
# -1 when the store has no state for the identity.
active_high_water() {
  local out
  if ! out=$(recovery_json "$@" 2>/dev/null); then
    echo -1
    return
  fi
  echo "$out" | jq -r '[.generations[]] | max_by(.generation) | .high_water'
}

quiesced_at() { # <tool> <sid> <uuid> <size>
  [ "$(active_high_water "$1" "$2" "$3")" = "$4" ]
}

# wait_quiesced <tool> <sid> <uuid> <source_file> — real shipper reaches the
# source's current size.
wait_quiesced() {
  local size
  size=$(stat -c %s "$4")
  wait_for "shipper quiescence on $1/$3 at $size bytes" 30 quiesced_at "$1" "$2" "$3" "$size"
}

# put_bytes <tool> <sid> <uuid> <offset> <bodyfile> — direct wire PUT (the
# wire is curl-debuggable by contract). Prints the HTTP status; response body
# lands in $WORK/last-put.json.
put_bytes() {
  curl -s -o "$WORK/last-put.json" -w '%{http_code}' -X PUT \
    -H 'Content-Type: application/octet-stream' \
    -H 'X-Sesh-Wire-Version: 1' \
    -H "X-Sesh-Hostname: $(hostname)" \
    -H "X-Sesh-OS-User: $(id -un)" \
    --data-binary @"$5" \
    "$STORE_URL/v1/files/$1/$2/$3/bytes?offset=$4"
}

# --- assertions --------------------------------------------------------------

mirror_path() { # <tool> <sid> <uuid> <generation>
  echo "$STORE_DIR/mirror/$1/$2/$3/generation-$4.jsonl"
}

assert_mirror_equals() { # <tool> <sid> <uuid> <generation> <source_file>
  local mp
  mp=$(mirror_path "$1" "$2" "$3" "$4")
  cmp -s "$mp" "$5" || fail "mirror $mp differs from source $5 (byte-compare)"
}

dbq() { "$BIN/dbq" -db "$STORE_DIR/store.sqlite" "$1"; }

assert_db() { # <description> <sql> <expected_output>
  local got
  got=$(dbq "$2") || fail "$1: query failed: $2"
  [ "$got" = "$3" ] || fail "$1: query [$2] = [$got], want [$3]"
}

# dbq_is <sql> <expected> — predicate form of assert_db for wait_for loops
# (the live index consumes append events asynchronously after mirror ACKs).
dbq_is() { [ "$(dbq "$1" 2>/dev/null)" = "$2" ]; }

# reg_offset <tool/sid/uuid> — the registry cursor offset, or "absent".
reg_offset() {
  jq -r --arg k "$1" 'if .cursors[$k] then .cursors[$k].offset else "absent" end' \
    "$STATE_DIR/cursors.json" 2>/dev/null || echo absent
}

reg_offset_is() { [ "$(reg_offset "$1")" = "$2" ]; }

all_green() { echo "ALL GREEN"; }
