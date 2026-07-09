#!/usr/bin/env bash
# Spec §6 S2 — resume churn: a Claude resume-into-new-file (the captured real
# pair: history rewritten under the resumed file's own content id, unified
# only by 141 overlapping message uuids). The transcript must not duplicate
# history — each (entry_type, message_uuid) indexed once — while the mirror
# holds BOTH files byte-faithfully. Index assertions run at the DB against
# the live serve-with-index; the drill-down render half is U7 integration.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

preflight
setup_workspace
build_binaries
start_store

ORIG_UUID=2c387aef-72ac-46bc-8ea5-e3b68690a937
NEW_UUID=e1be75ad-151b-47fa-9d69-46de1c117843
DIR=$(claude_tree proj-resume)

step "original session ships first (first-ingest order pins the unified id)"
cp "$FIXTURES/claude-resume-original.jsonl" "$DIR/$ORIG_UUID.jsonl"
start_shipper
wait_quiesced claude "$ORIG_UUID" "$ORIG_UUID" "$DIR/$ORIG_UUID.jsonl"
ok "original mirrored"

step "force the resume: the new file appears with overlapping history"
cp "$FIXTURES/claude-resume-new-file.jsonl" "$DIR/$NEW_UUID.jsonl"
wait_quiesced claude "$NEW_UUID" "$NEW_UUID" "$DIR/$NEW_UUID.jsonl"
ok "resumed file mirrored"

step "mirror holds both files byte-faithfully"
assert_mirror_equals claude "$ORIG_UUID" "$ORIG_UUID" 0 "$DIR/$ORIG_UUID.jsonl"
assert_mirror_equals claude "$NEW_UUID" "$NEW_UUID" 0 "$DIR/$NEW_UUID.jsonl"
ok "both histories intact in the mirror"

step "index unifies the pair into one logical session (overlap rule)"
wait_for "index to unify the resume pair" 30 dbq_is \
  "SELECT COUNT(DISTINCT logical_session_id) FROM sesh_index_messages WHERE quarantine=0" "1"
assert_db "unified id is the earliest file's content id (deterministic under first-ingest order)" \
  "SELECT DISTINCT logical_session_id FROM sesh_index_messages WHERE quarantine=0" "$ORIG_UUID"
ok "one logical session: $ORIG_UUID"

step "no duplicated history: every (entry_type, message_uuid) indexed once"
assert_db "no dedup-key violations" \
  "SELECT COUNT(*) FROM (SELECT entry_type, message_uuid FROM sesh_index_messages WHERE quarantine=0 AND message_uuid != '' GROUP BY entry_type, message_uuid HAVING COUNT(*) > 1)" "0"
EXPECTED_PAIRS=$(cat "$DIR/$ORIG_UUID.jsonl" "$DIR/$NEW_UUID.jsonl" |
  jq -r 'select(.uuid != null and .uuid != "") | "\(.type)|\(.uuid)"' | sort -u | wc -l)
assert_db "indexed pair count equals the union across both real files ($EXPECTED_PAIRS)" \
  "SELECT COUNT(DISTINCT entry_type || '|' || message_uuid) FROM sesh_index_messages WHERE quarantine=0 AND message_uuid != ''" \
  "$EXPECTED_PAIRS"
ok "index holds the union once — overlap deduped, no history duplication"

step "both files contribute rows (the resumed file's new tail is indexed)"
assert_db "rows exist from both file uuids" \
  "SELECT COUNT(DISTINCT file_uuid) FROM sesh_index_messages WHERE quarantine=0" "2"
assert_db "nothing quarantined in a clean resume" \
  "SELECT COUNT(*) FROM sesh_index_messages WHERE quarantine != 0" "0"
ok "unified transcript spans both files"

all_green
