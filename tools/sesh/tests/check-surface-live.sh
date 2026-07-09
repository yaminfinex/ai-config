#!/usr/bin/env bash
# U7 M2 gate — live surface integration: REAL `sesh serve` (ingest + surface
# listeners), REAL `sesh ship` over a fixture tree holding the captured
# resume pair; the recency page lists ONE session and the drill-down renders
# ONE transcript with no duplicated history (S2 render half over the live
# stack), with the raw fallback reachable. Prints ALL GREEN on success.
set -euo pipefail
source "$(dirname "$0")/lib.sh"

ORIG_UUID="2c387aef-72ac-46bc-8ea5-e3b68690a937"
NEW_UUID="e1be75ad-151b-47fa-9d69-46de1c117843"

preflight
setup_workspace
build_binaries

step "start real store with ingest + surface listeners"
STORE_PORT=$(free_port)
SURFACE_PORT=$(free_port)
STORE_URL="http://127.0.0.1:$STORE_PORT"
SURFACE_URL="http://127.0.0.1:$SURFACE_PORT"
"$BIN/sesh" serve --addr "127.0.0.1:$STORE_PORT" --surface-addr "127.0.0.1:$SURFACE_PORT" \
  --data-dir "$STORE_DIR" >>"$WORK/store.log" 2>&1 &
STORE_PID=$!
wait_for "store to accept connections" 10 store_up
surface_up() {
  [ "$(curl -s -o /dev/null -w '%{http_code}' "$SURFACE_URL/")" = "200" ]
}
wait_for "surface to accept connections" 10 surface_up
ok "serve is up (ingest :$STORE_PORT, surface :$SURFACE_PORT)"

step "real shipper mirrors the resume-pair fixture tree"
TREE=$(claude_tree "harness-resume-live")
cp "$FIXTURES/claude-resume-original.jsonl" "$TREE/$ORIG_UUID.jsonl"
cp "$FIXTURES/claude-resume-new-file.jsonl" "$TREE/$NEW_UUID.jsonl"
start_shipper
wait_quiesced claude "$ORIG_UUID" "$ORIG_UUID" "$TREE/$ORIG_UUID.jsonl"
wait_quiesced claude "$NEW_UUID" "$NEW_UUID" "$TREE/$NEW_UUID.jsonl"
assert_mirror_equals claude "$ORIG_UUID" "$ORIG_UUID" 0 "$TREE/$ORIG_UUID.jsonl"
assert_mirror_equals claude "$NEW_UUID" "$NEW_UUID" 0 "$TREE/$NEW_UUID.jsonl"
ok "both files mirrored byte-identical"

step "live index unifies the pair into one logical session"
unified() {
  [ "$(dbq 'SELECT COUNT(DISTINCT logical_session_id) FROM sesh_index_messages WHERE quarantine = 0')" = "1" ] &&
    [ "$(dbq 'SELECT COUNT(DISTINCT file_uuid) FROM sesh_index_messages WHERE quarantine = 0')" = "2" ]
}
wait_for "index to unify the resume pair" 30 unified
LOGICAL=$(dbq 'SELECT DISTINCT logical_session_id FROM sesh_index_messages WHERE quarantine = 0')
ROWS=$(dbq 'SELECT COUNT(*) FROM sesh_index_messages WHERE quarantine = 0')
# 206 + 269 lines - 141 overlapping (entry_type, uuid) pairs: the corpus
# README's verified S2 arithmetic.
[ "$ROWS" = "334" ] || fail "live index holds $ROWS deduped rows, want 334"
ok "one logical session ($LOGICAL) with 334 deduped rows"

step "recency page lists exactly one session for the pair"
page=$(curl -sf "$SURFACE_URL/") || fail "GET / failed"
links=$(grep -o 'href="/s/claude/[0-9a-f-]*"' <<<"$page" | sort -u | wc -l)
[ "$links" = "1" ] || fail "recency page shows $links claude session links, want 1"
grep -q "$LOGICAL" <<<"$page" || fail "recency page does not link the logical session"

step "drill-down renders ONE transcript (no duplicated history)"
transcript=$(curl -sf "$SURFACE_URL/s/claude/$LOGICAL") || fail "GET transcript failed"
entries=$(grep -c '<li class="entry' <<<"$transcript" || true)
[ "$entries" = "334" ] || fail "transcript renders $entries entries, want 334"
dup=$(grep -o 'data-uuid="[^"]*"' <<<"$transcript" | sort | uniq -d | head -3)
[ -z "$dup" ] || fail "duplicated message uuids in the rendered transcript: $dup"
grep -q "${ORIG_UUID:0:8}" <<<"$transcript" || fail "transcript does not list the original file"
grep -q "${NEW_UUID:0:8}" <<<"$transcript" || fail "transcript does not list the resumed file"
ok "one transcript, both files, no duplicates"

step "raw fallback reachable; zero write surface"
curl -sf "$SURFACE_URL/s/claude/$LOGICAL/raw" >/dev/null || fail "raw fallback GET failed"
if grep -qiE '<[[:space:]]*(form|input|button|select|textarea)[>[:space:]]' <<<"$transcript$page"; then
  fail "rendered pages carry a write surface (R17)"
fi
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$SURFACE_URL/")
[ "$code" = "405" ] || fail "POST / = $code, want 405"
ok "raw fallback up; no forms; POST rejected"

stop_shipper
stop_store
all_green
