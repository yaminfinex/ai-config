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
Usage: install-ship.sh --store-url URL [--binary /abs/path/sesh] [--dry-run] [--force]

  --store-url URL   store base URL, e.g. http://sesh.<tailnet>.ts.net:8765
                    (required; the ONLY coupling between a node and the store.
                    tsnet mode is plain http — the tailnet encrypts transport)
  --binary PATH     absolute path to the sesh binary
                    (default: <GOBIN>/sesh; GOPATH/bin when GOBIN is unset)
  --dry-run         print every action and rendered file; write nothing
  --force           overwrite an existing drop-in (default: refuse, so
                    operator edits to 10-local.conf are never clobbered)

Linux : installs a systemd --user unit (sesh-ship.service) + a drop-in
        carrying SESH_STORE_URL, then enables and starts it. Reboot survival on
        no-login nodes additionally needs: loginctl enable-linger $USER
Darwin: renders dev.sesh.ship.plist.tmpl into ~/Library/LaunchAgents and
        bootstraps it into the gui domain.
USAGE
}

ETC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STORE_URL=""
SESH_BIN=""
DRY_RUN=0
FORCE=0

while [ $# -gt 0 ]; do
  case "$1" in
    --store-url) STORE_URL="${2:?--store-url needs a value}"; shift 2 ;;
    --binary)    SESH_BIN="${2:?--binary needs a value}"; shift 2 ;;
    --dry-run)   DRY_RUN=1; shift ;;
    --force)     FORCE=1; shift ;;
    -h|--help)   usage; exit 0 ;;
    *) echo "install-ship.sh: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [ -z "$SESH_BIN" ]; then
  command -v go >/dev/null 2>&1 || {
    echo "install-ship.sh: go is required to resolve GOBIN; pass --binary explicitly" >&2
    exit 2
  }
  SESH_BIN="$(go env GOBIN)"
  [ -n "$SESH_BIN" ] || SESH_BIN="$(go env GOPATH)/bin"
  SESH_BIN="$SESH_BIN/sesh"
fi

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

render_dropin() {
  echo "# Written by install-ship.sh — node-local values only. Re-run --force to change."
  echo "[Service]"
  echo "Environment=SESH_STORE_URL=$STORE_URL"
}

render_unit() {
  sed "s|^ExecStart=/usr/local/bin/sesh ship$|ExecStart=$SESH_BIN ship|" \
    "$ETC_DIR/systemd/sesh-ship.service"
}

install_linux() {
  local unit_dir="$HOME/.config/systemd/user"
  local dropin_dir="$unit_dir/sesh-ship.service.d"
  local dropin="$dropin_dir/10-local.conf"

  # Preflight BEFORE any write: a broken user bus (SSH session without
  # lingering, no XDG_RUNTIME_DIR) would otherwise leave a half-installed,
  # never-started service on disk.
  if ! systemctl --user show-environment >/dev/null 2>&1; then
    if [ "$DRY_RUN" -eq 1 ]; then
      say "WARNING: systemd user manager is not reachable on this host; a real"
      say "         run would stop here. Remedy: loginctl enable-linger $USER"
      say "         (then reconnect so XDG_RUNTIME_DIR is set)."
    else
      echo "install-ship.sh: cannot talk to the systemd user manager — nothing was written." >&2
      echo "  Likely cause: SSH session without lingering (no user bus / XDG_RUNTIME_DIR)." >&2
      echo "  Remedy: loginctl enable-linger $USER   then reconnect and re-run." >&2
      exit 1
    fi
  fi

  # Preserve operator edits: never clobber an existing, differing drop-in
  # unless --force. Checked before any write so a refusal leaves the node
  # exactly as found.
  if [ -f "$dropin" ] && [ "$FORCE" -eq 0 ] && ! render_dropin | cmp -s - "$dropin"; then
    echo "install-ship.sh: $dropin exists with different content — refusing to overwrite." >&2
    echo "  Re-run with --force to replace it, or edit it directly. Nothing was written." >&2
    exit 1
  fi

  say "installing systemd user unit into $unit_dir"
  doit mkdir -p "$dropin_dir"
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "DRY-RUN: would write $unit_dir/sesh-ship.service:"
    render_unit | sed 's/^/    /'
  else
    render_unit >"$unit_dir/sesh-ship.service"
  fi
  render_dropin | emit "$dropin"

  doit systemctl --user daemon-reload
  doit systemctl --user enable --now sesh-ship.service
  say "installed and started."
  if [ "$(loginctl show-user "$USER" --property=Linger --value 2>/dev/null || echo unknown)" != "yes" ]; then
    say "NOTE: lingering is not enabled — the unit will NOT survive reboot on a"
    say "      node nobody logs into. Run: loginctl enable-linger $USER"
  fi
}

render_plist() {
  sed \
    -e "s|@SESH_BIN@|$SESH_BIN|g" \
    -e "s|@SESH_STORE_URL@|$STORE_URL|g" \
    -e "s|@HOME@|$HOME|g" \
    "$ETC_DIR/launchd/dev.sesh.ship.plist.tmpl"
}

install_darwin() {
  local agents_dir="$HOME/Library/LaunchAgents"
  local plist="$agents_dir/dev.sesh.ship.plist"

  # Same preservation rule as the Linux drop-in: a differing existing plist
  # is operator state; refuse before any write unless --force.
  if [ -f "$plist" ] && [ "$FORCE" -eq 0 ] && ! render_plist | cmp -s - "$plist"; then
    echo "install-ship.sh: $plist exists with different content — refusing to overwrite." >&2
    echo "  Re-run with --force to replace it. Nothing was written." >&2
    exit 1
  fi

  say "rendering launchd agent into $agents_dir"
  doit mkdir -p "$agents_dir" "$HOME/Library/Logs"
  render_plist | emit "$plist"
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
