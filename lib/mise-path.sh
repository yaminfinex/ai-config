#!/usr/bin/env bash

if [ -n "${AI_CONFIG_MISE_PATH_SH:-}" ]; then
  return 0
fi
AI_CONFIG_MISE_PATH_SH=1

mise_bin_dir() {
  printf '%s\n' "$(abs_path "$AI_CONFIG_ROOT/bin")"
}

mise_shims_dir() {
  printf '%s\n' "$(abs_path "$AI_CONFIG_ROOT/tools/herder/shims")"
}

mise_config_home() {
  printf '%s\n' "${XDG_CONFIG_HOME:-$HOME/.config}"
}

mise_config_dir() {
  printf '%s\n' "$(mise_config_home)/mise"
}

mise_path_config_file() {
  printf '%s\n' "$(mise_config_dir)/conf.d/ai-config.toml"
}

mise_path_marker() {
  printf '%s\n' "# Managed by ai-config. Remove with: bin/ai-setup --shims remove"
}

# hcom is a hard dependency of the herder bus substrate (plan 002 R4/R7): the
# github backend pulls the prebuilt release binary (attestation-verified) with
# no brew/compile. Pinned for reproducibility — bump deliberately.
mise_hcom_tool() {
  printf '%s\n' "github:aannoo/hcom"
}

mise_hcom_version() {
  printf '%s\n' "0.7.23"
}

mise_available() {
  command -v mise >/dev/null 2>&1 || [ -d "$(mise_config_dir)" ]
}

mise_require() {
  if mise_available; then
    return 0
  fi
  log_error "mise is required for ai-config PATH setup"
  log_error "install mise, then rerun bin/ai-setup"
  return 1
}

mise_render_config() {
  local bin_dir="$1"
  local shims_dir="$2"
  mise_path_marker
  printf '%s\n' "[env]"
  printf '_.path = ["%s", "%s"]\n' "$bin_dir" "$shims_dir"
  # Hand-typed launches skip permission prompts by default (the shims prepend
  # these before user args). Delete the lines locally for ask-mode machines.
  printf 'HERDER_SHIM_ARGS_CLAUDE = "--dangerously-skip-permissions"\n'
  printf 'HERDER_SHIM_ARGS_CODEX = "--dangerously-bypass-approvals-and-sandbox"\n'
  printf '%s\n' "[tools]"
  printf '"%s" = "%s"\n' "$(mise_hcom_tool)" "$(mise_hcom_version)"
}

mise_file_is_ours() {
  local file="$1"
  [ -f "$file" ] || return 1
  IFS= read -r first < "$file" || return 1
  [ "$first" = "$(mise_path_marker)" ]
}

mise_configured_paths() {
  local file="$1"
  [ -f "$file" ] || return 1
  sed -n 's/^[[:space:]]*_[.]path[[:space:]]*=[[:space:]]*\[\(.*\)\][[:space:]]*$/\1/p' "$file" |
    head -n1 |
    tr ',' '\n' |
    sed 's/^[[:space:]]*"//; s/"[[:space:]]*$//'
}

mise_path_count() {
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

mise_type_lines() {
  local tool="$1"
  type -a "$tool" 2>/dev/null || true
}

mise_tool_resolution_message() {
  local tool="$1"
  local expected="$2"
  local first
  local lines

  lines="$(mise_type_lines "$tool")"
  [ -n "$lines" ] || {
    printf '%s\n' "$tool: not found on PATH"
    return 0
  }

  first="$(printf '%s\n' "$lines" | sed -n '1p')"
  case "$first" in
    "$tool is $expected"|"$tool is hashed ($expected)")
      printf '%s\n' "$tool: expected first"
      ;;
    "$tool is aliased "*|"$tool is a function"*)
      printf '%s\n' "$tool: shadowed before expected ($first)"
      ;;
    "$tool is "*)
      local first_path="${first#"$tool is "}"
      if [ "$first_path" = "$expected" ]; then
        printf '%s\n' "$tool: expected first"
      elif printf '%s\n' "$lines" | grep -Fqx "$tool is $expected"; then
        printf '%s\n' "$tool: shadowed before expected ($first_path)"
      else
        printf '%s\n' "$tool: expected path not found in type -a output"
      fi
      ;;
    *)
      printf '%s\n' "$tool: shadowing unclear ($first)"
      ;;
  esac
}

