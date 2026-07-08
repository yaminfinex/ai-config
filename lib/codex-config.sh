#!/usr/bin/env bash

if [ -n "${AI_CONFIG_CODEX_CONFIG_SH:-}" ]; then
  return 0
fi
AI_CONFIG_CODEX_CONFIG_SH=1

CODEX_CONFIG_SHARED_REL="codex/config.shared.toml"
CODEX_CONFIG_LIVE="${CODEX_CONFIG_LIVE:-$HOME/.codex/config.toml}"

codex_config_shared_file() {
  printf '%s/%s\n' "$AI_CONFIG_ROOT" "$CODEX_CONFIG_SHARED_REL"
}

codex_config_shared_line() {
  local key="$1"
  awk -v key="$key" '
    $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
      sub(/^[[:space:]]*/, "")
      print
      found = 1
      exit
    }
    END { exit found ? 0 : 1 }
  ' "$(codex_config_shared_file)"
}

codex_config_toml_valid() {
  local file="$1"
  [ ! -f "$file" ] || python3 - "$file" <<'PY' >/dev/null 2>&1
import sys
import tomllib

with open(sys.argv[1], "rb") as f:
    tomllib.load(f)
PY
}

codex_config_shared_valid() {
  local shared

  shared="$(codex_config_shared_file)"
  [ -f "$shared" ] || {
    log_info "skip missing source: $shared"
    return 1
  }

  command -v python3 >/dev/null 2>&1 || {
    log_warn "python3 not installed; skipping codex shared config merge"
    return 1
  }

  codex_config_toml_valid "$shared" || {
    log_error "shared codex config fragment is not valid TOML: $shared"
    return 2
  }

  codex_config_shared_line status_line >/dev/null || {
    log_error "shared codex config missing tui.status_line: $shared"
    return 2
  }
  codex_config_shared_line terminal_title >/dev/null || {
    log_error "shared codex config missing tui.terminal_title: $shared"
    return 2
  }
}

