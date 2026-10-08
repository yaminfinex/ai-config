#!/usr/bin/env bash
# Read-only fleet placement drift report. One line per finding:
#   shared-tab tab=ID agents=N panes=ID,ID   a tab holding more than one agent pane
#   idle-shell pane=ID tab=ID cwd=PATH       a pane holding only its idle shell
# It never closes, moves or renames anything; cull.sh and the operator act.

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