mise_path_install() {
  local file
  local bin_dir
  local shims_dir
  local tmp

  mise_require || return 1

  file="$(mise_path_config_file)"
  bin_dir="$(mise_bin_dir)"
  shims_dir="$(mise_shims_dir)"
  [ -d "$bin_dir" ] || {
    log_error "bin dir missing: $bin_dir"
    return 1
  }
  [ -d "$shims_dir" ] || {
    log_error "herder shim dir missing: $shims_dir"
    return 1
  }

  if [ -e "$file" ] && ! mise_file_is_ours "$file"; then
    log_error "refusing to overwrite unmanaged mise config: $file"
    log_error "remove it manually or move it aside, then rerun bin/ai-setup"
    return 1
  fi

  if [ "${dry_run:-0}" -eq 1 ]; then
    log_info "would write $file:"
    mise_render_config "$bin_dir" "$shims_dir"
    return 0
  fi

  mkdir -p "$(dirname "$file")"
  tmp="$(mktemp)"
  mise_render_config "$bin_dir" "$shims_dir" > "$tmp"
  mv "$tmp" "$file"
  log_info "installed ai-config mise PATH config: $file"

  # Pull the now-declared managed tools (hcom) so a fresh setup lands them
  # without a separate step. Idempotent; already-present versions are skipped.
  if command -v mise >/dev/null 2>&1; then
    local hcom_spec="$(mise_hcom_tool)@$(mise_hcom_version)"
    if mise install "$hcom_spec" >/dev/null 2>&1; then
      log_info "installed managed mise tool: $hcom_spec"
    else
      log_warn "could not install $hcom_spec via mise; run: mise install \"$hcom_spec\""
    fi
  fi
}

mise_path_remove() {
  local file

  file="$(mise_path_config_file)"
  if [ ! -e "$file" ]; then
    log_info "ai-config mise PATH config already absent"
    return 0
  fi
  if ! mise_file_is_ours "$file"; then
    log_error "refusing to remove unmanaged mise config: $file"
    return 1
  fi
  if [ "${dry_run:-0}" -eq 1 ]; then
    log_info "would remove $file"
    return 0
  fi
  rm -f "$file"
  log_info "removed ai-config mise PATH config: $file"
}

mise_path_status() {
  local file
  local bin_dir
  local shims_dir
  local configured
  local owner="absent"
  local match_bin="n/a"
  local match_shims="n/a"

  file="$(mise_path_config_file)"
  bin_dir="$(mise_bin_dir)"
  shims_dir="$(mise_shims_dir)"

  if [ -e "$file" ]; then
    if mise_file_is_ours "$file"; then
      owner="installed"
      configured="$(mise_configured_paths "$file" || true)"
      if printf '%s\n' "$configured" | grep -Fqx "$bin_dir"; then
        match_bin="yes"
      else
        match_bin="no"
      fi
      if printf '%s\n' "$configured" | grep -Fqx "$shims_dir"; then
        match_shims="yes"
      else
        match_shims="no"
      fi
    else
      owner="foreign"
      configured="$(mise_configured_paths "$file" || true)"
      match_bin="unknown"
      match_shims="unknown"
    fi
  fi

  printf 'ai-config mise PATH: %s\n' "$owner"
  printf 'mise present: %s\n' "$(mise_available && printf yes || printf no)"
  printf 'config: %s\n' "$file"
  printf 'expected bin dir: %s\n' "$bin_dir"
  printf 'expected shim dir: %s\n' "$shims_dir"
  printf 'configured paths:\n'
  if [ -n "${configured:-}" ]; then
    printf '%s\n' "$configured" | sed 's/^/  /'
  else
    printf '  n/a\n'
  fi
  printf 'bin path configured: %s\n' "$match_bin"
  printf 'shim path configured: %s\n' "$match_shims"
  printf 'PATH entries for bin dir: %s\n' "$(mise_path_count "$bin_dir")"
  printf 'PATH entries for shim dir: %s\n' "$(mise_path_count "$shims_dir")"
  mise_tool_resolution_message herder "$bin_dir/herder"
  mise_tool_resolution_message claude "$shims_dir/claude"
  mise_tool_resolution_message codex "$shims_dir/codex"
}

mise_path_main() {
  local action="$1"

  case "$action" in
    install) mise_path_install ;;
    remove) mise_path_remove ;;
    status) mise_path_status ;;
    *)
      log_error "unknown shims action: $action"
      return 2
      ;;
  esac
}
