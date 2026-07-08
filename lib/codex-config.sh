#!/usr/bin/env bash

if [ -n "${AI_CONFIG_CODEX_CONFIG_SH:-}" ]; then
  return 0
fi
AI_CONFIG_CODEX_CONFIG_SH=1

CODEX_CONFIG_SHARED_REL="codex/config.shared.toml"
CODEX_CONFIG_LIVE="${CODEX_CONFIG_LIVE:-$HOME/.codex/config.toml}"
CODEX_CONFIG_STATE="${CODEX_CONFIG_STATE:-$HOME/.codex/ai-config-codex-config.state}"

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

codex_config_resolved_file() {
  local target

  if [ -L "$CODEX_CONFIG_LIVE" ]; then
    if [ ! -e "$CODEX_CONFIG_LIVE" ]; then
      log_error "refusing Codex config merge: $CODEX_CONFIG_LIVE is a dangling symlink. Fix or remove the symlink, or create its target before rerunning."
      return 1
    fi
    target="$(readlink "$CODEX_CONFIG_LIVE")"
    case "$target" in
      /*) printf '%s\n' "$target" ;;
      *) printf '%s/%s\n' "$(cd "$(dirname "$CODEX_CONFIG_LIVE")" && pwd -P)" "$target" ;;
    esac
  else
    printf '%s\n' "$CODEX_CONFIG_LIVE"
  fi
}

codex_config_backup_copy() {
  local target="$1"
  local rel
  local dest

  case "$target" in
    "$HOME"/*) rel="${target#"$HOME"/}" ;;
    /*) rel="${target#/}" ;;
    *) rel="$target" ;;
  esac

  dest="$AI_CONFIG_BACKUP_DIR/$AI_CONFIG_TIMESTAMP/$rel"
  mkdir -p "$(dirname "$dest")"
  cp -p "$target" "$dest"
  printf '%s\n' "$dest"
}

codex_config_safe_to_edit() {
  local file="$1"

  [ -f "$file" ] || return 0

  if grep -q "$(printf '\r')" "$file"; then
    log_warn "skip codex config: $file has CRLF line endings; convert it to LF or run bin/ai-setup --codex-config install after normalizing it."
    return 1
  fi

  awk '
    function is_header(line) { return line ~ /^\[[^]]+\][[:space:]]*(#.*)?$/ }
    function is_tui(line) { return line ~ /^\[tui\][[:space:]]*(#.*)?$/ }
    {
      if (is_header($0)) {
        in_tui = is_tui($0)
      }
      if (in_tui && $0 ~ /^[[:space:]]*(status_line|terminal_title)[[:space:]]*=/) {
        if ($0 ~ /#/) {
          exit 10
        }
        if ($0 !~ /^[[:space:]]*(status_line|terminal_title)[[:space:]]*=[[:space:]]*\[[^]]*\][[:space:]]*$/) {
          exit 11
        }
      }
    }
  ' "$file"
  case "$?" in
    0) return 0 ;;
    10)
      log_warn "skip codex config: $file has inline comments on managed [tui] keys; remove those comments or manage the Codex footer manually."
      return 1
      ;;
    11)
      log_warn "skip codex config: $file has multi-line or unsupported managed [tui] values; make status_line/terminal_title single-line arrays or manage them manually."
      return 1
      ;;
    *)
      log_warn "skip codex config: could not inspect $file safely; leaving it unmanaged."
      return 1
      ;;
  esac
}

codex_config_current_line() {
  local file="$1"
  local key="$2"

  [ -f "$file" ] || return 1
  awk -v key="$key" '
    function is_header(line) { return line ~ /^\[[^]]+\][[:space:]]*(#.*)?$/ }
    function is_tui(line) { return line ~ /^\[tui\][[:space:]]*(#.*)?$/ }
    {
      if (is_header($0)) {
        in_tui = is_tui($0)
      }
      if (in_tui && $0 ~ "^[[:space:]]*" key "[[:space:]]*=") {
        sub(/^[[:space:]]*/, "")
        print
        found = 1
        exit
      }
    }
    END { exit found ? 0 : 1 }
  ' "$file"
}

