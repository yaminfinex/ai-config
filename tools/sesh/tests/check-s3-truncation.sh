#!/usr/bin/env bash
# Spec §6 S3 — truncation: truncate a watched file mid-ship; the shipper
# resets and re-ships; no infinite re-ingest loop (the filebeat #38070
# failure: truncation detected but the cursor never reset). Then diverge the
# regrown content: the mirror must keep the original history on generation 0
# and land the new history on generation 1 (wire conflict handshake).
set -euo pipefail
. "$(dirname "$0")/lib.sh"

preflight
setup_workspace
build_binaries
start_store

UUID=45308169-72e6-4cbe-a05c-2a0025db055e
DIR=$(claude_tree proj-trunc)
SRC="$DIR/$UUID.jsonl"
cp "$FIXTURES/claude-normal.jsonl" "$SRC"
FULL_SIZE=$(stat -c %s "$SRC")

step "ship the full file to quiescence"
start_shipper
wait_quiesced claude "$UUID" "$UUID" "$SRC"
assert_mirror_equals claude "$UUID" "$UUID" 0 "$SRC"
GEN0_SHA=$(sha256sum "$(mirror_path claude "$UUID" "$UUID" 0)" | cut -d' ' -f1)
ok "full file mirrored ($FULL_SIZE bytes)"

step "truncate the source mid-ship (still above the 1024-byte fingerprint window)"
TRUNC=2000
truncate -s "$TRUNC" "$SRC"
wait_for "cursor reset to truncated size $TRUNC" 30 reg_offset_is "claude/$UUID/$UUID" "$TRUNC"
ok "shipper detected size regression and reset its cursor to $TRUNC"

step "no infinite re-ingest: cursor stays put and the store keeps the longer history"
sleep 1
reg_offset_is "claude/$UUID/$UUID" "$TRUNC" ||
  fail "cursor moved after reset: $(reg_offset "claude/$UUID/$UUID") (re-ingest loop?)"
assert_db "generation 0 high_water untouched by truncation (mirror retains history)" \
  "SELECT high_water FROM files WHERE file_uuid='$UUID' AND generation=0" "$FULL_SIZE"
assert_db "still a single generation" "SELECT COUNT(*) FROM files WHERE file_uuid='$UUID'" "1"
ok "no re-ingest loop; generation 0 retained at $FULL_SIZE"

step "regrow the truncated file with DIFFERENT real bytes (divergent history)"
head -c 3000 "$FIXTURES/claude-resume-new-file.jsonl" >>"$SRC"
NEW_SIZE=$(stat -c %s "$SRC")
wait_quiesced claude "$UUID" "$UUID" "$SRC"
ok "shipper quiesced on the divergent history ($NEW_SIZE bytes) — conflict handshake bounded"

step "generations: 0 = original history intact, 1 = current source, nothing poisoned"
assert_db "two generations exist" "SELECT COUNT(*) FROM files WHERE file_uuid='$UUID'" "2"
assert_db "nothing poisoned" "SELECT COUNT(*) FROM files WHERE poisoned != 0" "0"
assert_mirror_equals claude "$UUID" "$UUID" 1 "$SRC"
[ "$(sha256sum "$(mirror_path claude "$UUID" "$UUID" 0)" | cut -d' ' -f1)" = "$GEN0_SHA" ] ||
  fail "generation 0 mirror bytes changed during conflict handling (must never happen)"
assert_db "generation 1 at the new size" \
  "SELECT high_water FROM files WHERE file_uuid='$UUID' AND generation=1" "$NEW_SIZE"
ok "both histories preserved; active generation matches the source"

all_green
