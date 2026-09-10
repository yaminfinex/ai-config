#!/usr/bin/env bash
# Claude context-band advisory hook over herder's session view.
# Every failure path exits zero without output so the hook never blocks work.

set -u

jq_bin=$(command -v jq) || exit 0
IFS=$'\t' read -r session_id event start_source < <(
  "$jq_bin" -er '[.session_id, .hook_event_name, (.source // "")] | select(all(.[]; type == "string")) | @tsv' 2>/dev/null
) || exit 0
[[ "$session_id" =~ ^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$ ]] || exit 0

state_dir="${XDG_STATE_HOME:-${HOME:-/nonexistent}/.local/state}/ai-config/context-nudge"
marker="$state_dir/claude-$session_id.bands"

if [ "$event" = "SessionStart" ]; then
  [ "$start_source" = "compact" ] && rm -f -- "$marker" >/dev/null 2>&1
  exit 0
fi
case "$event" in UserPromptSubmit|PostToolUse) ;; *) exit 0 ;; esac

band_config="${AI_CONTEXT_NUDGE_BANDS:-200000,250000}"
[[ "$band_config" =~ ^[1-9][0-9]*(,[1-9][0-9]*)*$ ]] || exit 0
IFS=, read -r -a bands <<<"$band_config"
previous=0
for band in "${bands[@]}"; do
  (( band > previous )) || exit 0
  previous=$band
done

row="$(timeout 2s herder show --session "$session_id" --json 2>/dev/null)" || exit 0
used="$("$jq_bin" -er '.vitals.context_usage.used_tokens | select(type == "number" and . >= 0 and floor == .) | tostring' <<<"$row" 2>/dev/null)" || exit 0

warned=""; [ -f "$marker" ] && warned="$(<"$marker")"

new=()
for band in "${bands[@]}"; do
  if (( used >= band )) && [[ $'\n'"$warned"$'\n' != *$'\n'"$band"$'\n'* ]]; then
    new+=("$band")
  fi
done
[ "${#new[@]}" -gt 0 ] || exit 0

mkdir -p "$state_dir" 2>/dev/null && printf '%s\n' "${new[@]}" >>"$marker" 2>/dev/null || exit 0

crossed="$(IFS=,; printf '%s' "${new[*]}")"
# memo §4 text, verbatim
# shellcheck disable=SC2016
printf -v reminder 'Context reminder: the last completed request used %s input-context tokens and crossed your %s-token warning band. Compact now or notify your orchestrator now with your token count and next safe boundary. If you are orchestrating, first rewrite your state/handoff to current truth, then run $AI_CONFIG_ROOT/tools/fleet/selfcompact.sh <own-hcom-name> '\''<what to preserve>'\'' '\''<explicit continuation>'\'', end this turn so it can run, and resume from the continuation. If you are a worker, compact at the next safe boundary using /compact, or finish the current bounded unit and immediately report for compaction or pickup. If someone manages you, send that manager an hcom update now; use your current manager, otherwise the launcher'\''s reply-to instruction. If no orchestrator is known, own the compaction yourself. This reminder does not block work: do not stop to wait for permission, and do not abandon the task. This band is warned once per compaction cycle.' "$used" "$crossed"
# The single-quoted jq program contains jq variables, not shell expansions.
# shellcheck disable=SC2016
"$jq_bin" -cn --arg event "$event" --arg text "$reminder" \
  '{hookSpecificOutput:{hookEventName:$event,additionalContext:$text}}' 2>/dev/null || true