codex_config_write_state() {
  local file="$1"
  local tmp
  local key
  local line

  mkdir -p "$(dirname "$CODEX_CONFIG_STATE")"
  tmp="$(mktemp "$(dirname "$CODEX_CONFIG_STATE")/.ai-config-codex-config.state.XXXXXX")"
  {
    printf '%s\n' "version=1"
    for key in status_line terminal_title; do
      if line="$(codex_config_current_line "$file" "$key")"; then
        printf '%s\tpresent\t%s\n' "$key" "$(printf '%s' "$line" | base64 | tr -d '\n')"
      else
        printf '%s\tabsent\t\n' "$key"
      fi
    done
  } > "$tmp"
  mv "$tmp" "$CODEX_CONFIG_STATE"
}

codex_config_state_mode() {
  local key="$1"
  awk -F '\t' -v key="$key" '$1 == key { print $2; found = 1; exit } END { exit found ? 0 : 1 }' "$CODEX_CONFIG_STATE"
}

codex_config_state_line() {
  local key="$1"
  awk -F '\t' -v key="$key" '$1 == key { print $3; found = 1; exit } END { exit found ? 0 : 1 }' "$CODEX_CONFIG_STATE" | base64 --decode
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

codex_config_render_restored() {
  local live="$1"
  local status_mode="absent"
  local title_mode="absent"
  local status_restore=""
  local title_restore=""

  status_mode="$(codex_config_state_mode status_line 2>/dev/null || printf absent)"
  title_mode="$(codex_config_state_mode terminal_title 2>/dev/null || printf absent)"
  if [ "$status_mode" = "present" ]; then
    status_restore="$(codex_config_state_line status_line)"
  fi
  if [ "$title_mode" = "present" ]; then
    title_restore="$(codex_config_state_line terminal_title)"
  fi

  awk \
    -v status_mode="$status_mode" \
    -v title_mode="$title_mode" \
    -v status_restore="$status_restore" \
    -v title_restore="$title_restore" '
      function is_header(line) { return line ~ /^\[[^]]+\][[:space:]]*(#.*)?$/ }
      function is_tui(line) { return line ~ /^\[tui\][[:space:]]*(#.*)?$/ }
      function emit_restored() {
        if (status_mode == "present") {
          print status_restore
        }
        if (title_mode == "present") {
          print title_restore
        }
        inserted = 1
      }
      {
        if (is_header($0)) {
          if (in_tui && !inserted) {
            emit_restored()
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
          emit_restored()
        } else if (!seen_tui && (status_mode == "present" || title_mode == "present")) {
          if (NR > 0) {
            print ""
          }
          print "[tui]"
          emit_restored()
        }
      }
    ' "$live"
}

codex_config_in_sync() {
  local live="$1"
  local tmp

  tmp="$(mktemp)"
  codex_config_render_merged "$live" > "$tmp"
  if [ -f "$live" ]; then
    cmp -s "$live" "$tmp"
  else
    [ ! -s "$tmp" ]
  fi
  local rc=$?
  rm -f "$tmp"
  return "$rc"
}

codex_config_apply() {
  local dry_run="${1:-0}"
  local live
  local merged tmp backup

  codex_config_shared_valid || return $?
  live="$(codex_config_resolved_file)" || return 1

  codex_config_toml_valid "$live" || {
    log_warn "live codex config is not valid TOML, leaving untouched: $CODEX_CONFIG_LIVE"
    return 0
  }
  codex_config_safe_to_edit "$live" || return 0

  if codex_config_in_sync "$live"; then
    log_info "codex config already includes shared fragment"
    return 0
  fi

  merged="$(codex_config_render_merged "$live")"

  mkdir -p "$(dirname "$live")"
  tmp="$(mktemp "$(dirname "$live")/.config.toml.XXXXXX")"
  printf '%s\n' "$merged" > "$tmp"
  codex_config_toml_valid "$tmp" || {
    rm -f "$tmp"
    log_error "merged codex config would not be valid TOML: $CODEX_CONFIG_LIVE"
    return 1
  }

  if [ "$dry_run" -eq 1 ]; then
    log_info "would merge $(codex_config_shared_file) into $CODEX_CONFIG_LIVE:"
    diff <([ -f "$live" ] && cat "$live" || printf '') "$tmp" || true
    rm -f "$tmp"
    return 0
  fi

  if [ -f "$live" ]; then
    if [ ! -f "$CODEX_CONFIG_STATE" ]; then
      codex_config_write_state "$live"
      log_info "recorded previous Codex [tui] values in $CODEX_CONFIG_STATE"
    fi
    backup="$(codex_config_backup_copy "$live")"
    log_info "backed up $live -> $backup"
  fi

  mv "$tmp" "$live"
  log_info "merged shared config into $CODEX_CONFIG_LIVE"
}

codex_config_apply_default() {
  local dry_run="${1:-0}"
  local codex_home_existed="${2:-0}"
  local skip="${3:-0}"

  if [ "$skip" -eq 1 ]; then
    log_info "skip codex config: --skip-codex-config requested; run bin/ai-setup --codex-config install to manage it explicitly."
    return 0
  fi

  if [ "$codex_home_existed" -ne 1 ]; then
    log_info "skip codex config: $HOME/.codex did not exist before setup, so ai-setup will not fabricate a Codex install. Run bin/ai-setup --codex-config install to opt in."
    return 0
  fi

  codex_config_apply "$dry_run"
}

codex_config_remove() {
  local dry_run="${1:-0}"
  local live
  local removed tmp backup

  codex_config_shared_valid || return $?
  live="$(codex_config_resolved_file)" || return 1
  [ -f "$live" ] || {
    log_info "codex config already absent: $CODEX_CONFIG_LIVE"
    return 0
  }

  codex_config_toml_valid "$live" || {
    log_warn "live codex config is not valid TOML, leaving untouched: $CODEX_CONFIG_LIVE"
    return 0
  }
  codex_config_safe_to_edit "$live" || return 0

  if [ -f "$CODEX_CONFIG_STATE" ]; then
    removed="$(codex_config_render_restored "$live")"
  else
    removed="$(codex_config_render_removed "$live")"
  fi

  tmp="$(mktemp "$(dirname "$live")/.config.toml.XXXXXX")"
  printf '%s\n' "$removed" > "$tmp"
  codex_config_toml_valid "$tmp" || {
    rm -f "$tmp"
    log_error "removed codex config would not be valid TOML: $CODEX_CONFIG_LIVE"
    return 1
  }

  if cmp -s "$live" "$tmp"; then
    rm -f "$tmp"
    log_info "codex shared config already absent"
    return 0
  fi

  if [ "$dry_run" -eq 1 ]; then
    log_info "would remove shared codex config from $CODEX_CONFIG_LIVE:"
    diff "$live" "$tmp" || true
    rm -f "$tmp"
    return 0
  fi

  backup="$(codex_config_backup_copy "$live")"
  log_info "backed up $live -> $backup"
  mv "$tmp" "$live"
  if [ -f "$CODEX_CONFIG_STATE" ]; then
    rm -f "$CODEX_CONFIG_STATE"
    log_info "restored previous Codex [tui] values and removed $CODEX_CONFIG_STATE"
  else
    log_info "removed shared codex config from $CODEX_CONFIG_LIVE"
  fi
}

codex_config_status() {
  local live
  local status_line
  local terminal_title
  local status_ref=0
  local title_ref=0
  local state

  codex_config_shared_valid || return $?
  live="$(codex_config_resolved_file)" || return 1
  status_line="$(codex_config_shared_line status_line)"
  terminal_title="$(codex_config_shared_line terminal_title)"

  [ -f "$live" ] && grep -Fxq "$status_line" "$live" && status_ref=1
  [ -f "$live" ] && grep -Fxq "$terminal_title" "$live" && title_ref=1

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
