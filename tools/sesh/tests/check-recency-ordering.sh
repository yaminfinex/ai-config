#!/usr/bin/env bash
# M2 recency gate — recency is the maximum PARSED message timestamp per
# logical session (wire doc "Message Index Schema"), never ingest time:
# a late-onboarded node's backfill of old sessions must sort below live work
# even though its bytes arrive later. Drill: ship the newer-content session
# first, then backfill an older-content session LAST — the later ingest must
# still rank below. Assertions at the DB (the page render is U7 integration).
set -euo pipefail
. "$(dirname "$0")/lib.sh"

preflight
setup_workspace
build_binaries
start_store

LIVE_UUID=45308169-72e6-4cbe-a05c-2a0025db055e   # real capture, content timestamps 2026-07-02
OLD_UUID=019f01cf-3d22-7ea0-923e-e463b90ea31e    # real capture, content timestamps 2026-06-26
DIR=$(claude_tree proj-live)
LIVE_SRC="$DIR/$LIVE_UUID.jsonl"
cp "$FIXTURES/claude-normal.jsonl" "$LIVE_SRC"

# Both roots must exist before the shipper starts: fsnotify watches are laid
# at startup, and a root created afterwards waits for the 60s rescan.
CODEX_DIR=$(codex_tree)

step "live session ships first"
start_shipper
wait_quiesced claude "$LIVE_UUID" "$LIVE_UUID" "$LIVE_SRC"
ok "live session mirrored"

step "late-onboarded backfill: the OLDER-content session arrives LATER"
OLD_SRC="$CODEX_DIR/rollout-2026-06-26T02-43-06-$OLD_UUID.jsonl"
cp "$FIXTURES/codex-rollout-meta.jsonl" "$OLD_SRC"
wait_quiesced codex "$OLD_UUID" "$OLD_UUID" "$OLD_SRC"
wait_for "both sessions indexed with parsed timestamps" 30 dbq_is \
  "SELECT COUNT(DISTINCT logical_session_id) FROM sesh_index_messages WHERE quarantine=0 AND timestamp_utc IS NOT NULL" "2"
ok "backfilled session mirrored and indexed after the live one"

step "harness self-check: the fixtures really have the timestamp skew the drill needs"
LIVE_MAX=$(jq -r 'select(.timestamp != null) | .timestamp' "$LIVE_SRC" | sort | tail -1)
OLD_MAX=$(jq -r 'select(.timestamp != null) | .timestamp' "$OLD_SRC" | sort | tail -1)
[ -n "$LIVE_MAX" ] && [ -n "$OLD_MAX" ] || fail "fixtures lost their parsed timestamps"
[ "$OLD_MAX" \< "$LIVE_MAX" ] || fail "fixture skew inverted: old=$OLD_MAX live=$LIVE_MAX"
ok "content timestamps: live=$LIVE_MAX > backfilled=$OLD_MAX"

step "recency ranks by parsed timestamps: backfilled-old sorts below live"
RANKING=$(dbq "SELECT logical_session_id FROM sesh_index_messages WHERE quarantine=0 AND timestamp_utc IS NOT NULL GROUP BY logical_session_id ORDER BY MAX(timestamp_utc) DESC")
EXPECTED=$(printf '%s\n%s' "$LIVE_UUID" "$OLD_UUID")
[ "$RANKING" = "$EXPECTED" ] || fail "recency ranking wrong: got [$RANKING], want live above backfilled"
LATER_INGEST=$(dbq "SELECT wire_session_id FROM index_file_state ORDER BY rowid DESC LIMIT 1")
[ "$LATER_INGEST" = "$OLD_UUID" ] || fail "harness self-check: backfill was not the later ingest (got $LATER_INGEST)"
ok "later-ingested backfill ranks below live — recency is parsed time, not ingest time"

all_green
