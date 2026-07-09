#!/usr/bin/env bash
# Install (or re-install) the per-user sesh shipper service on this node.
#
# Idempotent: re-run after upgrading the binary or changing the store URL.
# No repo-path assumptions — templates are read from this script's own
# directory, the binary path and store URL are arguments; the module moves
# repos without touching this script.
#
# One shipper per OS user: run this as each user who should ship (the
# cursor-registry flock refuses a second instance per user regardless).
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: install-ship.sh --store-url URL [--binary /abs/path/sesh] [--dry-run]

  --store-url URL   store base URL, e.g. https://sesh.<tailnet>.ts.net
                    (required; the ONLY coupling between a node and the store)
  --binary PATH     absolute path to the sesh binary
                    (default: /usr/local/bin/sesh)
  --dry-run         print every action and rendered file; write nothing

Linux : installs a systemd --user unit (sesh-ship.service) + a drop-in
        carrying SESH_STORE_URL (and an ExecStart override when --binary is
        not the default), then enables and starts it. Reboot survival on
        no-login nodes additionally needs: loginctl enable-linger $USER
Darwin: renders dev.sesh.ship.plist.tmpl into ~/Library/LaunchAgents and
        bootstraps it into the gui domain.
USAGE
}

ETC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STORE_URL=""
SESH_BIN="/usr/local/bin/sesh"
DRY_RUN=0

while [ $# -gt 0 ]; do
  case "$1" in
    --store-url) STORE_URL="${2:?--store-url needs a value}"; shift 2 ;;
    --binary)    SESH_BIN="${2:?--binary needs a value}"; shift 2 ;;
    --dry-run)   DRY_RUN=1; shift ;;
    -h|--help)   usage; exit 0 ;;
    *) echo "install-ship.sh: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$STORE_URL" ] || { echo "install-ship.sh: --store-url is required" >&2; usage >&2; exit 2; }
case "$SESH_BIN" in
  /*) ;;
  *) echo "install-ship.sh: --binary must be an absolute path (got: $SESH_BIN)" >&2; exit 2 ;;
esac
if [ "$DRY_RUN" -eq 0 ] && [ ! -x "$SESH_BIN" ]; then
  echo "install-ship.sh: $SESH_BIN is not an executable file; install the binary first" >&2
  exit 2
fi

say()   { echo "install-ship: $*"; }
doit()  { if [ "$DRY_RUN" -eq 1 ]; then echo "DRY-RUN: $*"; else "$@"; fi; }
emit()  { # <target path> — file content on stdin
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "DRY-RUN: would write $1:"
    sed 's/^/    /'
  else
    cat >"$1"
  fi
}

install_linux() {
  local unit_dir="$HOME/.config/systemd/user"
  local dropin_dir="$unit_dir/sesh-ship.service.d"
  say "installing systemd user unit into $unit_dir"
  doit mkdir -p "$dropin_dir"
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "DRY-RUN: would copy $ETC_DIR/systemd/sesh-ship.service -> $unit_dir/sesh-ship.service"
  else
    cp "$ETC_DIR/systemd/sesh-ship.service" "$unit_dir/sesh-ship.service"
  fi

  {
    echo "# Written by install-ship.sh — node-local values only. Re-run to change."
    echo "[Service]"
    echo "Environment=SESH_STORE_URL=$STORE_URL"
    if [ "$SESH_BIN" != "/usr/local/bin/sesh" ]; then
      echo "ExecStart="
      echo "ExecStart=$SESH_BIN ship"
    fi
  } | emit "$dropin_dir/10-local.conf"

  doit systemctl --user daemon-reload
  doit systemctl --user enable --now sesh-ship.service
  say "installed and started."
  if [ "$(loginctl show-user "$USER" --property=Linger --value 2>/dev/null || echo unknown)" != "yes" ]; then
    say "NOTE: lingering is not enabled — the unit will NOT survive reboot on a"
    say "      node nobody logs into. Run: loginctl enable-linger $USER"
  fi
}

install_darwin() {
  local agents_dir="$HOME/Library/LaunchAgents"
  local plist="$agents_dir/dev.sesh.ship.plist"
  say "rendering launchd agent into $agents_dir"
  doit mkdir -p "$agents_dir" "$HOME/Library/Logs"
  sed \
    -e "s|@SESH_BIN@|$SESH_BIN|g" \
    -e "s|@SESH_STORE_URL@|$STORE_URL|g" \
    -e "s|@HOME@|$HOME|g" \
    "$ETC_DIR/launchd/dev.sesh.ship.plist.tmpl" | emit "$plist"
  # bootout is idempotent cleanup for re-installs; first install has nothing
  # to remove.
  doit launchctl bootout "gui/$(id -u)/dev.sesh.ship" 2>/dev/null || true
  doit launchctl bootstrap "gui/$(id -u)" "$plist"
  say "installed and bootstrapped (logs: ~/Library/Logs/sesh-ship.log)."
}

case "$(uname -s)" in
  Linux)  install_linux ;;
  Darwin) install_darwin ;;
  *) echo "install-ship.sh: unsupported platform $(uname -s) (no Windows in v1)" >&2; exit 2 ;;
esac
