#!/usr/bin/env bash
# Spec §6 S4 — move: `/cd` relocates a live session file to another project
# directory; no duplicate session appears and bytes keep flowing. Identity is
# session uuid + fingerprint, never path/inode: the moved file must keep its
# single store identity, single generation, and reach byte parity with bytes
# appended AFTER the move.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

preflight
setup_workspace
build_binaries
start_store

UUID=e1be75ad-151b-47fa-9d69-46de1c117843
DIR_A=$(claude_tree proj-before-cd)
SRC_A="$DIR_A/$UUID.jsonl"
SPLIT=300000 # ship this real prefix before the move; append the rest after
head -c "$SPLIT" "$FIXTURES/claude-resume-new-file.jsonl" >"$SRC_A"

step "ship the pre-move prefix to quiescence"
start_shipper
wait_quiesced claude "$UUID" "$UUID" "$SRC_A"
ok "prefix mirrored at $SPLIT bytes"

step "/cd: move the live file to a new project directory"
DIR_B=$(claude_tree proj-after-cd)
SRC_B="$DIR_B/$UUID.jsonl"
mv "$SRC_A" "$SRC_B"

step "bytes keep flowing: append the remainder at the new location"
tail -c +"$((SPLIT + 1))" "$FIXTURES/claude-resume-new-file.jsonl" >>"$SRC_B"
cmp -s "$SRC_B" "$FIXTURES/claude-resume-new-file.jsonl" || fail "harness self-check: rebuilt source != fixture"
wait_quiesced claude "$UUID" "$UUID" "$SRC_B"
assert_mirror_equals claude "$UUID" "$UUID" 0 "$SRC_B"
ok "post-move appends shipped to full parity"

step "no duplicate session: one identity, one generation, no conflicts"
assert_db "single store row for the moved file" \
  "SELECT COUNT(*) FROM files WHERE file_uuid='$UUID'" "1"
assert_db "still generation 0 (move is not a conflict)" \
  "SELECT generation FROM files WHERE file_uuid='$UUID'" "0"
assert_db "no other identity appeared" "SELECT COUNT(*) FROM files" "1"
[ "$(jq -r '.cursors | length' "$STATE_DIR/cursors.json")" = "1" ] ||
  fail "registry grew a second cursor for a moved file (path leaked into identity)"
ok "single identity across the move; path stayed advisory"

all_green
