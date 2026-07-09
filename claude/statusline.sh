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
ctx_pct=$(echo "$input" | jq -r '
  if .context_window.used_percentage != null then
    (.context_window.used_percentage | tonumber | round | tostring)
  elif (.context_window.total_input_tokens != null and .context_window.context_window_size != null and (.context_window.context_window_size | tonumber) > 0) then
    (((.context_window.total_input_tokens | tonumber) * 100 / (.context_window.context_window_size | tonumber)) | round | tostring)
  else
    empty
  end
')
cost=$(echo "$input" | jq -r '.cost.total_cost_usd // empty')

CYAN='\033[36m'
RED='\033[31m'
YELLOW='\033[33m'
GREEN='\033[32m'
MAGENTA='\033[35m'
BLUE='\033[34m'
BOLD='\033[1m'
RESET='\033[0m'

small_uint() {
  case "$1" in
    ''|*[!0-9]*) return 1 ;;
    ???????????*) return 1 ;;
    *) return 0 ;;
  esac
}

small_number() {
  case "$1" in
    ''|*[!0-9.]*|*.*.*) return 1 ;;
    *) return 0 ;;
  esac
}

write_context_snapshot() {
  [ -n "${hcom_state_file:-}" ] || return 0
  [ -n "${ctx_pct:-}" ] || return 0
  [ -n "${ctx_in:-}" ] || return 0
  [ -n "${ctx_size:-}" ] || return 0
  small_number "$ctx_pct" || return 0
  small_uint "$ctx_in" || return 0
  small_uint "$ctx_size" || return 0

  ctx_ts="${EPOCHSECONDS:-}"
  small_uint "$ctx_ts" || return 0

  hcom_unread=""
  hcom_last_ts=""
  hcom_last_age_s=""
  if [ -r "$hcom_state_file" ]; then
    while IFS='=' read -r key value; do
      case "$key" in
        HCOM_UNREAD) hcom_unread="$value" ;;
        HCOM_LAST_TS) hcom_last_ts="$value" ;;
        HCOM_LAST_AGE_S) hcom_last_age_s="$value" ;;
      esac
    done < "$hcom_state_file"
  fi

  state_dir="$(dirname -- "$hcom_state_file")" || return 0
  mkdir -p -- "$state_dir" 2>/dev/null || return 0
  tmp="$(mktemp "${state_dir}/.$(basename -- "$hcom_state_file").XXXXXX.tmp")" || return 0
  {
    if small_uint "$hcom_unread"; then printf 'HCOM_UNREAD=%s\n' "$hcom_unread"; fi
    if small_uint "$hcom_last_ts"; then printf 'HCOM_LAST_TS=%s\n' "$hcom_last_ts"; fi
    if small_uint "$hcom_last_age_s"; then printf 'HCOM_LAST_AGE_S=%s\n' "$hcom_last_age_s"; fi
    printf 'CTX_PCT=%s\n' "$ctx_pct"
    printf 'CTX_TOKENS=%s\n' "$ctx_in"
    printf 'CTX_SIZE=%s\n' "$ctx_size"
    printf 'CTX_TS=%s\n' "$ctx_ts"
  } > "$tmp" || { rm -f -- "$tmp"; return 0; }
  mv -f -- "$tmp" "$hcom_state_file" 2>/dev/null || { rm -f -- "$tmp"; return 0; }
}

line1="${CYAN}${project_dir}${RESET}"

if [ -n "$branch" ]; then
  line1="${line1} ・ ${BOLD}${RED}git:(${branch})${RESET}"
fi

# herder/herdr identity — pure env, no `herdr` call (statusline renders often).
# HERDR_PANE_ID is set in every herdr pane; HERDER_* only on spawned agents.
if [ "${HERDR_ENV:-}" = "1" ] && [ -n "${HERDR_PANE_ID:-}" ]; then
  herder_seg="${BLUE}⬡ ${HERDR_PANE_ID}${RESET}"
  if [ -n "${HERDER_LABEL:-}" ]; then
    herder_seg="${herder_seg} ${BLUE}${HERDER_LABEL}${RESET}"
  fi
  if [ -n "${HERDER_ROLE:-}" ]; then
    herder_seg="${herder_seg} ${BLUE}[${HERDER_ROLE}]${RESET}"
  fi
  hcom_name="${HCOM_INSTANCE_NAME:-${HCOM_NAME:-}}"
  if [ -n "$hcom_name" ]; then
    herder_seg="${herder_seg} ${BLUE}@${hcom_name}${RESET}"
  fi
  line1="${line1} ・ ${herder_seg}"
fi

# Optional hcom bus snapshot. The event-driven herder sidecar updates this
# file; this renderer only performs cheap file reads and omits the segment when
# no snapshot exists.
hcom_state_file="${HCOM_STATUSLINE_STATE:-}"
if [ -z "$hcom_state_file" ] && [ -n "${HCOM_DIR:-}" ]; then
  hcom_state_file="${HCOM_DIR%/}/statusline/${HCOM_INSTANCE_NAME:-${HCOM_NAME:-self}}.env"
fi
write_context_snapshot
if [ -n "$hcom_state_file" ] && [ -r "$hcom_state_file" ]; then
  hcom_unread=""
  hcom_last_ts=""
  hcom_last_age_s=""
  while IFS='=' read -r key value; do
    case "$key" in
      HCOM_UNREAD) hcom_unread="$value" ;;
      HCOM_LAST_TS) hcom_last_ts="$value" ;;
      HCOM_LAST_AGE_S) hcom_last_age_s="$value" ;;
    esac
  done < "$hcom_state_file"
  if small_uint "$hcom_last_ts"; then
    hcom_now="${EPOCHSECONDS:-}"
    if small_uint "$hcom_now"; then
      if [ "$hcom_last_ts" -le "$hcom_now" ]; then
        hcom_last_age_s="$(( hcom_now - hcom_last_ts ))"
      else
        hcom_last_age_s="0"
      fi
    fi
  fi
  hcom_bus_seg=""
  if small_uint "$hcom_unread" && [ "$hcom_unread" != "0" ]; then
    hcom_bus_seg="${MAGENTA}✉ ${hcom_unread}${RESET}"
  fi
  if small_uint "$hcom_last_age_s"; then
    if [ "$hcom_last_age_s" -lt 60 ]; then
      hcom_age="${hcom_last_age_s}s"
    elif [ "$hcom_last_age_s" -lt 3600 ]; then
      hcom_age="$(( (hcom_last_age_s + 30) / 60 ))m"
    else
      hcom_age="$(( (hcom_last_age_s + 1800) / 3600 ))h"
    fi
    [ -n "$hcom_bus_seg" ] && hcom_bus_seg="${hcom_bus_seg} "
    hcom_bus_seg="${hcom_bus_seg}${MAGENTA}last ${hcom_age}${RESET}"
  fi
  if [ -n "$hcom_bus_seg" ]; then
    line1="${line1} ・ ${hcom_bus_seg}"
  fi
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
