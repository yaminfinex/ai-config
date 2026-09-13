#!/usr/bin/env bash
# hcom fleet terminal-preset open helper.
#
# Contract: the caller sets FLEET_PANE to an existing, idle shell pane. The
# helper prints that pane id as the first stdout line (hcom captures it as
# {id}), stamps hcom's generated pane title, and runs bash {script} there.
#
# Git identity: the seat inherits the pane cwd's git identity (user.name and
# user.email as `git config` resolves them there: repo, then global) pinned
# through GIT_AUTHOR_* and GIT_COMMITTER_*. Those outrank `git -c user.email`
# and `--author` cannot move the committer, so a seat cannot commit under an
# address the checkout does not own. Nothing is hardcoded here; a checkout
# with no resolvable identity is refused before the pane is touched.

set -euo pipefail

die() {
  printf 'fleet spawn-pane: %s\n' "$*" >&2
  exit 1
}

[[ $# -eq 2 ]] || die "usage: spawn-pane.sh <hcom-script> <pane-title>"
[[ -n ${FLEET_PANE:-} ]] || die "FLEET_PANE is required"
[[ ${FLEET_TOOL:-} =~ ^(claude|codex)$ ]] || die "FLEET_TOOL must be claude or codex"

launch_script=$1
pane_title=$2

[[ -f $launch_script ]] || die "hcom launch script does not exist: $launch_script"
command -v git >/dev/null || die "git is required"
command -v jq >/dev/null || die "jq is required"
pane_output=$(herdr pane get "$FLEET_PANE") || die "pane does not exist: $FLEET_PANE"
pane_cwd=$(jq -er '.result.pane.foreground_cwd // .result.pane.cwd | select(length > 0)' <<<"$pane_output") \
  || die "pane has no cwd: $FLEET_PANE"
[[ -d $pane_cwd ]] || die "pane cwd is not a directory: $pane_cwd"

git_name=$(git -C "$pane_cwd" config --get user.name 2>/dev/null || true)
git_email=$(git -C "$pane_cwd" config --get user.email 2>/dev/null || true)
[[ -n $git_name && -n $git_email ]] \
  || die "no git identity resolves at $pane_cwd (user.name and user.email must be set in the checkout or globally)"
printf -v git_name_q '%q' "$git_name"
printf -v git_email_q '%q' "$git_email"
identity_env="GIT_AUTHOR_NAME=$git_name_q GIT_AUTHOR_EMAIL=$git_email_q GIT_COMMITTER_NAME=$git_name_q GIT_COMMITTER_EMAIL=$git_email_q"

# This must remain the first stdout line: hcom parses {id} from it.
printf '%s\n' "$FLEET_PANE"
herdr pane rename "$FLEET_PANE" "$pane_title" >/dev/null
printf -v launch_script_q '%q' "$launch_script"
herdr pane run "$FLEET_PANE" "$identity_env HERDR_AGENT=$FLEET_TOOL bash $launch_script_q" >/dev/null
