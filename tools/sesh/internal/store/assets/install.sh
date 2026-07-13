#!/bin/sh
# sesh installer — served by the sesh store itself, so the URL this script
# was fetched from IS the store URL (design 2026-07-12, option 1c). Reaching
# it over the tailnet is the access check. It ends by running
# `sesh setup --store-url $BASE`, so onboarding and URL migration are one
# command:  curl http://sesh.<tailnet>.ts.net:8765/install.sh | sh
# Extra args pass through to sesh setup:  ... | sh -s -- --force
set -eu

# Single-quoted and host-validated server-side: no expansion, no injection.
BASE='{{BASE}}'

OS=$(uname -s)
case "$OS" in
  Darwin) OS=darwin ;;
  Linux)  OS=linux ;;
  *) echo "sesh: unsupported OS: $OS (darwin and linux only)" >&2; exit 1 ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
  arm64|aarch64) ARCH=arm64 ;;
  x86_64|amd64)  ARCH=amd64 ;;
  *) echo "sesh: unsupported architecture: $ARCH (arm64 and amd64 only)" >&2; exit 1 ;;
esac

# latest is read exactly once; every fetch below uses immutable
# /releases/<ver>/ paths, so a `latest` flip mid-download cannot mix
# artifacts from two releases. Edge whitespace is trimmed (never interior —
# deleting bytes would hide corruption), then the value must match the
# release shape exactly, kept in lockstep with scripts/release.sh and
# internal/update/update.go (versionRE): a corrupt channel value (field bug:
# a mangled publish served 'sesh-v0.1.0n') must fail HERE, loudly, instead
# of becoming a 404 download URL.
VER=$(curl -fsSL "$BASE/releases/latest/VERSION" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
case "$VER" in
  ''|*[!A-Za-z0-9._-]*)
    echo "sesh: malformed version from $BASE/releases/latest/VERSION: '$VER' — refusing to build a download URL from it" >&2
    exit 1 ;;
esac
printf '%s' "$VER" | grep -Eq '^(sesh-)?v[0-9]+\.[0-9]+\.[0-9]+(-[0-9]+-g[0-9a-f]+)?$|^[0-9a-f]{7,40}$' || {
  echo "sesh: malformed version from $BASE/releases/latest/VERSION: '$VER' (want [sesh-]vX.Y.Z[-N-g<hash>] or a commit hash) — refusing to build a download URL from it" >&2
  exit 1
}
echo "installing sesh $VER ($OS-$ARCH) from $BASE ..."

SUMS=$(mktemp)
DEST="$HOME/.local/bin"
mkdir -p "$DEST"
TMP="$DEST/.sesh-install.$$.tmp"
trap 'rm -f "$SUMS" "$TMP"' EXIT

curl -fsSL "$BASE/releases/$VER/sesh-$OS-$ARCH" -o "$TMP"
curl -fsSL "$BASE/releases/$VER/SHA256SUMS" -o "$SUMS"

WANT=$(awk -v f="sesh-$OS-$ARCH" '$2 == f || $2 == "*"f {print $1}' "$SUMS")
[ -n "$WANT" ] || { echo "sesh: SHA256SUMS has no entry for sesh-$OS-$ARCH" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  GOT=$(sha256sum "$TMP" | cut -d' ' -f1)
elif command -v shasum >/dev/null 2>&1; then
  GOT=$(shasum -a 256 "$TMP" | cut -d' ' -f1)
else
  echo "sesh: need sha256sum or shasum to verify the download" >&2; exit 1
fi
[ "$GOT" = "$WANT" ] || {
  echo "sesh: checksum mismatch for sesh-$OS-$ARCH (got $GOT, want $WANT) — aborting" >&2
  exit 1
}

# Temp file lives in DEST so this rename is atomic; a running service keeps
# its open inode and the next start sees the new file.
chmod 0755 "$TMP"
mv "$TMP" "$DEST/sesh"

case ":$PATH:" in
  *":$DEST:"*) ;;
  *) echo "sesh: note: $DEST is not on your PATH (the service does not need it; your shell does)" ;;
esac

# Hand off to the binary: unit render, store-URL drop-in (DP-4b provenance
# rules), service start. All service logic lives in Go where it is testable.
exec "$DEST/sesh" setup --store-url "$BASE" "$@"
