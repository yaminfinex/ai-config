#!/usr/bin/env bash
# Claude Code status line — styled after robbyrussell oh-my-zsh theme

input=$(cat)

project_dir=$(echo "$input" | jq -r '.workspace.project_dir // ""')
model=$(echo "$input" | jq -r '.model.display_name // ""')
effort=$(echo "$input" | jq -r '.effort.level // ""')
session_id=$(echo "$input" | jq -r '.session_id // ""')
session_name=$(echo "$input" | jq -r '.session_name // ""')

branch=""
if git_out=$(GIT_OPTIONAL_LOCKS=0 git -C "$project_dir" symbolic-ref --short HEAD 2>/dev/null); then
  branch="$git_out"
elif git_out=$(GIT_OPTIONAL_LOCKS=0 git -C "$project_dir" rev-parse --short HEAD 2>/dev/null); then
  branch="$git_out"
fi

used=$(echo "$input" | jq -r '.context_window.used_percentage // empty')
ctx_in=$(echo "$input" | jq -r '.context_window.total_input_tokens // empty')
ctx_size=$(echo "$input" | jq -r '.context_window.context_window_size // empty')
cost=$(echo "$input" | jq -r '.cost.total_cost_usd // empty')

CYAN='\033[36m'
RED='\033[31m'
YELLOW='\033[33m'
GREEN='\033[32m'
MAGENTA='\033[35m'
BLUE='\033[34m'
BOLD='\033[1m'
RESET='\033[0m'


line1="${CYAN}${project_dir}${RESET}"

if [ -n "$branch" ]; then
  line1="${line1} ・ ${BOLD}${RED}git:(${branch})${RESET}"
fi

# herder/herdr identity — pure env, no `herdr` call (statusline renders often).
# HERDR_PANE_ID is set in every herdr pane; HERDER_ROLE only on spawned agents.
if [ "${HERDR_ENV:-}" = "1" ] && [ -n "${HERDR_PANE_ID:-}" ]; then
  herder_seg="${BLUE}⬡ ${HERDR_PANE_ID}${RESET}"
  if [ -n "${HERDER_ROLE:-}" ]; then
    herder_seg="${herder_seg} ${BLUE}[${HERDER_ROLE}]${RESET}"
  fi
  line1="${line1} ・ ${herder_seg}"
fi

line2=""
if [ -n "$model" ]; then
  line2="${YELLOW}${model}${RESET}"
  if [ -n "$effort" ]; then
    line2="${line2} ${YELLOW}[${effort}]${RESET}"
  fi
fi

if [ -n "$ctx_in" ] && [ -n "$ctx_size" ]; then
  ctx_in_k=$(( (ctx_in + 500) / 1000 ))
  ctx_size_k=$(( (ctx_size + 500) / 1000 ))
  [ -n "$line2" ] && line2="${line2} ・ "
  line2="${line2}${MAGENTA}${ctx_in_k}k / ${ctx_size_k}k${RESET}"
elif [ -n "$used" ]; then
  [ -n "$line2" ] && line2="${line2} ・ "
  line2="${line2}ctx:$(printf '%.0f' "$used")%"
fi

if [ -n "$cost" ]; then
  [ -n "$line2" ] && line2="${line2} ・ "
  line2="${line2}${GREEN}\$$(printf '%.4f' "$cost")${RESET}"
fi

if [ -n "$session_id" ]; then
  [ -n "$line2" ] && line2="${line2} ・ "
  line2="${line2}${RED}${session_id}${RESET}"
  if [ -n "$session_name" ]; then
    line2="${line2} ・ ${session_name}"
  fi
fi

printf "%b\n%b\n" "$line1" "$line2"
