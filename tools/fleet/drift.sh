#!/usr/bin/env bash
# Read-only fleet placement drift report. One line per finding:
#   shared-tab tab=ID agents=N panes=ID,ID   a tab holding more than one agent pane
#   idle-shell pane=ID tab=ID cwd=PATH       a pane holding only its idle shell
#   orphan-db pid=N port=P data=DIR          a worktree Postgres no live seat or pane is in
#   orphan-valkey pid=N port=P dir=DIR       the same for a worktree Valkey
# An orphan's DIR is <linked worktree>/data/postgres or /data/valkey, suffixed
# " (gone)" once that dir no longer exists (the worktree was removed under a
# running server). Main checkouts, the shared :5433 Postgres and the trace
# store are never listed. It never closes, moves, renames or stops anything;
# cull.sh and the operator act.

set -euo pipefail

fleet_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=tools/fleet/lib.sh
source "$fleet_dir/lib.sh"

die() {
  printf 'fleet drift: %s\n' "$*" >&2
  exit 1
}

[[ $# -eq 0 ]] || die "usage: drift.sh"
command -v jq >/dev/null || die "jq is required"
command -v herdr >/dev/null || die "herdr is required"

panes=$(herdr pane list) || die "cannot read herdr panes"

jq -r '
  [.result.panes[]? | select(.agent != null and .tab_id != null)]
  | group_by(.tab_id)[]
  | select(length > 1)
  | "shared-tab tab=\(.[0].tab_id) agents=\(length) panes=\(map(.pane_id) | join(","))"
' <<<"$panes"

# Every pane is inspected: a pane herdr still marks as an agent's can be left
# at a bare shell after its agent exits.
jq -r '.result.panes[]? | [.pane_id, (.tab_id // "unknown"), (.foreground_cwd // .cwd // "unknown")] | @tsv' <<<"$panes" \
  | while IFS=$'\t' read -r pane_id tab_id pane_cwd; do
    process_output=$(herdr pane process-info --pane "$pane_id" </dev/null 2>/dev/null) || continue
    if fleet_idle_shell "$process_output"; then
      printf 'idle-shell pane=%s tab=%s cwd=%s\n' "$pane_id" "$tab_id" "$pane_cwd"
    fi
  done

# Worktree servers: listed only when the data dir is <linked worktree>/data/
# <postgres|valkey> (or such a dir is gone) and no live seat or pane is in
# that worktree. Without a readable seat roster no orphan can be proven.
if ! roster=$(hcom list --json 2>/dev/null); then
  printf 'fleet drift: cannot read hcom seats; orphan servers not checked\n' >&2
  exit 0
fi
fleet_db_servers | while IFS=$'\t' read -r kind pid port dir gone; do
  case $kind:$dir in
    postgres:*/data/postgres) wt=${dir%/data/postgres} label=orphan-db key=data ;;
    valkey:*/data/valkey) wt=${dir%/data/valkey} label=orphan-valkey key=dir ;;
    *) continue ;;
  esac
  if [[ -d $wt ]]; then
    top=$(fleet_linked_worktree "$wt") || continue
    [[ $(realpath -e -- "$top") == "$(realpath -e -- "$wt")" ]] || continue
  fi
  owners=$(fleet_worktree_owners "$wt" "$roster" "$panes") || {
    printf 'fleet drift: malformed seat or pane list; orphan servers not checked\n' >&2
    exit 0
  }
  [[ -z $owners ]] || continue
  suffix=
  ((gone == 0)) || suffix=' (gone)'
  printf '%s pid=%s port=%s %s=%s%s\n' "$label" "$pid" "$port" "$key" "$dir" "$suffix"
done
