#!/usr/bin/env bash
# Claude SessionStart hook: on compact or resume, re-inject the current rendered
# docs/hcom-launch-notes.txt so an hcom seat never runs on its launch-time copy.
# Every failure path exits zero without output so the hook never blocks work.

set -u

[ "${HCOM_LAUNCHED:-}" = 1 ] || exit 0
jq_bin=$(command -v jq) || exit 0
IFS=$'\t' read -r event start_source < <(
  "$jq_bin" -er '[.hook_event_name, (.source // "")] | select(all(.[]; type == "string")) | @tsv' 2>/dev/null
) || exit 0
[ "$event" = "SessionStart" ] || exit 0
case "$start_source" in compact|resume) ;; *) exit 0 ;; esac

# The installed copy under ~/.claude/hooks is a symlink into the checkout.
self=$(readlink -f -- "${BASH_SOURCE[0]}" 2>/dev/null) || exit 0
root=${self%/claude/hooks/notes-refresh.sh}
[ "$root" != "$self" ] && [ -n "$root" ] || exit 0
notes_file="$root/docs/hcom-launch-notes.txt"
[ -s "$notes_file" ] || exit 0
notes=$(<"$notes_file") 2>/dev/null || exit 0

# Same rendering as tools/fleet/apply-hcom-notes.sh.
notes=${notes//__AI_CONFIG_ROOT__/$root}
[ -n "$notes" ] || exit 0
[[ $notes != *__AI_CONFIG_ROOT__* ]] || exit 0

header="Current hcom NOTES section, re-read from $notes_file after $start_source; it supersedes any earlier NOTES section in this session."
# The single-quoted jq program contains jq variables, not shell expansions.
# shellcheck disable=SC2016
"$jq_bin" -cn --arg text "$header"$'\n\n'"$notes" \
  '{hookSpecificOutput:{hookEventName:"SessionStart",additionalContext:$text}}' 2>/dev/null || true
