#!/usr/bin/env bash
# Spec §6 S6a/S6b — SESSION_OWNER correlation against the REAL /proc (U9):
# live-style fixture processes, real store, real shipper.
#   S6a  codex: a process holding the rollout file open with SESSION_OWNER
#        exported stamps exactly (fd join, not inference).
#   S6b  claude: two same-cwd processes with different owners → honest
#        absence; one alone → its owner stamps.
# Linux-only by nature (/proc); darwin's S11 side is the cross-compile gate
# plus the build-tagged unit test.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

[ "$(uname -s)" = "Linux" ] || fail "S6 correlation harness requires Linux (/proc)"

preflight
setup_workspace
build_binaries

# Fixture processes must outlive the assertions but never the machine:
# every spawn is under timeout(1) and killed again on exit.
FIXTURE_PIDS=()
cleanup_fixture_procs() {
  for p in ${FIXTURE_PIDS[@]+"${FIXTURE_PIDS[@]}"}; do kill "$p" 2>/dev/null || true; done
  cleanup_workspace
}
trap cleanup_fixture_procs EXIT

# spawn_claude <cwd> <owner> — a process whose comm is "claude" (symlink to
# sleep, exec'd via the symlink), cwd'd into the cohort dir, with
# SESSION_OWNER in its environ. Echoes its pid.
ln -s "$(command -v sleep)" "$BIN/claude"
spawn_claude() {
  (cd "$1" && SESSION_OWNER="$2" exec "$BIN/claude" 120) >/dev/null 2>&1 &
  echo $!
}

munge() { printf '%s' "$1" | sed 's/[^A-Za-z0-9]/-/g'; }

start_store

step "S6a: codex fixture process holds a rollout open with SESSION_OWNER=alice"
CODEX_UUID=$(fresh_uuid)
DIR_CODEX=$(codex_tree)
CODEX_SRC="$DIR_CODEX/rollout-2026-06-26T02-43-06-$CODEX_UUID.jsonl"
cp "$FIXTURES/codex-rollout-meta.jsonl" "$CODEX_SRC"
SESSION_OWNER=alice timeout 120 tail -f "$CODEX_SRC" >/dev/null 2>&1 &
FIXTURE_PIDS+=($!)

step "S6b: one lone claude (owner bob) and one collided cohort (carol vs dave)"
SOLO_CWD="$WORK/solo.work-tree" && mkdir -p "$SOLO_CWD"
DUP_CWD="$WORK/dup.work-tree" && mkdir -p "$DUP_CWD"
SOLO_UUID=$(fresh_uuid)
DUP_UUID=$(fresh_uuid)
DIR_SOLO=$(claude_tree "$(munge "$SOLO_CWD")")
DIR_DUP=$(claude_tree "$(munge "$DUP_CWD")")
cp "$FIXTURES/claude-normal.jsonl" "$DIR_SOLO/$SOLO_UUID.jsonl"
cp "$FIXTURES/claude-resume-original.jsonl" "$DIR_DUP/$DUP_UUID.jsonl"
FIXTURE_PIDS+=("$(spawn_claude "$SOLO_CWD" bob)")
FIXTURE_PIDS+=("$(spawn_claude "$DUP_CWD" carol)")
FIXTURE_PIDS+=("$(spawn_claude "$DUP_CWD" dave)")

step "processes alive first; the real shipper's first pass correlates + ships"
start_shipper
wait_quiesced codex  "$CODEX_UUID" "$CODEX_UUID" "$CODEX_SRC"
wait_quiesced claude "$SOLO_UUID"  "$SOLO_UUID"  "$DIR_SOLO/$SOLO_UUID.jsonl"
wait_quiesced claude "$DUP_UUID"   "$DUP_UUID"   "$DIR_DUP/$DUP_UUID.jsonl"
assert_mirror_equals codex  "$CODEX_UUID" "$CODEX_UUID" 0 "$CODEX_SRC"
assert_mirror_equals claude "$SOLO_UUID"  "$SOLO_UUID"  0 "$DIR_SOLO/$SOLO_UUID.jsonl"
assert_mirror_equals claude "$DUP_UUID"   "$DUP_UUID"   0 "$DIR_DUP/$DUP_UUID.jsonl"
ok "all three fixture files mirrored to parity"

step "facts: codex exact stamp"
assert_db "codex owner is alice (fd-exact)" \
  "SELECT DISTINCT session_owner FROM fact_observations WHERE file_uuid='$CODEX_UUID' AND session_owner IS NOT NULL" \
  "alice"

step "facts: lone claude cohort stamps"
assert_db "solo claude owner is bob (unanimous cohort of one)" \
  "SELECT DISTINCT session_owner FROM fact_observations WHERE file_uuid='$SOLO_UUID' AND session_owner IS NOT NULL" \
  "bob"

step "facts: collided claude cohort is honest absence"
assert_db "dup cohort observed the file" \
  "SELECT EXISTS(SELECT 1 FROM fact_observations WHERE file_uuid='$DUP_UUID')" "1"
assert_db "dup cohort stamped NO owner (carol vs dave collision)" \
  "SELECT COUNT(*) FROM fact_observations WHERE file_uuid='$DUP_UUID' AND session_owner IS NOT NULL" \
  "0"
ok "guessing suppressed: collision renders as absence, never a pick"

step "no environ error spam in the shipper log"
if grep -qi "environ" "$WORK/ship.log"; then
  fail "shipper log mentions environ (S7/I9 requires silent skips): $(grep -i environ "$WORK/ship.log" | head -3)"
fi
ok "ship.log clean of environ errors"

all_green