codex_config_render_merged() {
  local live="$1"
  local status_line
  local terminal_title

  status_line="$(codex_config_shared_line status_line)"
  terminal_title="$(codex_config_shared_line terminal_title)"

  if [ -f "$live" ]; then
    awk -v status_line="$status_line" -v terminal_title="$terminal_title" '
      function is_header(line) { return line ~ /^\[[^]]+\][[:space:]]*(#.*)?$/ }
      function is_tui(line) { return line ~ /^\[tui\][[:space:]]*(#.*)?$/ }
      function emit_managed() {
        print status_line
        print terminal_title
        inserted = 1
      }
      {
        if (is_header($0)) {
          if (in_tui && !inserted) {
            emit_managed()
          }
          in_tui = 0
          if (is_tui($0)) {
            seen_tui = 1
            in_tui = 1
            inserted = 0
          }
        }

        if (in_tui && $0 ~ /^[[:space:]]*(status_line|terminal_title)[[:space:]]*=/) {
          next
        }

        print
      }
      END {
        if (in_tui && !inserted) {
          emit_managed()
        } else if (!seen_tui) {
          if (NR > 0) {
            print ""
          }
          print "[tui]"
          emit_managed()
        }
      }
    ' "$live"
  else
    printf '[tui]\n%s\n%s\n' "$status_line" "$terminal_title"
  fi
}

codex_config_render_removed() {
  local live="$1"
  local status_line
  local terminal_title

  status_line="$(codex_config_shared_line status_line)"
  terminal_title="$(codex_config_shared_line terminal_title)"

  awk -v status_line="$status_line" -v terminal_title="$terminal_title" '
    function is_header(line) { return line ~ /^\[[^]]+\][[:space:]]*(#.*)?$/ }
    function is_tui(line) { return line ~ /^\[tui\][[:space:]]*(#.*)?$/ }
    {
      if (is_header($0)) {
        in_tui = is_tui($0)
      }
      if (in_tui && ($0 == status_line || $0 == terminal_title)) {
        next
      }
      print
    }
  ' "$live"
}

codex_config_in_sync() {
  local tmp

  tmp="$(mktemp)"
  codex_config_render_merged "$CODEX_CONFIG_LIVE" > "$tmp"
  if [ -f "$CODEX_CONFIG_LIVE" ]; then
    cmp -s "$CODEX_CONFIG_LIVE" "$tmp"
  else
    [ ! -s "$tmp" ]
  fi
  local rc=$?
  rm -f "$tmp"
  return "$rc"
}

codex_config_backup_copy() {
  local dest

  dest="$AI_CONFIG_BACKUP_DIR/$AI_CONFIG_TIMESTAMP/${CODEX_CONFIG_LIVE#"$HOME"/}"
  mkdir -p "$(dirname "$dest")"
  cp -p "$CODEX_CONFIG_LIVE" "$dest"
  printf '%s\n' "$dest"
}

codex_config_apply() {
  local dry_run="${1:-0}"
  local merged tmp backup

  codex_config_shared_valid || return $?

  codex_config_toml_valid "$CODEX_CONFIG_LIVE" || {
    log_warn "live codex config is not valid TOML, leaving untouched: $CODEX_CONFIG_LIVE"
    return 0
  }

  if codex_config_in_sync; then
    log_info "codex config already includes shared fragment"
    return 0
  fi

  merged="$(codex_config_render_merged "$CODEX_CONFIG_LIVE")"

  tmp="$(mktemp)"
  printf '%s\n' "$merged" > "$tmp"
  codex_config_toml_valid "$tmp" || {
    rm -f "$tmp"
    log_error "merged codex config would not be valid TOML: $CODEX_CONFIG_LIVE"
    return 1
  }

  if [ "$dry_run" -eq 1 ]; then
    log_info "would merge $(codex_config_shared_file) into $CODEX_CONFIG_LIVE:"
    diff <([ -f "$CODEX_CONFIG_LIVE" ] && cat "$CODEX_CONFIG_LIVE" || printf '') "$tmp" || true
    rm -f "$tmp"
    return 0
  fi

  mkdir -p "$(dirname "$CODEX_CONFIG_LIVE")"

  if [ -f "$CODEX_CONFIG_LIVE" ]; then
    backup="$(codex_config_backup_copy)"
    log_info "backed up $CODEX_CONFIG_LIVE -> $backup"
  fi

  mv "$tmp" "$CODEX_CONFIG_LIVE"
  log_info "merged shared config into $CODEX_CONFIG_LIVE"
}

codex_config_remove() {
  local dry_run="${1:-0}"
  local removed tmp backup

  codex_config_shared_valid || return $?
  [ -f "$CODEX_CONFIG_LIVE" ] || {
    log_info "codex config already absent: $CODEX_CONFIG_LIVE"
    return 0
  }

  codex_config_toml_valid "$CODEX_CONFIG_LIVE" || {
    log_warn "live codex config is not valid TOML, leaving untouched: $CODEX_CONFIG_LIVE"
    return 0
  }

  removed="$(codex_config_render_removed "$CODEX_CONFIG_LIVE")"
  tmp="$(mktemp)"
  printf '%s\n' "$removed" > "$tmp"
  codex_config_toml_valid "$tmp" || {
    rm -f "$tmp"
    log_error "removed codex config would not be valid TOML: $CODEX_CONFIG_LIVE"
    return 1
  }

  if cmp -s "$CODEX_CONFIG_LIVE" "$tmp"; then
    rm -f "$tmp"
    log_info "codex shared config already absent"
    return 0
  fi

  if [ "$dry_run" -eq 1 ]; then
    log_info "would remove shared codex config from $CODEX_CONFIG_LIVE:"
    diff "$CODEX_CONFIG_LIVE" "$tmp" || true
    rm -f "$tmp"
    return 0
  fi

  backup="$(codex_config_backup_copy)"
  log_info "backed up $CODEX_CONFIG_LIVE -> $backup"
  mv "$tmp" "$CODEX_CONFIG_LIVE"
  log_info "removed shared codex config from $CODEX_CONFIG_LIVE"
}

codex_config_status() {
  local status_line
  local terminal_title
  local status_ref=0
  local title_ref=0
  local state

  codex_config_shared_valid || return $?
  status_line="$(codex_config_shared_line status_line)"
  terminal_title="$(codex_config_shared_line terminal_title)"

  [ -f "$CODEX_CONFIG_LIVE" ] && grep -Fxq "$status_line" "$CODEX_CONFIG_LIVE" && status_ref=1
  [ -f "$CODEX_CONFIG_LIVE" ] && grep -Fxq "$terminal_title" "$CODEX_CONFIG_LIVE" && title_ref=1

  if [ "$status_ref" -eq 1 ] && [ "$title_ref" -eq 1 ]; then
    state="installed"
  elif [ "$status_ref" -eq 1 ] || [ "$title_ref" -eq 1 ]; then
    state="partial"
  else
    state="absent"
  fi

  printf 'Codex config: %s (%s)\n' "$state" "$CODEX_CONFIG_LIVE"
}

codex_config_main() {
  local action="$1"
  local dry_run="${2:-0}"

  case "$action" in
    install) codex_config_apply "$dry_run" ;;
    remove) codex_config_remove "$dry_run" ;;
    status) codex_config_status ;;
    *)
      log_error "unknown codex config action: $action"
      return 2
      ;;
  esac
}
