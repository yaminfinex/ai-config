#!/usr/bin/env bash
# fence-display.sh - Claude Code MessageDisplay hook: draw <status> and
# <internal> fences differently in the terminal. Display only: the transcript,
# the model, herder web and ctrl+o keep the original text.
#
# Grammar contract: tools/herder/web/src/features/transcript/fencingModel.ts and
# docs/fencing-convention.md. The four bare lowercase tags are recognised
# anywhere, with no Markdown grammar: inside backticks, inside a code fence,
# mid-line. A tag with an attribute, other casing or other spelling is text.
#
# Rendering (plain text, drawn where the delta was):
#   <status>BODY</status> on one line -> "· BODY" in place of the pair, text
#       before and after the pair kept (empty BODY -> "· status").
#   <internal> starting a line ... </internal> ending a line -> one line
#       "▸ internal note · N lines" in place of the opener; body lines and the
#       closer draw as nothing. Text on the opener or closer line counts as a
#       body line. When the block spans batches the opener is drawn once as
#       "▸ internal note" (count unknown) and later batches of the body draw
#       as nothing. With nothing to change, no displayContent is emitted.
#
# Structural divergences from the web parser, settled:
#   - Line granularity. A <status> whose closer is on another line, a status
#     body holding a carriage return, a stray closer with no open block, and a
#     line mixing a tag with anything unparsable all pass through with their
#     bytes untouched. Internal openers outside line start or closers outside
#     line end stay literal, including inline and backticked pairs with
#     surrounding text.
#   - Bounded loss. Streaming cannot undo already-drawn batches, so the hook
#     never fails a whole message open. Inside an open internal block, a line
#     holding any bare tag other than the closer ends the note right there
#     (count so far); that line and everything after it in the batch draw
#     literally. An internal block left open at final draws nothing extra.
#     What the terminal may lose is only body lines of an internal block that
#     later proves malformed, never a status line or plain text; ctrl+o has
#     the whole text.
#
# State: one file per message_id under ${XDG_RUNTIME_DIR:-/tmp}/fence-display-$UID
# (mode 0700) holding "<open|closed> <body-lines>", created lazily, removed on
# final; files older than a day are pruned opportunistically. Batches are not
# synchronised against each other (Claude Code serialises them); a late writer
# can leave a stale file behind, which the daily prune removes.
#
# Robustness law: every failure exits 0 with no output (original drawn).
# Dependencies: bash and jq (find, when present, does the pruning).

# shellcheck disable=SC2053  # $tag_glob / $bad_body_glob are patterns on purpose
set -u

jq_bin=$(command -v jq) || exit 0
input=$(cat 2>/dev/null) || exit 0
[ -n "$input" ] || exit 0

state_dir="${XDG_RUNTIME_DIR:-/tmp}/fence-display-$(id -u 2>/dev/null || echo 0)"
if [ ! -d "$state_dir" ]; then mkdir -m 0700 "$state_dir" 2>/dev/null || exit 0; fi
[ -w "$state_dir" ] || exit 0

# jq writes "message_id NUL final NUL delta" to a private seekable file: bash
# reads a regular file buffered, but a pipe one byte at a time, which alone
# costs half a second on a 20 000-line batch.
parsed=$(mktemp "$state_dir/.parse.XXXXXX" 2>/dev/null) || exit 0
trap 'rm -f -- "$parsed"' EXIT
printf '%s' "$input" | "$jq_bin" -j '
  select(type == "object" and .hook_event_name == "MessageDisplay"
    and (.message_id | type) == "string" and (.delta | type) == "string")
  | .message_id, "\u0000", (.final == true | tostring), "\u0000", .delta' >"$parsed" 2>/dev/null || exit 0
exec 3<"$parsed" || exit 0
message_id='' final=''
IFS= read -r -d '' message_id <&3
IFS= read -r -d '' final <&3
[ -n "$message_id" ] || exit 0
[[ "$message_id" =~ ^[A-Za-z0-9._-]{1,128}$ ]] || exit 0
[[ "$final" == true || "$final" == false ]] || exit 0

state_file="$state_dir/$message_id"
if command -v find >/dev/null 2>&1; then
  find "$state_dir" -maxdepth 1 -type f -mmin +1440 -delete 2>/dev/null || true
fi

# Load state: block open?, running body line count.
open=closed count=0
if [ -f "$state_file" ]; then
  read -r open count <"$state_file" 2>/dev/null || { open=closed; count=0; }
  [[ "$open" == open || "$open" == closed ]] || open=closed
  [[ "$count" =~ ^[0-9]+$ ]] || count=0
