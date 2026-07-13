#!/bin/sh -eu
# deploy-remote.sh — the VM-side half of `just deploy-store`. Uploaded fresh
# on every deploy and run as root:
#
#   sudo sh /tmp/deploy-remote.sh /tmp/sesh-linux-amd64
#
# Replaces /usr/local/bin/sesh with the crash-safe ordering the design
# requires (docs/design/2026-07-12-sesh-store-served-distribution.md §6):
# the only known-good binary is never overwritten in place, and the target
# path is never missing at any crash point.
#
#   1. stage the upload NEXT TO the target (same filesystem, so the final
#      rename is atomic) and prove it executes (`sesh version`) before
#      touching anything known-good
#   2. retain the current binary as sesh.prev via hardlink — the target
#      itself is never unlinked
#   3. atomic rename staged -> target, then restart sesh-serve (systemd's
#      SIGTERM is the drain signal; the unit gives it TimeoutStopSec)
#   4. report the version of the RUNNING image (/proc/<pid>/exe), not the
#      on-disk file — updated-but-not-restarted must read as failure
#
# SESH_OPS_ROOT is the same test seam as bootstrap.sh (prefixes paths,
# skips the root check).

NEW="${1:-}"
[ -n "$NEW" ] || { echo "usage: deploy-remote.sh /path/to/uploaded-binary" >&2; exit 2; }

ROOT="${SESH_OPS_ROOT:-}"
BIN_DIR="$ROOT/usr/local/bin"
TARGET="$BIN_DIR/sesh"
STAGED="$TARGET.next"

fail() { echo "deploy-remote.sh: ERROR: $*" >&2; exit 1; }

if [ -z "$ROOT" ] && [ "$(id -u)" -ne 0 ]; then
  fail "must run as root"
fi
[ -f "$NEW" ] || fail "uploaded binary not found: $NEW"
mkdir -p "$BIN_DIR"

# Stray staging from a crashed prior run is cleaned, never promoted.
rm -f "$STAGED"
install -m 755 "$NEW" "$STAGED"
if ! "$STAGED" version >/dev/null 2>&1; then
  rm -f "$STAGED"
  fail "uploaded binary does not execute ('sesh version' failed) — target untouched"
fi

if [ -e "$TARGET" ]; then
  ln -f "$TARGET" "$TARGET.prev"
fi
sync "$STAGED" 2>/dev/null || sync
mv -f "$STAGED" "$TARGET"
sync "$BIN_DIR" 2>/dev/null || sync

systemctl restart sesh-serve.service ||
  fail "restart failed — on-disk binary is the new one; previous retained at $TARGET.prev"
sleep 1
PID="$(systemctl show sesh-serve.service --property=MainPID --value)"
if [ -z "$PID" ] || [ "$PID" = 0 ]; then
  fail "sesh-serve has no main pid after restart"
fi
printf 'store now: '
"/proc/$PID/exe" version || fail "running image did not report a version"
