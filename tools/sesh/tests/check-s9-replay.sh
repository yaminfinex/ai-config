#!/usr/bin/env bash
# Spec §6 S9 — store restart / duplicate range: re-send an already-ACKed
# range → no mirror corruption (and no index duplication once U6 lands; the
# M1 byte-flow half is mirror + high_water integrity). Also carries the U5
# store kill-and-restart check: kill -9 the store MID-PUT (body still
# streaming), restart, and prove clean recovery to full parity via the real
# shipper.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

preflight
setup_workspace
build_binaries
start_store

UUID=45308169-72e6-4cbe-a05c-2a0025db055e
DIR=$(claude_tree proj-replay)
SRC="$DIR/$UUID.jsonl"
cp "$FIXTURES/claude-normal.jsonl" "$SRC"
SIZE=$(stat -c %s "$SRC")

step "ship to quiescence, then stop the shipper (wire steps below are surgical)"
start_shipper
wait_quiesced claude "$UUID" "$UUID" "$SRC"
stop_shipper
MIRROR=$(mirror_path claude "$UUID" "$UUID" 0)
SHA_BEFORE=$(sha256sum "$MIRROR" | cut -d' ' -f1)
ok "baseline mirrored at $SIZE bytes"

step "duplicate range: re-PUT the entire already-ACKed range at offset 0"
CODE=$(put_bytes claude "$UUID" "$UUID" 0 "$SRC")
[ "$CODE" = "200" ] || fail "replay PUT returned HTTP $CODE, want 200 (body: $(cat "$WORK/last-put.json"))"
[ "$(jq -r .status "$WORK/last-put.json")" = "ack" ] || fail "replay response is not an ack"
[ "$(jq -r .high_water "$WORK/last-put.json")" = "$SIZE" ] || fail "replay moved high_water"
[ "$(sha256sum "$MIRROR" | cut -d' ' -f1)" = "$SHA_BEFORE" ] || fail "replay mutated mirror bytes"
ok "idempotent replay: 200 ack, high_water $SIZE, mirror bytes identical"

step "graceful store restart: state survives, replay still idempotent"
stop_store
start_store "$STORE_PORT"
[ "$(active_high_water claude "$UUID" "$UUID")" = "$SIZE" ] || fail "high_water lost across restart"
CODE=$(put_bytes claude "$UUID" "$UUID" 0 "$SRC")
[ "$CODE" = "200" ] || fail "post-restart replay returned HTTP $CODE"
[ "$(sha256sum "$MIRROR" | cut -d' ' -f1)" = "$SHA_BEFORE" ] || fail "post-restart replay mutated mirror"
ok "restarted store answers from durable state"

step "kill -9 the store MID-PUT (large body still streaming)"
BIG_UUID=$(fresh_uuid) # name plumbing; bytes are the real resume-new-file capture
BIG_DIR=$(claude_tree proj-midput)
BIG_SRC="$BIG_DIR/$BIG_UUID.jsonl"
cp "$FIXTURES/claude-resume-new-file.jsonl" "$BIG_SRC"
BIG_SIZE=$(stat -c %s "$BIG_SRC")
curl -s --limit-rate 100K -X PUT \
  -H 'Content-Type: application/octet-stream' \
  -H 'X-Sesh-Wire-Version: 1' \
  -H "X-Sesh-Hostname: $(hostname)" \
  -H "X-Sesh-OS-User: $(id -un)" \
  --data-binary @"$BIG_SRC" \
  "$STORE_URL/v1/files/claude/$BIG_UUID/$BIG_UUID/bytes?offset=0" >/dev/null 2>&1 &
CURL_PID=$!
sleep 1.5 # ~150 KB of ~780 KB transferred: the PUT is provably mid-flight
kill9_store
wait "$CURL_PID" 2>/dev/null || true # the interrupted PUT fails; that is the point
ok "store killed with the PUT mid-stream"

step "restart: no corruption, high_water consistent with mirrored bytes"
start_store "$STORE_PORT"
assert_db "database opens and answers after kill -9" "SELECT COUNT(*) FROM files WHERE file_uuid='$UUID'" "1"
HW=$(active_high_water claude "$BIG_UUID" "$BIG_UUID")
BIG_MIRROR=$(mirror_path claude "$BIG_UUID" "$BIG_UUID" 0)
if [ "$HW" = "-1" ]; then
  [ ! -s "$BIG_MIRROR" ] || fail "store has no state for the interrupted PUT but mirror holds bytes"
  ok "interrupted PUT left no state (never ACKed, nothing durable claimed)"
else
  [ "$(stat -c %s "$BIG_MIRROR")" = "$HW" ] || fail "high_water $HW != mirrored bytes $(stat -c %s "$BIG_MIRROR")"
  head -c "$HW" "$BIG_SRC" | cmp -s - "$BIG_MIRROR" || fail "mirrored prefix diverges from source after kill -9"
  ok "durable state is an exact source prefix at high_water $HW"
fi

step "the real shipper replays to full parity"
start_shipper
wait_quiesced claude "$BIG_UUID" "$BIG_UUID" "$BIG_SRC"
assert_mirror_equals claude "$BIG_UUID" "$BIG_UUID" 0 "$BIG_SRC"
assert_db "single generation for the recovered file" \
  "SELECT COUNT(*) FROM files WHERE file_uuid='$BIG_UUID'" "1"
[ "$(sha256sum "$MIRROR" | cut -d' ' -f1)" = "$SHA_BEFORE" ] || fail "kill/recovery cycle touched an unrelated mirror"
ok "recovered to byte parity ($BIG_SIZE bytes) on one generation"

all_green
