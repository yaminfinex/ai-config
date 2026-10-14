#!/bin/sh -eu
# deploy-store.sh <store-host> <binary> — the shipping half of
# `just deploy-store`. ops/deploy-remote.sh does the crash-safe swap and
# restart (see its header); this only gets it and the binary onto the host.
#
# <store-host> is an ssh destination ([user@]host). Run on that host itself
# (hostname matches), the swap runs in place under sudo; from anywhere else
# it is one scp and one ssh, so any machine with ssh access to the host can
# deploy. The remote user needs sudo there.

HOST="${1:-}"
BIN="${2:-}"
[ -n "$HOST" ] && [ -n "$BIN" ] || { echo "usage: deploy-store.sh <[user@]store-host> <binary>" >&2; exit 2; }
[ -f "$BIN" ] || { echo "deploy-store.sh: binary not found: $BIN" >&2; exit 1; }
OPS="$(cd "$(dirname "$0")/../ops" && pwd)"

if [ "$(hostname)" = "${HOST#*@}" ]; then
  exec sudo sh "$OPS/deploy-remote.sh" "$BIN"
fi
NAME=$(basename "$BIN")
scp "$BIN" "$OPS/deploy-remote.sh" "$HOST:/tmp/"
ssh "$HOST" "sudo sh /tmp/deploy-remote.sh /tmp/$NAME"
