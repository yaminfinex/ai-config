#!/usr/bin/env bash
# fence-display.sh - Claude Code MessageDisplay hook: draw <status> and
# <internal> fences differently in the terminal. Display only: the transcript,
# the model, herder web and ctrl+o keep the original text.
#
# Rendering (plain text, drawn where the delta was):
#   <status>BODY</status>  (bare tags, one line)  -> "· BODY"  (empty -> "· status")
#   <internal> ... </internal>                     -> one line "▸ internal note · N lines"
#       in place of the opener; body lines and the closer draw as nothing.
#       When the block spans batches the opener is drawn once as "▸ internal note"
#       (count unknown) and later batches of the body draw as nothing.
#   Anything else (text, misspelt/attributed/backticked tags, lines inside a
#   ``` code fence) passes through unchanged. With nothing to change the hook
#   emits no displayContent so Claude Code draws the original.
#
# Grammar contract: tools/herder/web/src/features/transcript/fencingModel.ts and
# docs/fencing-convention.md. Accepted divergence: an <internal> left unclosed at
# `final` draws nothing extra here (its body was already suppressed batch by
# batch), whereas the web parser fails the whole message open to literal text.
#
# State: one file per message_id under ${XDG_RUNTIME_DIR:-/tmp}/fence-display-$UID
# (mode 0700) holding "<open|closed> <body-lines> <in-code-fence>", created
# lazily, removed on final; files older than a day are pruned opportunistically.
#
# Robustness law: every failure exits 0 with no output (original drawn).
# Dependencies: bash and jq. Runtime per batch: tens of milliseconds.

set -u

jq_bin=$(command -v jq) || exit 0
input=$(cat 2>/dev/null) || exit 0
[ -n "$input" ] || exit 0

message_id='' final='' delta=''
{
  IFS= read -r -d '' message_id
  IFS= read -r -d '' final
  IFS= read -r -d '' delta
} < <(
  printf '%s' "$input" | "$jq_bin" -j '
    select(type == "object" and .hook_event_name == "MessageDisplay"
      and (.message_id | type) == "string" and (.delta | type) == "string")
    | .message_id, "\u0000", (.final == true | tostring), "\u0000", .delta' 2>/dev/null
  printf '\0'
)
[ -n "$message_id" ] || exit 0
[[ "$message_id" =~ ^[A-Za-z0-9._-]{1,128}$ ]] || exit 0
[[ "$final" == true || "$final" == false ]] || exit 0

state_dir="${XDG_RUNTIME_DIR:-/tmp}/fence-display-$(id -u 2>/dev/null || echo 0)"
if [ ! -d "$state_dir" ]; then mkdir -m 0700 "$state_dir" 2>/dev/null || exit 0; fi
[ -w "$state_dir" ] || exit 0
state_file="$state_dir/$message_id"
if command -v find >/dev/null 2>&1; then
  find "$state_dir" -maxdepth 1 -type f -mmin +1440 -delete 2>/dev/null || true
fi

# Load state: block open?, running body line count, inside a ``` fence?
open=closed count=0 fence=0
if [ -f "$state_file" ]; then
  read -r open count fence <"$state_file" 2>/dev/null || { open=closed; count=0; fence=0; }
  [[ "$open" == open || "$open" == closed ]] || open=closed
  [[ "$count" =~ ^[0-9]+$ ]] || count=0
  [[ "$fence" == 0 || "$fence" == 1 ]] || fence=0
fi

tag_re='</?(internal|status)>'
status_re='^[[:space:]]*<status>(.*)</status>[[:space:]]*$'
open_re='^[[:space:]]*<internal>(.*)$'
oneline_re='^(.*)</internal>[[:space:]]*$'
fence_re='^[[:space:]]{0,3}(```|~~~)'

out=()          # rendered lines for this batch, each with its own newline
changed=0
opener_idx=-1   # index in out[] of an opener drawn in this batch

emit() { out+=("$1"); }

# Split the delta into lines, keeping each terminating newline.
rest="$delta"
while [ -n "$rest" ]; do
  if [[ "$rest" == *$'\n'* ]]; then
    line="${rest%%$'\n'*}"; nl=$'\n'; rest="${rest#*$'\n'}"
  else
    line="$rest"; nl=''; rest=''
  fi

  if [ "$open" = open ]; then
    changed=1
    if [[ "$line" =~ $oneline_re ]]; then
      pre="${BASH_REMATCH[1]}"
      [[ "$pre" =~ [^[:space:]] ]] && count=$((count + 1))
      open=closed
      if [ "$opener_idx" -ge 0 ]; then
        out[opener_idx]="▸ internal note · $count lines"$'\n'
        opener_idx=-1
      fi
      count=0
    else
      count=$((count + 1))
    fi
    continue
  fi

  if [[ "$line" =~ $fence_re ]]; then
    fence=$((1 - fence)); emit "$line$nl"; continue
  fi
  if [ "$fence" = 1 ]; then emit "$line$nl"; continue; fi

  if [[ "$line" =~ $status_re ]]; then
    body="${BASH_REMATCH[1]}"
    if [[ ! "$body" =~ $tag_re ]]; then
      changed=1
      [ -n "$body" ] || body=status
      emit "· $body"$'\n'; continue
    fi
  fi

  if [[ "$line" =~ $open_re ]]; then
    after="${BASH_REMATCH[1]}"
    if [[ "$after" =~ $oneline_re ]]; then
      inner="${BASH_REMATCH[1]}"
      if [[ ! "$inner" =~ $tag_re ]]; then
        changed=1; n=0; [[ "$inner" =~ [^[:space:]] ]] && n=1
        emit "▸ internal note · $n lines"$'\n'; continue
      fi
    elif [[ ! "$after" =~ $tag_re ]]; then
      changed=1; open=open; count=0
      [[ "$after" =~ [^[:space:]] ]] && count=1
      opener_idx=${#out[@]}
      emit "▸ internal note"$'\n'; continue
    fi
  fi

  emit "$line$nl"
done

# Persist or clear state.
if [ "$final" = true ]; then
  rm -f -- "$state_file" 2>/dev/null
elif [ "$open" = open ] || [ "$fence" = 1 ] || [ -f "$state_file" ]; then
  printf '%s %s %s\n' "$open" "$count" "$fence" >"$state_file" 2>/dev/null || exit 0
fi

[ "$changed" = 1 ] || exit 0
content=""
for l in "${out[@]+"${out[@]}"}"; do content+="$l"; done
# The single-quoted jq program contains a jq variable, not a shell expansion.
# shellcheck disable=SC2016
"$jq_bin" -cn --arg c "$content" \
  '{hookSpecificOutput:{hookEventName:"MessageDisplay",displayContent:$c}}' 2>/dev/null || true
exit 0
