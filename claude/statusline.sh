#!/usr/bin/env bash
# Claude Code status line — styled after robbyrussell oh-my-zsh theme.
# Line 2 is provided by `bunx ccusage statusline` (session/block/burn/context).

# Make mise-managed shims (bun, bunx) findable even when launched outside an
# interactive shell.
export PATH="$HOME/.local/share/mise/shims:$HOME/.bun/bin:$PATH"

input=$(cat)

project_dir=$(printf '%s' "$input" | jq -r '.workspace.project_dir // ""')
effort=$(printf '%s' "$input" | jq -r '.effort.level // ""')
session_id=$(printf '%s' "$input" | jq -r '.session_id // ""')
session_name=$(printf '%s' "$input" | jq -r '.session_name // ""')

branch=""
if git_out=$(GIT_OPTIONAL_LOCKS=0 git -C "$project_dir" symbolic-ref --short HEAD 2>/dev/null); then
  branch="$git_out"
elif git_out=$(GIT_OPTIONAL_LOCKS=0 git -C "$project_dir" rev-parse --short HEAD 2>/dev/null); then
  branch="$git_out"
fi

CYAN='\033[36m'
RED='\033[31m'
YELLOW='\033[33m'
BOLD='\033[1m'
RESET='\033[0m'

line1="${CYAN}${project_dir}${RESET}"
if [ -n "$branch" ]; then
  line1="${line1} ・ ${BOLD}${RED}git:(${branch})${RESET}"
fi

line2=""
if command -v bunx >/dev/null 2>&1; then
  line2=$(printf '%s' "$input" | bunx ccusage statusline 2>/dev/null)
fi

if [ -z "$line2" ]; then
  # Fallback when bunx / ccusage unavailable: minimal model + cost from input.
  model=$(printf '%s' "$input" | jq -r '.model.display_name // ""')
  cost=$(printf '%s' "$input" | jq -r '.cost.total_cost_usd // empty')
  [ -n "$model" ] && line2="${YELLOW}${model}${RESET}"
  [ -n "$cost" ] && line2="${line2} ・ \$$(printf '%.4f' "$cost")"
fi

if [ -n "$effort" ]; then
  line2="${line2} ・ ${YELLOW}[${effort}]${RESET}"
fi

if [ -n "$session_id" ]; then
  line2="${line2} ・ ${RED}${session_id}${RESET}"
  [ -n "$session_name" ] && line2="${line2} ・ ${session_name}"
fi

printf "%b\n%b\n" "$line1" "$line2"
