#!/usr/bin/env bash
# hcom fleet terminal-preset open helper.
#
# Contract: the caller sets FLEET_PANE to an existing, idle shell pane. The
# helper prints that pane id as the first stdout line (hcom captures it as
# {id}), stamps hcom's generated pane title, and runs bash {script} there.

set -euo pipefail

die() {
  printf 'fleet spawn-pane: %s\n' "$*" >&2
  exit 1
}

[[ $# -eq 2 ]] || die "usage: spawn-pane.sh <hcom-script> <pane-title>"
[[ -n ${FLEET_PANE:-} ]] || die "FLEET_PANE is required"

launch_script=$1
pane_title=$2

[[ -f $launch_script ]] || die "hcom launch script does not exist: $launch_script"
herdr pane get "$FLEET_PANE" >/dev/null || die "pane does not exist: $FLEET_PANE"

# This must remain the first stdout line: hcom parses {id} from it.
printf '%s\n' "$FLEET_PANE"
herdr pane rename "$FLEET_PANE" "$pane_title" >/dev/null
printf -v launch_script_q '%q' "$launch_script"
herdr pane run "$FLEET_PANE" "bash $launch_script_q" >/dev/null
