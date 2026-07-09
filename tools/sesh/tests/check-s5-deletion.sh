#!/usr/bin/env bash
# Spec §6 S5 — deletion vs retention: delete a source file (routine under
# Claude's 30-day cleanup); the shipper GCs its cursor, while the mirror and
# store bookkeeping retain the transcript. Deletion must never be confused
# with truncation (no reset, no re-ship, no new generation).
set -euo pipefail
. "$(dirname "$0")/lib.sh"

preflight
setup_workspace
build_binaries
start_store

UUID=45308169-72e6-4cbe-a05c-2a0025db055e
DIR=$(claude_tree proj-del)
SRC="$DIR/$UUID.jsonl"
cp "$FIXTURES/claude-normal.jsonl" "$SRC"
SIZE=$(stat -c %s "$SRC")
SAVED="$WORK/source-before-delete.jsonl"
cp "$SRC" "$SAVED"

step "ship to quiescence; cursor exists"
start_shipper
wait_quiesced claude "$UUID" "$UUID" "$SRC"
reg_offset_is "claude/$UUID/$UUID" "$SIZE" ||
  fail "expected a registry cursor at $SIZE before deletion"
ok "file mirrored, cursor at $SIZE"

step "delete the source; cursor GCs"
rm "$SRC"
wait_for "cursor GC after source deletion" 30 reg_offset_is "claude/$UUID/$UUID" absent
ok "registry cursor removed"

step "retention: mirror and store state survive the source"
MIRROR_SHA=$(sha256sum "$(mirror_path claude "$UUID" "$UUID" 0)" | cut -d' ' -f1)
[ "$MIRROR_SHA" = "$(sha256sum "$SAVED" | cut -d' ' -f1)" ] ||
  fail "mirror bytes no longer match the deleted source"
assert_db "store row retained at full high_water" \
  "SELECT generation, high_water FROM files WHERE file_uuid='$UUID'" "0	$SIZE"
ok "mirror outlives the source at byte parity"

step "deletion is not truncation: nothing resets, nothing re-ships"
sleep 1
assert_db "still exactly one generation" "SELECT COUNT(*) FROM files WHERE file_uuid='$UUID'" "1"
[ "$(sha256sum "$(mirror_path claude "$UUID" "$UUID" 0)" | cut -d' ' -f1)" = "$MIRROR_SHA" ] ||
  fail "mirror bytes changed after deletion (spurious re-ship)"
[ "$(active_high_water claude "$UUID" "$UUID")" = "$SIZE" ] ||
  fail "recovery high_water drifted after deletion"
ok "no reset, no re-ship, no new generation"

all_green
