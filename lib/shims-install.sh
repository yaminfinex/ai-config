#!/usr/bin/env bash

if [ -n "${AI_CONFIG_SHIMS_INSTALL_SH:-}" ]; then
  return 0
fi
AI_CONFIG_SHIMS_INSTALL_SH=1

shims_dir() {
  printf '%s\n' "$(abs_path "$AI_CONFIG_ROOT/tools/herder/shims")"
}

shims_config_home() {
  printf '%s\n' "${XDG_CONFIG_HOME:-$HOME/.config}"
}

shims_config_file() {
  printf '%s\n' "$(shims_config_home)/mise/conf.d/ai-config-shims.toml"
}

shims_marker() {
  printf '%s\n' "# Managed by ai-config --shims. Remove with: bin/ai-setup --shims remove"
}

shims_render_config() {
  local dir="$1"
  shims_marker
  printf '%s\n' "[env]"
  printf '_.path = ["%s"]\n' "$dir"
}

shims_file_is_ours() {
  local file="$1"
  [ -f "$file" ] || return 1
  IFS= read -r first < "$file" || return 1
  [ "$first" = "$(shims_marker)" ]
}

shims_configured_path() {
  local file="$1"
  [ -f "$file" ] || return 1
  sed -n 's/^[[:space:]]*_[.]path[[:space:]]*=[[:space:]]*\["\(.*\)"\][[:space:]]*$/\1/p' "$file" | head -n1
}

shims_path_count() {
  local dir="$1"
  local count=0
  local entry
  local old_ifs="$IFS"
  IFS=:
  for entry in ${PATH:-}; do
    [ "$entry" = "$dir" ] && count=$((count + 1))
  done
  IFS="$old_ifs"
  printf '%s\n' "$count"
}

shims_type_lines() {
  local tool="$1"
  type -a "$tool" 2>/dev/null || true
}

shims_first_resolution() {
  local tool="$1"
  shims_type_lines "$tool" | sed -n '1p'
}

shims_tool_shadow_message() {
  local tool="$1"
  local dir="$2"
  local shim="$dir/$tool"
  local first
  local lines

  lines="$(shims_type_lines "$tool")"
  [ -n "$lines" ] || {
    printf '%s\n' "$tool: not found on PATH"
    return 0
  }

  first="$(printf '%s\n' "$lines" | sed -n '1p')"
  case "$first" in
    "$tool is $shim"|"$tool is hashed ($shim)")
      printf '%s\n' "$tool: shim first"
      ;;
    "$tool is aliased "*|"$tool is a function"*)
      printf '%s\n' "$tool: shadowed before shim ($first)"
      ;;
    "$tool is "*)
      local first_path="${first#"$tool is "}"
      if [ "$first_path" = "$shim" ]; then
        printf '%s\n' "$tool: shim first"
      elif printf '%s\n' "$lines" | grep -Fqx "$tool is $shim"; then
        printf '%s\n' "$tool: shadowed before shim ($first_path)"
      else
        printf '%s\n' "$tool: shim not found in type -a output"
      fi
      ;;
    *)
      printf '%s\n' "$tool: shadowing unclear ($first)"
      ;;
  esac
}

shims_install() {
  local file
  local dir
  local tmp

  file="$(shims_config_file)"
  dir="$(shims_dir)"
  [ -d "$dir" ] || {
    log_error "shim dir missing: $dir"
    return 1
  }

  if [ -e "$file" ] && ! shims_file_is_ours "$file"; then
    log_error "refusing to overwrite unmanaged mise config: $file"
    log_error "remove it manually or move it aside, then rerun ai-setup --shims install"
    return 1
  fi

  if [ "${dry_run:-0}" -eq 1 ]; then
    log_info "would write $file:"
    shims_render_config "$dir"
    return 0
  fi

  mkdir -p "$(dirname "$file")"
  tmp="$(mktemp)"
  shims_render_config "$dir" > "$tmp"
  mv "$tmp" "$file"
  log_info "installed herder shims mise config: $file"
}

shims_remove() {
  local file

  file="$(shims_config_file)"
  if [ ! -e "$file" ]; then
    log_info "herder shims already absent"
    return 0
  fi
  if ! shims_file_is_ours "$file"; then
    log_error "refusing to remove unmanaged mise config: $file"
    return 1
  fi
  if [ "${dry_run:-0}" -eq 1 ]; then
    log_info "would remove $file"
    return 0
  fi
  rm -f "$file"
  log_info "removed herder shims mise config: $file"
}

shims_status() {
  local file
  local dir
  local configured=""
  local count
  local owner="absent"
  local match="n/a"

  file="$(shims_config_file)"
  dir="$(shims_dir)"
  count="$(shims_path_count "$dir")"

  if [ -e "$file" ]; then
    if shims_file_is_ours "$file"; then
      owner="installed"
      configured="$(shims_configured_path "$file")"
      if [ "$configured" = "$dir" ]; then
        match="yes"
      else
        match="no"
      fi
    else
      owner="foreign"
      configured="$(shims_configured_path "$file" || true)"
      match="unknown"
    fi
  fi

  printf 'herder shims: %s\n' "$owner"
  printf 'config: %s\n' "$file"
  printf 'expected shim dir: %s\n' "$dir"
  printf 'configured shim dir: %s\n' "${configured:-n/a}"
  printf 'matches this checkout: %s\n' "$match"
  printf 'PATH entries for shim dir: %s\n' "$count"
  shims_tool_shadow_message claude "$dir"
  shims_tool_shadow_message codex "$dir"
}

shims_main() {
  local action="$1"

  case "$action" in
    install) shims_install ;;
    remove) shims_remove ;;
    status) shims_status ;;
    *)
      log_error "unknown shims action: $action"
      return 2
      ;;
  esac
}
