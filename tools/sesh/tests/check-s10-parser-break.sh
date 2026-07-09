#!/usr/bin/env bash
# Spec §6 S10 — parser-break drill: feed the index valid-JSONL lines the
# parser cannot produce normal rows from (valid JSON, wrong shape: the drill
# variant spec S10 names). The mirror must stay byte-intact, quarantine must
# be visible (rows + ledger), other files and subsequent rows must keep
# indexing, and `sesh reindex` must re-derive the identical index from the
# mirror — the disposable-index guarantee the eventual parser fix relies on.
# The raw-fallback render half is U7 integration; assertions here are DB.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

preflight
setup_workspace
build_binaries
start_store

GOOD_UUID=45308169-72e6-4cbe-a05c-2a0025db055e
DRILL_UUID=$(fresh_uuid) # name plumbing; drill content below is a variant of real bytes
DIR=$(claude_tree proj-drill)
GOOD_SRC="$DIR/$GOOD_UUID.jsonl"
DRILL_SRC="$DIR/$DRILL_UUID.jsonl"
cp "$FIXTURES/claude-normal.jsonl" "$GOOD_SRC"

# Drill variant: real fixture lines with valid-JSON-but-not-an-object lines
# spliced mid-file and appended — the exact "unparseable-but-valid-JSONL"
# class (S10): json.Unmarshal into an object fails, line-JSONL shape holds.
# Built from a DIFFERENT real session than the healthy file: same-session
# bytes would (correctly) dedup against it and mask the drill.
DRILL_BASE="$FIXTURES/claude-interleaved-writers-standin.jsonl"
DRILL_REAL_LINES=$(wc -l <"$DRILL_BASE")
head -n 5 "$DRILL_BASE" >"$DRILL_SRC"
printf '["valid","jsonl","but","not","a","transcript","object"]\n' >>"$DRILL_SRC"
tail -n +6 "$DRILL_BASE" >>"$DRILL_SRC"
printf '42\n' >>"$DRILL_SRC"

step "ship the healthy file and the drill file"
start_shipper
wait_quiesced claude "$GOOD_UUID" "$GOOD_UUID" "$GOOD_SRC"
wait_quiesced claude "$DRILL_UUID" "$DRILL_UUID" "$DRILL_SRC"
ok "both files mirrored to quiescence"

step "mirror intact: parse failures never block the byte mirror"
assert_mirror_equals claude "$GOOD_UUID" "$GOOD_UUID" 0 "$GOOD_SRC"
assert_mirror_equals claude "$DRILL_UUID" "$DRILL_UUID" 0 "$DRILL_SRC"
DRILL_MIRROR_SHA=$(sha256sum "$(mirror_path claude "$DRILL_UUID" "$DRILL_UUID" 0)" | cut -d' ' -f1)
ok "drill bytes mirrored byte-faithfully alongside the quarantine"

step "quarantine visible: rows flagged with a stable reason, ledger written"
wait_for "both drill lines to quarantine" 30 dbq_is \
  "SELECT COUNT(*) FROM sesh_index_messages WHERE file_uuid='$DRILL_UUID' AND quarantine=1" "2"
assert_db "stable quarantine reason" \
  "SELECT DISTINCT quarantine_reason FROM sesh_index_messages WHERE quarantine=1" "invalid_json"
assert_db "quarantine ledger carries both drill lines" \
  "SELECT COUNT(*) FROM quarantine_ledger WHERE file_uuid='$DRILL_UUID'" "2"
ok "quarantine is loud, not silent"

step "quarantine blocks nothing: later rows in the drill file still index"
wait_for "drill file's real lines to index normally" 30 dbq_is \
  "SELECT COUNT(*) FROM sesh_index_messages WHERE file_uuid='$DRILL_UUID' AND quarantine=0" "$DRILL_REAL_LINES"
ok "all $DRILL_REAL_LINES real lines of the drill file indexed past the quarantined ones"

step "other files continue: the healthy file keeps flowing after the drill"
printf '["another","drill","line"]\n' >>"$DRILL_SRC"
head -c 2000 "$FIXTURES/claude-resume-original.jsonl" >>"$GOOD_SRC"
wait_quiesced claude "$GOOD_UUID" "$GOOD_UUID" "$GOOD_SRC"
assert_mirror_equals claude "$GOOD_UUID" "$GOOD_UUID" 0 "$GOOD_SRC"
wait_for "third drill line to quarantine" 30 dbq_is \
  "SELECT COUNT(*) FROM sesh_index_messages WHERE file_uuid='$DRILL_UUID' AND quarantine=1" "3"
ok "healthy file shipped and indexed while the drill file quarantined"

step "reindex re-derives the identical index from the mirror"
SNAPSHOT_SQL="SELECT tool, logical_session_id, wire_session_id, entry_type, message_uuid, file_uuid, generation, role, COALESCE(timestamp_utc,''), file_ordinal, line_ordinal, byte_start, byte_end, quarantine, quarantine_reason FROM sesh_index_messages ORDER BY tool, file_uuid, generation, line_ordinal, byte_start"
BEFORE="$WORK/index-before.tsv"
AFTER="$WORK/index-after.tsv"
dbq "$SNAPSHOT_SQL" >"$BEFORE"
stop_shipper
stop_store
# Recapture now: the drill file grew a third line mid-scenario, so the early
# sha is stale; the reindex-must-not-touch-bytes baseline is the final state.
DRILL_MIRROR_SHA=$(sha256sum "$(mirror_path claude "$DRILL_UUID" "$DRILL_UUID" 0)" | cut -d' ' -f1)
"$BIN/sesh" reindex --data-dir "$STORE_DIR" >>"$WORK/store.log" 2>&1 || fail "sesh reindex exited nonzero"
dbq "$SNAPSHOT_SQL" >"$AFTER"
cmp -s "$BEFORE" "$AFTER" || fail "reindex produced different rows than live indexing (diff: $(diff "$BEFORE" "$AFTER" | head -5))"
[ "$(sha256sum "$(mirror_path claude "$DRILL_UUID" "$DRILL_UUID" 0)" | cut -d' ' -f1)" = "$DRILL_MIRROR_SHA" ] ||
  fail "reindex mutated mirror bytes (must never happen)"
ok "index re-derived identically; mirror untouched — a fixed parser heals by the same path"

all_green