fi

# $tag_glob matches text holding any of the four bare tags; $bad_body_glob
# also catches a carriage return. One extglob test per check beats a regex or
# a function call at 20 000 lines a batch.
shopt -s extglob
tag_glob='*<?(/)@(status|internal)>*'
bad_body_glob="*@(<?(/)@(status|internal)>|"$'\r'")*"
open_re='^[[:space:]]*<internal>(.*)$'
close_re='^(.*)</internal>[[:space:]]*$'

out=()          # rendered lines for this batch, each with its own line ending
changed=0
opener_idx=-1   # index in out[] of an opener drawn in this batch
literal=0       # set once a malformed block ended the note: the rest draws as-is

end_note() {
  open=closed
  if [ "$opener_idx" -ge 0 ]; then
    out[opener_idx]="▸ internal note · $count lines"$'\n'
    opener_idx=-1
  fi
  count=0
}

# Linear pass over the delta, which is the rest of fd 3. `read` fails only on a
# final unterminated line, leaving it in $line; that line gets no line ending.
line='' nl=$'\n'
while IFS= read -r line <&3 || { nl=''; [ -n "$line" ]; }; do
  if [[ "$literal" == 1 ]]; then out+=("$line$nl"); continue; fi

  if [[ "$open" == open ]]; then
    changed=1
    pre=''; [[ "$line" =~ $close_re ]] && pre="${BASH_REMATCH[1]}"
    if [[ "$line" == *"</internal>"* && "$pre" != $tag_glob ]] && [[ "$line" =~ $close_re ]]; then
      [[ "$pre" =~ [^[:space:]] ]] && count=$((count + 1))
      end_note
    elif [[ "$line" == $tag_glob ]]; then
      end_note; literal=1; out+=("$line$nl")
    else
      count=$((count + 1))
    fi
    continue
  fi

  if [[ "$line" != *"<"* ]]; then out+=("$line$nl"); continue; fi

  if [[ "$line" == *"<internal>"* ]] && [[ "$line" =~ $open_re ]]; then
    after="${BASH_REMATCH[1]}"
    if [[ "$after" =~ $close_re ]]; then
      inner="${BASH_REMATCH[1]}"
      if [[ "$inner" != $tag_glob ]]; then
        n=0; [[ "$inner" =~ [^[:space:]] ]] && n=1
        changed=1; out+=("▸ internal note · $n lines"$'\n'); continue
      fi
    elif [[ "$after" != $tag_glob ]]; then
      changed=1; open=open; count=0
      [[ "$after" =~ [^[:space:]] ]] && count=1
      opener_idx=${#out[@]}
      out+=("▸ internal note"$'\n'); continue
    fi
    out+=("$line$nl"); continue
  fi

  # Rewrite every <status>BODY</status> pair on the line in place; the line
  # draws untouched when anything on it does not parse.
  rest="$line" acc="" pairs=0
  while [[ "$rest" == *"<status>"* ]]; do
    pre="${rest%%<status>*}"
    [[ -n "$pre" && "$pre" == $tag_glob ]] && { pairs=-1; break; }
    rest="${rest#*<status>}"
    [[ "$rest" == *"</status>"* ]] || { pairs=-1; break; }
    body="${rest%%</status>*}"
    [[ "$body" == $bad_body_glob ]] && { pairs=-1; break; }
    acc+="$pre· ${body:-status}"
    rest="${rest#*</status>}"
    pairs=$((pairs + 1))
  done
  if [[ "$pairs" -le 0 ]] || [[ -n "$rest" && "$rest" == $tag_glob ]]; then
    out+=("$line$nl")
  else
    changed=1; out+=("$acc$rest$nl")
  fi
done
exec 3<&-

# Persist or clear state.
if [ "$final" = true ]; then
  rm -f -- "$state_file" 2>/dev/null
elif [ "$open" = open ] || [ -f "$state_file" ]; then
  printf '%s %s\n' "$open" "$count" >"$state_file" 2>/dev/null || exit 0
fi

[ "$changed" = 1 ] || exit 0
# Content goes through stdin, not an argument: a large batch exceeds the
# kernel's single-argument limit and would silently fall back to the original.
printf '%s' "${out[@]+"${out[@]}"}" | "$jq_bin" -cRs \
  '{hookSpecificOutput:{hookEventName:"MessageDisplay",displayContent:.}}' 2>/dev/null || true
exit 0
