#!/usr/bin/env bash
# The one way the shared Rust build cache (mbx, /mnt/xfs-nvme/mbx) is pruned.
# mbx never collects on its own here (gc.auto = false in
# ~/.config/mbx/config.toml), so nothing is deleted until a caller runs this
# with --apply. Without --apply it only previews. The budgets come from that
# config: gc.max_total_size and gc.min_free_size. Before running mbx at all it
# proves those premises from mbx's effective settings and refuses otherwise.
# Nothing calls this script implicitly; any cadence is the operator's call.
#   prune-build-cache.sh            preview what `mbx gc` would remove
#   prune-build-cache.sh --apply    run `mbx gc`

set -euo pipefail

CACHE_MOUNT=${CACHE_MOUNT:-/mnt/xfs-nvme}

die() {
  printf 'fleet prune-build-cache: %s\n' "$*" >&2
  exit 1
}

setting() {
  "$MBX_BIN" settings get "$1" || die "cannot read mbx setting $1"
}

apply=0
case $# in
  0) ;;
  1) [[ $1 == --apply ]] || die "usage: prune-build-cache.sh [--apply]"; apply=1 ;;
  *) die "usage: prune-build-cache.sh [--apply]" ;;
esac

# mbx is the mr-boxington tool in the global mise config, asked for from ~ so
# that an untrusted or unrelated project config in the cwd cannot shadow it;
# MBX_BIN overrides.
if [[ -z ${MBX_BIN:-} ]]; then
  MBX_BIN=$(cd ~ && mise which mbx 2>/dev/null) || MBX_BIN=$(command -v mbx) \
    || die "mbx not found: neither \`mise which mbx\` nor PATH has it"
fi
[[ -x $MBX_BIN ]] || die "mbx not found at $MBX_BIN"
mountpoint -q -- "$CACHE_MOUNT" || die "$CACHE_MOUNT is not mounted"

# mbx prints an empty line for an unset key.
cache_dir=$(setting cache_dir)
[[ -n $cache_dir ]] || die "mbx cache_dir is unset, so mbx would use its default cache, not $CACHE_MOUNT"
mount_real=$(realpath -- "$CACHE_MOUNT")
cache_real=$(realpath -m -- "$cache_dir")
[[ $cache_real == "${mount_real%/}"/* ]] || die "mbx cache_dir $cache_dir is not under $CACHE_MOUNT"
[[ $(setting gc.auto) == false ]] || die "mbx gc.auto is not false, so this is not the only path that deletes"
for key in gc.max_total_size gc.min_free_size; do
  [[ -n $(setting "$key") ]] || die "mbx $key is unset, so mbx would prune to its default budget"
done

df -h --output=target,used,avail -- "$CACHE_MOUNT"
if ((apply)); then
  "$MBX_BIN" gc
  df -h --output=target,used,avail -- "$CACHE_MOUNT"
else
  "$MBX_BIN" gc --dry-run
  printf 'fleet prune-build-cache: preview only; rerun with --apply to remove\n'
  printf 'fleet prune-build-cache: while the disk is under gc.min_free_size, --apply can remove more than this preview lists\n'
fi
