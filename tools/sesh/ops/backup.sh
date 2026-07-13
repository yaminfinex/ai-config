#!/bin/sh -eu
# sesh-backup.sh — installed to /usr/local/bin/sesh-backup.sh by
# ops/bootstrap.sh; run every 15 minutes by sesh-backup.timer as user `sesh`
# (RPO = 15 min). Same GCS machinery as quick's backup, zero quick-owned
# files touched: separate script, separate timer, separate bucket prefix
# (backlog/docs/doc-002 §2b backup shape).
#
# What makes the copy recoverable:
#   - store.sqlite is NEVER copied live: `VACUUM INTO` takes an online,
#     consistent, compacted snapshot — the only safe copy of a live WAL
#     database
#   - ordering keeps the bucket invariant "the db snapshot never claims
#     bytes the mirror lacks": snapshot the db FIRST, then upload the mirror
#     BEFORE the snapshot. Mirror files are append-only, so the uploaded
#     mirror is always at least as new as the snapshot's high-water claims;
#     a crash between the two uploads leaves an OLDER db against a NEWER
#     mirror, which restores cleanly (surplus mirror bytes heal via reindex
#     and the wire's PUT overlap comparison). The reverse — a db claiming
#     data the restored mirror does not hold — can never reach the bucket.
#   - tsnet/ is included: it is the store's tailnet identity; restoring it
#     brings the node back AS `sesh`, same URL, zero shipper changes
#   - releases/ is rebuildable (re-publish regenerates it) but rides along
#
# The bucket has object versioning: deletes/overwrites retain history.
# Restore is the drill in ops/README.md — backup is real only after it has
# been performed once.

DATA_DIR="${SESH_DATA_DIR:-/var/lib/sesh}"
BUCKET="${SESH_BACKUP_BUCKET:-gs://infinex-quick-backup/sesh}"
WARN_PCT="${SESH_DISK_WARN_PCT:-80}"

[ -d "$DATA_DIR" ] || { echo "sesh-backup.sh: data dir missing: $DATA_DIR" >&2; exit 1; }

# Capacity: unbounded mirror growth on the shared disk is an escape trigger
# (ops/README.md). Warn loudly on every run past the threshold; keep backing
# up — a full-ish disk is exactly when the backup matters most.
USED_PCT=$(df -P "$DATA_DIR" | awk 'NR==2 { sub("%",""); print $5 }')
if [ "$USED_PCT" -ge "$WARN_PCT" ]; then
  echo "sesh-backup.sh: WARNING: disk holding $DATA_DIR is ${USED_PCT}% full (threshold ${WARN_PCT}%) — evaluate the escape triggers in ops/README.md" >&2
fi

mkdir -p "$DATA_DIR/backup/db"
if [ -f "$DATA_DIR/store.sqlite" ]; then
  rm -f "$DATA_DIR/backup/db/store.sqlite.tmp"
  sqlite3 "$DATA_DIR/store.sqlite" "VACUUM INTO '$DATA_DIR/backup/db/store.sqlite.tmp'"
  mv "$DATA_DIR/backup/db/store.sqlite.tmp" "$DATA_DIR/backup/db/store.sqlite"
fi

# Upload order is load-bearing (see header): mirror first, db snapshot after.
if [ -d "$DATA_DIR/mirror" ]; then
  gcloud storage rsync -r "$DATA_DIR/mirror" "$BUCKET/mirror"
fi
gcloud storage rsync -r "$DATA_DIR/backup/db" "$BUCKET/db"
if [ -d "$DATA_DIR/tsnet" ]; then
  gcloud storage rsync -r "$DATA_DIR/tsnet" "$BUCKET/tsnet"
fi
if [ -d "$DATA_DIR/releases" ]; then
  gcloud storage rsync -r "$DATA_DIR/releases" "$BUCKET/releases"
fi
