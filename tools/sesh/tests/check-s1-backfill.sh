#!/usr/bin/env bash
# Spec §6 S1 — backfill parity: install the shipper on a node with
# pre-existing sessions; byte-compare mirror vs source for every file.
# Also carries the U5 shipper kill-and-restart check: SIGKILL the shipper with
# a file partially shipped, grow the file while it is dead, restart — the
# durable cursor resumes mid-file and parity holds with a single generation.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

preflight
setup_workspace
build_binaries
start_store

NORMAL_UUID=45308169-72e6-4cbe-a05c-2a0025db055e
RESUME_ORIG_UUID=2c387aef-72ac-46bc-8ea5-e3b68690a937
RESUME_NEW_UUID=e1be75ad-151b-47fa-9d69-46de1c117843
INTERLEAVED_UUID=e4578030-c4a9-493f-82e6-de6156d0179a
CODEX_UUID=019f01cf-3d22-7ea0-923e-e463b90ea31e
PARTIAL_UUID=$(fresh_uuid) # name plumbing; bytes are the real trailing-partial capture

step "pre-existing session tree (all six corpus fixtures) BEFORE the shipper exists"
DIR_A=$(claude_tree proj-a)
DIR_B=$(claude_tree proj-b)
DIR_CODEX=$(codex_tree)
cp "$FIXTURES/claude-normal.jsonl"                     "$DIR_A/$NORMAL_UUID.jsonl"
cp "$FIXTURES/claude-resume-original.jsonl"            "$DIR_B/$RESUME_ORIG_UUID.jsonl"
cp "$FIXTURES/claude-resume-new-file.jsonl"            "$DIR_B/$RESUME_NEW_UUID.jsonl"
cp "$FIXTURES/claude-interleaved-writers-standin.jsonl" "$DIR_A/$INTERLEAVED_UUID.jsonl"
cp "$FIXTURES/claude-trailing-partial.jsonl"           "$DIR_A/$PARTIAL_UUID.jsonl"
cp "$FIXTURES/codex-rollout-meta.jsonl"                "$DIR_CODEX/rollout-2026-06-26T02-43-06-$CODEX_UUID.jsonl"

step "real shipper backfills to quiescence"
start_shipper
wait_quiesced claude "$NORMAL_UUID"      "$NORMAL_UUID"      "$DIR_A/$NORMAL_UUID.jsonl"
wait_quiesced claude "$RESUME_ORIG_UUID" "$RESUME_ORIG_UUID" "$DIR_B/$RESUME_ORIG_UUID.jsonl"
wait_quiesced claude "$RESUME_NEW_UUID"  "$RESUME_NEW_UUID"  "$DIR_B/$RESUME_NEW_UUID.jsonl"
wait_quiesced claude "$INTERLEAVED_UUID" "$INTERLEAVED_UUID" "$DIR_A/$INTERLEAVED_UUID.jsonl"
wait_quiesced claude "$PARTIAL_UUID"     "$PARTIAL_UUID"     "$DIR_A/$PARTIAL_UUID.jsonl"
wait_quiesced codex  "$CODEX_UUID"       "$CODEX_UUID"       "$DIR_CODEX/rollout-2026-06-26T02-43-06-$CODEX_UUID.jsonl"
ok "all six pre-existing files reached quiescence"

step "byte-compare mirror vs source for every file"
assert_mirror_equals claude "$NORMAL_UUID"      "$NORMAL_UUID"      0 "$DIR_A/$NORMAL_UUID.jsonl"
assert_mirror_equals claude "$RESUME_ORIG_UUID" "$RESUME_ORIG_UUID" 0 "$DIR_B/$RESUME_ORIG_UUID.jsonl"
assert_mirror_equals claude "$RESUME_NEW_UUID"  "$RESUME_NEW_UUID"  0 "$DIR_B/$RESUME_NEW_UUID.jsonl"
assert_mirror_equals claude "$INTERLEAVED_UUID" "$INTERLEAVED_UUID" 0 "$DIR_A/$INTERLEAVED_UUID.jsonl"
assert_mirror_equals claude "$PARTIAL_UUID"     "$PARTIAL_UUID"     0 "$DIR_A/$PARTIAL_UUID.jsonl"
assert_mirror_equals codex  "$CODEX_UUID"       "$CODEX_UUID"       0 "$DIR_CODEX/rollout-2026-06-26T02-43-06-$CODEX_UUID.jsonl"
ok "byte parity on all six mirrors (incl. the trailing-partial bytes — byte mirror does not care)"

step "store-DB: six identities, all generation 0, high_water == size"
assert_db "identity count" "SELECT COUNT(*) FROM files" "6"
assert_db "all at generation 0" "SELECT COUNT(*) FROM files WHERE generation != 0" "0"
assert_db "normal file high_water" \
  "SELECT high_water FROM files WHERE file_uuid='$NORMAL_UUID'" \
  "$(stat -c %s "$DIR_A/$NORMAL_UUID.jsonl")"
ok "store DB matches the tree"

step "kill-and-restart: SIGKILL shipper with a file partially shipped"
GROW_UUID=$(fresh_uuid)
GROW_SRC="$DIR_A/$GROW_UUID.jsonl"
SPLIT=400000 # mid-line byte offset inside the real resume-new-file capture
head -c "$SPLIT" "$FIXTURES/claude-resume-new-file.jsonl" >"$GROW_SRC"
wait_quiesced claude "$GROW_UUID" "$GROW_UUID" "$GROW_SRC"
kill9_shipper
reg_offset_is "claude/$GROW_UUID/$GROW_UUID" "$SPLIT" ||
  fail "registry cursor after SIGKILL is $(reg_offset "claude/$GROW_UUID/$GROW_UUID"), want $SPLIT (durable mid-file cursor)"
ok "cursor registry survived SIGKILL at offset $SPLIT (mid-file)"

step "file grows while the shipper is dead; restart resumes from the cursor"
tail -c +"$((SPLIT + 1))" "$FIXTURES/claude-resume-new-file.jsonl" >>"$GROW_SRC"
cmp -s "$GROW_SRC" "$FIXTURES/claude-resume-new-file.jsonl" || fail "harness self-check: rebuilt source != fixture"
start_shipper
wait_quiesced claude "$GROW_UUID" "$GROW_UUID" "$GROW_SRC"
assert_mirror_equals claude "$GROW_UUID" "$GROW_UUID" 0 "$GROW_SRC"
assert_db "resume stayed on one generation (append continuation, no conflict)" \
  "SELECT COUNT(*) FROM files WHERE file_uuid='$GROW_UUID'" "1"
ok "restart resumed mid-file to full parity on generation 0"

all_green
