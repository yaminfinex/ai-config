#!/usr/bin/env bash

if [ -n "${AI_CONFIG_CLAUDE_SETTINGS_SH:-}" ]; then
  return 0
fi
AI_CONFIG_CLAUDE_SETTINGS_SH=1

CLAUDE_SETTINGS_SHARED_REL="claude/settings.shared.json"
CLAUDE_SETTINGS_LIVE="${CLAUDE_SETTINGS_LIVE:-$HOME/.claude/settings.json}"

# Objects merge recursively with shared winning on scalars; arrays union so a
# machine-local permissions.deny entry survives the merge.
CLAUDE_SETTINGS_MELD_JQ='
def meld($a; $b):
  if ($a | type) == "object" and ($b | type) == "object" then
    reduce ($b | keys_unsorted[]) as $k ($a;
      .[$k] = (if has($k) then meld(.[$k]; $b[$k]) else $b[$k] end))
  elif ($a | type) == "array" and ($b | type) == "array" then
    ($a + $b) | unique
  else
    $b
  end;
'

claude_settings_shared_file() {
  printf '%s/%s\n' "$AI_CONFIG_ROOT" "$CLAUDE_SETTINGS_SHARED_REL"
}

claude_settings_live_valid() {
  [ ! -f "$CLAUDE_SETTINGS_LIVE" ] || jq -e . "$CLAUDE_SETTINGS_LIVE" >/dev/null 2>&1
}

claude_settings_merged() {
  local live_json='{}'

  [ -f "$CLAUDE_SETTINGS_LIVE" ] && live_json="$(cat "$CLAUDE_SETTINGS_LIVE")"
  jq -n \
    --argjson live "$live_json" \
    --slurpfile shared "$(claude_settings_shared_file)" \
    "$CLAUDE_SETTINGS_MELD_JQ meld(\$live; \$shared[0])"
}

claude_settings_in_sync() {
  local live_sorted='{}'

  [ -f "$CLAUDE_SETTINGS_LIVE" ] && live_sorted="$(jq -S . "$CLAUDE_SETTINGS_LIVE")"
  [ "$(claude_settings_merged | jq -S .)" = "$live_sorted" ]
}

claude_settings_backup_copy() {
  local dest

  dest="$AI_CONFIG_BACKUP_DIR/$AI_CONFIG_TIMESTAMP/${CLAUDE_SETTINGS_LIVE#"$HOME"/}"
  mkdir -p "$(dirname "$dest")"
  cp -p "$CLAUDE_SETTINGS_LIVE" "$dest"
  printf '%s\n' "$dest"
}

# Meld the repo's shared fragment into the live Claude settings file.
# $1 = dry_run flag (0/1). Additive and idempotent: never removes keys that
# only exist live, never rewrites the file when already in sync.
claude_settings_apply() {
  local dry_run="${1:-0}"
  local shared merged tmp backup

  shared="$(claude_settings_shared_file)"
  [ -f "$shared" ] || {
    log_info "skip missing source: $shared"
    return
  }

  if ! command -v jq >/dev/null 2>&1; then
    log_warn "jq not installed; skipping claude shared settings merge"
    return
  fi

  jq -e . "$shared" >/dev/null 2>&1 || {
    log_error "shared settings fragment is not valid JSON: $shared"
    return 1
  }

  claude_settings_live_valid || {
    log_warn "live claude settings are not valid JSON, leaving untouched: $CLAUDE_SETTINGS_LIVE"
    return
  }

  if claude_settings_in_sync; then
    log_info "claude settings already include shared fragment"
    return
  fi

  merged="$(claude_settings_merged)"

  if [ "$dry_run" -eq 1 ]; then
    log_info "would merge $shared into $CLAUDE_SETTINGS_LIVE:"
    diff <([ -f "$CLAUDE_SETTINGS_LIVE" ] && jq -S . "$CLAUDE_SETTINGS_LIVE" || printf '{}\n') \
      <(printf '%s\n' "$merged" | jq -S .) || true
    return
  fi

  mkdir -p "$(dirname "$CLAUDE_SETTINGS_LIVE")"

  if [ -f "$CLAUDE_SETTINGS_LIVE" ]; then
    backup="$(claude_settings_backup_copy)"
    log_info "backed up $CLAUDE_SETTINGS_LIVE -> $backup"
  fi

  tmp="$(mktemp "$(dirname "$CLAUDE_SETTINGS_LIVE")/.settings.json.XXXXXX")"
  printf '%s\n' "$merged" >"$tmp"
  mv "$tmp" "$CLAUDE_SETTINGS_LIVE"
  log_info "merged shared settings into $CLAUDE_SETTINGS_LIVE"
}
