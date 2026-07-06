#!/usr/bin/env bash

if [ -n "${AI_CONFIG_HCOM_HOOKS_SH:-}" ]; then
  return 0
fi
AI_CONFIG_HCOM_HOOKS_SH=1

hcom_hooks_claude_settings() {
  printf '%s\n' "$HOME/.claude/settings.json"
}

hcom_hooks_codex_config() {
  printf '%s\n' "$HOME/.codex/config.toml"
}

hcom_hooks_codex_hooks() {
  printf '%s\n' "$HOME/.codex/hooks.json"
}

hcom_hooks_codex_rules() {
  printf '%s\n' "$HOME/.codex/rules/hcom.rules"
}

hcom_hooks_backup_file() {
  local target="$1"
  local rel
  local dest

  [ -e "$target" ] || return 0

  case "$target" in
    "$HOME"/*) rel="${target#"$HOME"/}" ;;
    /*) rel="${target#/}" ;;
    *) rel="$target" ;;
  esac

  dest="$AI_CONFIG_BACKUP_DIR/$AI_CONFIG_TIMESTAMP/$rel"
  if [ "${dry_run:-0}" -eq 1 ]; then
    log_info "would back up $target -> $dest"
    return 0
  fi

  mkdir -p "$(dirname "$dest")"
  cp -p "$target" "$dest"
  log_info "backed up $target -> $dest"
}

hcom_hooks_backup_targets() {
  hcom_hooks_backup_file "$(hcom_hooks_claude_settings)"
  hcom_hooks_backup_file "$(hcom_hooks_codex_config)"
  hcom_hooks_backup_file "$(hcom_hooks_codex_hooks)"
  hcom_hooks_backup_file "$(hcom_hooks_codex_rules)"
}

hcom_hooks_native_available() {
  command -v hcom >/dev/null 2>&1 || return 1
  hcom hooks --help 2>/dev/null | grep -q 'hcom hooks add \[tool\]' || return 1
  hcom hooks --help 2>/dev/null | grep -q 'hcom hooks remove \[tool\]' || return 1
}

hcom_hooks_run_native() {
  local verb="$1"
  local tool="$2"

  if [ "${dry_run:-0}" -eq 1 ]; then
    printf 'DRY hcom hooks %s %s\n' "$verb" "$tool"
    return 0
  fi

  # W5 manages only global hooks. hcom also knows about HCOM_DIR-local hooks, so
  # clear HCOM_DIR for this durable/manual layer instead of inheriting a worktree bus.
  ( unset HCOM_DIR; hcom hooks "$verb" "$tool" )
}

hcom_hooks_file_has_ref() {
  local file="$1"
  [ -f "$file" ] || return 1
  grep -Eq '(^|[^A-Za-z0-9_])hcom([^A-Za-z0-9_]|$)|HCOM|Bash\(hcom' "$file"
}

hcom_hooks_claude_status() {
  local file
  local hooks=0
  local perms=0
  local env_ref=0

  file="$(hcom_hooks_claude_settings)"
  [ -f "$file" ] || {
    printf '%s\n' "absent"
    return
  }

  grep -q 'exec \$cmd' "$file" && grep -q 'HCOM:-hcom' "$file" && hooks=1
  grep -q 'Bash(hcom' "$file" && perms=1
  grep -q '"HCOM"[[:space:]]*:' "$file" && env_ref=1

  if [ "$hooks" -eq 1 ] && [ "$perms" -eq 1 ] && [ "$env_ref" -eq 1 ]; then
    printf '%s\n' "installed"
  elif [ "$hooks" -eq 1 ] || [ "$perms" -eq 1 ] || [ "$env_ref" -eq 1 ] || hcom_hooks_file_has_ref "$file"; then
    printf '%s\n' "partial"
  else
    printf '%s\n' "absent"
  fi
}

hcom_hooks_codex_status() {
  local config
  local hooks
  local rules
  local config_ref=0
  local hooks_ref=0
  local rules_ref=0

  config="$(hcom_hooks_codex_config)"
  hooks="$(hcom_hooks_codex_hooks)"
  rules="$(hcom_hooks_codex_rules)"

  [ -f "$config" ] && grep -q 'hcom_hook_definition_hash\|hcom_codex_cli_version' "$config" && config_ref=1
  [ -f "$hooks" ] && grep -q 'hcom codex-' "$hooks" && hooks_ref=1
  [ -f "$rules" ] && grep -q 'hcom integration\|pattern=\["hcom"' "$rules" && rules_ref=1

  if [ "$config_ref" -eq 1 ] && [ "$hooks_ref" -eq 1 ] && [ "$rules_ref" -eq 1 ]; then
    printf '%s\n' "installed"
  elif [ "$config_ref" -eq 1 ] || [ "$hooks_ref" -eq 1 ] || [ "$rules_ref" -eq 1 ]; then
    printf '%s\n' "partial"
  else
    printf '%s\n' "absent"
  fi
}

hcom_hooks_overall_status() {
  local claude="$1"
  local codex="$2"

  if [ "$claude" = "installed" ] && [ "$codex" = "installed" ]; then
    printf '%s\n' "installed"
  elif [ "$claude" = "absent" ] && [ "$codex" = "absent" ]; then
    printf '%s\n' "absent"
  else
    printf '%s\n' "partial"
  fi
}

hcom_hooks_status() {
  local claude
  local codex
  local overall

  claude="$(hcom_hooks_claude_status)"
  codex="$(hcom_hooks_codex_status)"
  overall="$(hcom_hooks_overall_status "$claude" "$codex")"

  printf 'hcom hooks: %s\n' "$overall"
  printf 'Claude: %s (%s)\n' "$claude" "$(hcom_hooks_claude_settings)"
  printf 'Codex:  %s (%s)\n' "$codex" "$(hcom_hooks_codex_config)"
}

hcom_hooks_scrub_claude_settings() {
  local file
  local tmp

  file="$(hcom_hooks_claude_settings)"
  [ -f "$file" ] || return 0
  hcom_hooks_file_has_ref "$file" || return 0

  command -v jq >/dev/null 2>&1 || {
    log_error "jq is required to clean residual Claude hcom hook state in $file"
    return 1
  }

  tmp="$(mktemp)"
  jq '
    del(.env.HCOM) |
    if (.env? == {}) then del(.env) else . end |
    if (.permissions.allow? | type == "array") then
      .permissions.allow |= map(select((type == "string" and startswith("Bash(hcom")) | not))
    else . end |
    if (.hooks? | type == "object") then
      .hooks |= with_entries(
        .value |= map(
          select(
            ((.hooks // []) | any((.command? // "") | test("hcom|HCOM"))) | not
          )
        )
      )
    else . end
  ' "$file" > "$tmp"

  if [ "${dry_run:-0}" -eq 1 ]; then
    log_info "would clean residual Claude hcom state in $file"
    rm -f "$tmp"
  else
    mv "$tmp" "$file"
    log_info "cleaned residual Claude hcom state in $file"
  fi
}

hcom_hooks_scrub_codex_config() {
  local file
  local tmp

  file="$(hcom_hooks_codex_config)"
  [ -f "$file" ] || return 0
  grep -q 'hcom_hook_definition_hash\|hcom_codex_cli_version' "$file" || return 0

  tmp="$(mktemp)"
  awk '
    function flush_block() {
      if (block != "" && hcom_block == 0) {
        printf "%s", block
      }
      block = ""
      hcom_block = 0
    }
    /^\[/ {
      flush_block()
      block = $0 ORS
      next
    }
    {
      block = block $0 ORS
      if ($0 ~ /hcom_hook_definition_hash|hcom_codex_cli_version/) {
        hcom_block = 1
      }
    }
    END { flush_block() }
  ' "$file" > "$tmp"

  if [ "${dry_run:-0}" -eq 1 ]; then
    log_info "would clean residual Codex hcom state in $file"
    rm -f "$tmp"
  else
    mv "$tmp" "$file"
    log_info "cleaned residual Codex hcom state in $file"
  fi
}

hcom_hooks_scrub_codex_hooks_json() {
  local file
  local tmp

  file="$(hcom_hooks_codex_hooks)"
  [ -f "$file" ] || return 0
  grep -q 'hcom codex-' "$file" || return 0

  command -v jq >/dev/null 2>&1 || {
    log_error "jq is required to clean residual Codex hcom hooks in $file"
    return 1
  }

  tmp="$(mktemp)"
  jq '
    if (.hooks? | type == "object") then
      .hooks |= with_entries(
        .value |= map(
          select(
            ((.hooks // []) | any((.command? // "") | test("^hcom codex-"))) | not
          )
        )
      ) |
      .hooks |= with_entries(select((.value | length) > 0))
    else . end
  ' "$file" > "$tmp"

  if [ "${dry_run:-0}" -eq 1 ]; then
    log_info "would clean residual Codex hcom hooks in $file"
    rm -f "$tmp"
  elif grep -q '"hooks"[[:space:]]*:[[:space:]]*{}' "$tmp"; then
    rm -f "$file" "$tmp"
    log_info "removed empty Codex hcom hooks file $file"
  else
    mv "$tmp" "$file"
    log_info "cleaned residual Codex hcom hooks in $file"
  fi
}

hcom_hooks_scrub_codex_rules() {
  local file

  file="$(hcom_hooks_codex_rules)"
  [ -f "$file" ] || return 0
  grep -q 'hcom integration\|pattern=\["hcom"' "$file" || return 0

  if [ "${dry_run:-0}" -eq 1 ]; then
    log_info "would remove Codex hcom rules file $file"
  else
    rm -f "$file"
    log_info "removed Codex hcom rules file $file"
  fi
}

hcom_hooks_scrub_residuals() {
  hcom_hooks_scrub_claude_settings
  hcom_hooks_scrub_codex_config
  hcom_hooks_scrub_codex_hooks_json
  hcom_hooks_scrub_codex_rules
}

hcom_hooks_install() {
  hcom_hooks_native_available || {
    log_error "hcom hooks add/remove commands are not available"
    return 1
  }

  hcom_hooks_backup_targets
  hcom_hooks_run_native add claude
  hcom_hooks_run_native add codex
}

hcom_hooks_remove() {
  local claude
  local codex

  hcom_hooks_native_available || {
    log_error "hcom hooks add/remove commands are not available"
    return 1
  }

  claude="$(hcom_hooks_claude_status)"
  codex="$(hcom_hooks_codex_status)"
  if [ "$claude" = "absent" ] && [ "$codex" = "absent" ]; then
    log_info "hcom hooks already absent"
    return 0
  fi

  hcom_hooks_backup_targets
  hcom_hooks_run_native remove claude
  hcom_hooks_run_native remove codex
  hcom_hooks_scrub_residuals
}

hcom_hooks_main() {
  local action="$1"

  case "$action" in
    install) hcom_hooks_install ;;
    remove) hcom_hooks_remove ;;
    status) hcom_hooks_status ;;
    *)
      log_error "unknown hcom hook action: $action"
      return 2
      ;;
  esac
}
