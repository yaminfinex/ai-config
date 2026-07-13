#!/bin/sh -eu
# bootstrap.sh — idempotent quick-host bring-up for the sesh store.
# Design: docs/design/2026-07-12-sesh-store-served-distribution.md §8;
# hosting shape: backlog/docs/doc-002 §2b (system unit, dedicated user,
# /var/lib/sesh, tsnet day 1).
#
# Usage: copy ops/ to the VM and run as root, with the tag:sesh auth key in
# the environment for the FIRST run only:
#
#   gcloud compute scp --recurse --tunnel-through-iap ops quick-host:/tmp/sesh-ops
#   gcloud compute ssh quick-host --tunnel-through-iap \
#     --command 'sudo TS_AUTHKEY=tskey-auth-... sh /tmp/sesh-ops/bootstrap.sh'
#
# What it does (all idempotent — a re-run with nothing changed rewrites no
# file and restarts no service):
#   - system user `sesh` (nologin), /var/lib/sesh data dir (+ tsnet/,
#     releases/ group-writable for publishing, backup/db/)
#   - TS_AUTHKEY handoff: written once to /etc/sesh/serve.env (root-only,
#     0600); systemd hands it to the first `sesh serve --tsnet` start, which
#     joins the tailnet and persists node identity under /var/lib/sesh/tsnet.
#     After that the key is inert and the file may be scrubbed.
#   - installs sesh-serve.service, sesh-backup.{service,timer}, and the
#     backup script; enables them; (re)starts sesh-serve only when the store
#     binary exists (the first binary arrives via `just deploy-store`, which
#     also starts the service)
#
# quickd, Caddy, and the VM's tailscaled are untouched by construction: the
# store embeds tsnet with its own node identity and state dir.
#
# Divergence from quick's bootstrap, deliberate: no automatic restore from
# GCS on an empty data dir. Restoring the store is the documented drill
# (ops/README.md), run by an operator on purpose — an empty /var/lib/sesh on
# a fresh host is also the normal first-boot state.
#
# SESH_OPS_ROOT is a test seam: a non-empty value prefixes every absolute
# path, skips the root check and ownership changes, and lets the gate
# harness (tests/check-store-deploy.sh) exercise this exact script.

ROOT="${SESH_OPS_ROOT:-}"
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ETC_DIR="$ROOT/etc/sesh"
ENV_FILE="$ETC_DIR/serve.env"
DATA_DIR="$ROOT/var/lib/sesh"
UNIT_DIR="$ROOT/etc/systemd/system"
BIN_DIR="$ROOT/usr/local/bin"

fail() { echo "bootstrap.sh: ERROR: $*" >&2; exit 1; }
own()  { [ -n "$ROOT" ] || chown "$@"; }

if [ -z "$ROOT" ] && [ "$(id -u)" -ne 0 ]; then
  fail "must run as root"
fi

# --- dependencies (backup needs both) ----------------------------------------
if ! command -v sqlite3 >/dev/null 2>&1; then
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq sqlite3
fi
command -v gcloud >/dev/null 2>&1 ||
  fail "gcloud not found (backup pushes to GCS; expected on GCE images)"

# --- user + dirs ---------------------------------------------------------------
if ! id sesh >/dev/null 2>&1; then
  useradd --system --home /var/lib/sesh --shell /usr/sbin/nologin sesh
fi

mkdir -p "$DATA_DIR/tsnet" "$DATA_DIR/backup/db" "$DATA_DIR/releases" \
  "$ETC_DIR" "$UNIT_DIR" "$BIN_DIR"
chmod 750 "$DATA_DIR"
chmod 700 "$DATA_DIR/tsnet"          # the node key lives here
chmod 2775 "$DATA_DIR/releases"      # group-writable + setgid: `just release`
                                     # publishes over ssh as a member of
                                     # group sesh, no sudo in the publish path
own sesh:sesh "$DATA_DIR" "$DATA_DIR/tsnet" "$DATA_DIR/backup" \
  "$DATA_DIR/backup/db" "$DATA_DIR/releases"

# --- TS_AUTHKEY first-start handoff --------------------------------------------
# tsnet reads TS_AUTHKEY only while it has no stored node state; once
# /var/lib/sesh/tsnet is populated the node re-authenticates from state and
# the key is dead weight.
if [ -z "$(ls -A "$DATA_DIR/tsnet" 2>/dev/null)" ]; then
  if [ -f "$ENV_FILE" ] && grep -q '^TS_AUTHKEY=' "$ENV_FILE"; then
    : # key already delivered to the env file (perms converged below)
  elif [ -n "${TS_AUTHKEY:-}" ]; then
    (umask 077 && printf 'TS_AUTHKEY=%s\n' "$TS_AUTHKEY" >"$ENV_FILE")
    echo "bootstrap.sh: wrote TS_AUTHKEY to $ENV_FILE (root-only) for the first start"
  else
    fail "no tsnet state yet and no auth key: run with TS_AUTHKEY=tskey-auth-... (a reusable tag:sesh key) or place TS_AUTHKEY=... in $ENV_FILE first"
  fi
else
  if [ -f "$ENV_FILE" ] && grep -q '^TS_AUTHKEY=' "$ENV_FILE"; then
    echo "bootstrap.sh: note: node state exists under $DATA_DIR/tsnet; the TS_AUTHKEY in $ENV_FILE is no longer needed and may be scrubbed"
  fi
fi
# However the env file got here — this script or an operator pre-seeding it
# under a normal umask — the reusable tag:sesh key must never be readable by
# other local users. Converge owner+mode (content untouched) before anything
# is installed or started.
if [ -f "$ENV_FILE" ]; then
  chmod 600 "$ENV_FILE"
  own root:root "$ENV_FILE"
fi

# --- units + backup script (install only what changed) --------------------------
CHANGED=0
put() { # <src> <dst> <mode>
  if ! cmp -s "$1" "$2"; then
    install -m "$3" "$1" "$2"
    CHANGED=1
    echo "bootstrap.sh: installed $2"
  fi
}
put "$SCRIPT_DIR/systemd/sesh-serve.service"  "$UNIT_DIR/sesh-serve.service"  644
put "$SCRIPT_DIR/systemd/sesh-backup.service" "$UNIT_DIR/sesh-backup.service" 644
put "$SCRIPT_DIR/systemd/sesh-backup.timer"   "$UNIT_DIR/sesh-backup.timer"   644
put "$SCRIPT_DIR/backup.sh"                   "$BIN_DIR/sesh-backup.sh"       755

# --- enable + start --------------------------------------------------------------
if [ "$CHANGED" -eq 1 ]; then
  systemctl daemon-reload
fi
systemctl enable sesh-serve.service sesh-backup.timer
systemctl start sesh-backup.timer

if [ -x "$BIN_DIR/sesh" ]; then
  if [ "$CHANGED" -eq 1 ]; then
    systemctl restart sesh-serve.service
  else
    systemctl start sesh-serve.service   # no-op when already running
  fi
else
  echo "bootstrap.sh: NOTE: $BIN_DIR/sesh not installed yet — sesh-serve stays stopped; deploy the store binary with 'just deploy-store'"
fi

echo "bootstrap.sh: done"
