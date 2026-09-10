#!/usr/bin/env bash

set -u

if [ -x /usr/bin/jq ]; then
  jq_bin=/usr/bin/jq
elif [ -x /bin/jq ]; then
  jq_bin=/bin/jq
else
  jq_bin="$(command -v jq 2>/dev/null)" || exit 0
fi
IFS=$'\t' read -r session_id event source < <(
  "$jq_bin" -er '[.session_id, .hook_event_name, (.source // "")] | select(all(.[]; type == "string")) | @tsv' 2>/dev/null
) || exit 0
[[ "$session_id" =~ ^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$ ]] || exit 0

if [ -n "${XDG_STATE_HOME:-}" ]; then
  state_base="$XDG_STATE_HOME"
elif [ -n "${HOME:-}" ]; then
  state_base="$HOME/.local/state"
else
  exit 0
fi
state_dir="$state_base/ai-config/context-nudge"
marker="$state_dir/claude-$session_id.bands"

if [ "$event" = "SessionStart" ]; then
  [ "$source" = "compact" ] && rm -f -- "$marker" >/dev/null 2>&1
  exit 0
fi
case "$event" in UserPromptSubmit|PostToolUse) ;; *) exit 0 ;; esac

band_config="${AI_CONTEXT_NUDGE_BANDS:-200000,250000}"
[[ "$band_config" =~ ^[1-9][0-9]*(,[1-9][0-9]*)*$ ]] || exit 0
IFS=, read -r -a bands <<<"$band_config"
previous=0
for band in "${bands[@]}"; do
  (( 10#$band > previous )) || exit 0
  previous=$((10#$band))
done

command -v timeout >/dev/null 2>&1 || exit 0
command -v herder >/dev/null 2>&1 || exit 0
snapshot="$(timeout 2s herder show --session "$session_id" --json 2>/dev/null)" || exit 0
used="$("$jq_bin" -er '.vitals.context_usage.used_tokens | select(type == "number" and . >= 0 and floor == .) | tostring' <<<"$snapshot" 2>/dev/null)" || exit 0
[[ "$used" =~ ^[0-9]+$ ]] || exit 0

existing=""
warned_bands=()
if [ -e "$marker" ]; then
  existing="$(cat "$marker" 2>/dev/null)" || exit 0
fi
while IFS= read -r warned; do
  [ -z "$warned" ] || [[ "$warned" =~ ^[1-9][0-9]*$ ]] || exit 0
  [ -z "$warned" ] || warned_bands+=("$warned")
done <<<"$existing"

new=()
for band in "${bands[@]}"; do
  already_warned=0
  for warned in "${warned_bands[@]}"; do
    [ "$warned" = "$band" ] && already_warned=1
  done
  if (( 10#$used >= 10#$band && already_warned == 0 )); then
    new+=("$band")
  fi
done
[ "${#new[@]}" -gt 0 ] || exit 0

mkdir -p "$state_dir" >/dev/null 2>&1 || exit 0
tmp="$(mktemp "$state_dir/.bands.XXXXXX" 2>/dev/null)" || exit 0
trap 'rm -f "$tmp"' EXIT
[ -z "$existing" ] || printf '%s\n' "$existing" >"$tmp" || exit 0
printf '%s\n' "${new[@]}" >>"$tmp" || exit 0
sort -n -u -o "$tmp" "$tmp" 2>/dev/null || exit 0
mv "$tmp" "$marker" 2>/dev/null || exit 0

crossed="$(IFS=,; printf '%s' "${new[*]}")"
# The reminder deliberately names $AI_CONFIG_ROOT for the receiving agent.
# shellcheck disable=SC2016
printf -v reminder 'Context reminder: the last completed request used %s input-context tokens and crossed your %s-token warning band. Compact now or notify your orchestrator now with your token count and next safe boundary. If you are orchestrating, first rewrite your state/handoff to current truth, then run $AI_CONFIG_ROOT/tools/fleet/selfcompact.sh <own-hcom-name> '\''<what to preserve>'\'' '\''<explicit continuation>'\'', end this turn so it can run, and resume from the continuation. If you are a worker, compact at the next safe boundary using /compact, or finish the current bounded unit and immediately report for compaction or pickup. If someone manages you, send that manager an hcom update now; use your current manager, otherwise the launcher'\''s reply-to instruction. If no orchestrator is known, own the compaction yourself. This reminder does not block work: do not stop to wait for permission, and do not abandon the task. This band is warned once per compaction cycle.' "$used" "$crossed"
# The single-quoted jq program contains jq variables, not shell expansions.
# shellcheck disable=SC2016
"$jq_bin" -cn --arg event "$event" --arg text "$reminder" \
  '{hookSpecificOutput:{hookEventName:$event,additionalContext:$text}}' 2>/dev/null || true
