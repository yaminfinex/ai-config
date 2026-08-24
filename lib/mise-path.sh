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
#
# THIS IS THE SINGLE SOURCE OF TRUTH for the managed hcom version. Bump it here
# and nowhere else: ai-setup renders it into conf.d, and the test battery
# (check-mise-path-install, check-grok-doctor, check-grok-transport) sources this
# file and derives the version from mise_hcom_version rather than hardcoding it.
# See docs/hcom-upgrade.md for the full bump procedure.
mise_hcom_tool() {
  printf '%s\n' "github:aannoo/hcom"
}

mise_hcom_version() {
  printf '%s\n' "0.7.25"
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
  # bin/ only. The herder shims dir is deliberately NOT on the global PATH any
  # more: hand-typed claude/codex/grok are shell functions (lib/launchers.sh,
  # installed by the ai-setup rc block), which win name resolution regardless
  # of PATH order — global shim interception lost the PATH-ordering race to
  # mise hook-env on every config-boundary cd. The retired shims never ride
  # machine-wide PATH; the launcher functions resolve vendors themselves.
  mise_path_marker
  printf '%s\n' "[env]"
  printf '_.path = ["%s"]\n' "$bin_dir"
  # Optional per-machine override of the launcher functions' baked default
  # args (lib/launchers.sh). Set empty values for an ask-mode machine.
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
  local tmp

  mise_require || return 1

  file="$(mise_path_config_file)"
  bin_dir="$(mise_bin_dir)"
  [ -d "$bin_dir" ] || {
    log_error "bin dir missing: $bin_dir"
    return 1
  }

  if [ -e "$file" ] && ! mise_file_is_ours "$file"; then
    log_error "refusing to overwrite unmanaged mise config: $file"
    log_error "remove it manually or move it aside, then rerun bin/ai-setup"
    return 1
  fi

  if [ "${dry_run:-0}" -eq 1 ]; then
    log_info "would write $file:"
    mise_render_config "$bin_dir"
    return 0
  fi

  mkdir -p "$(dirname "$file")"
  tmp="$(mktemp)"
  mise_render_config "$bin_dir" > "$tmp"
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

mise_launcher_status_message() {
  # claude/codex/grok are launcher shell functions in interactive shells
  # (lib/launchers.sh via the ai-setup rc block); PATH position is irrelevant
  # to them. Outside an rc-loaded shell the function is legitimately absent,
  # so this is informational, not a verdict — the doctor's login-shell check
  # is the real gate.
  local tool="$1"
  if [ "$(type -t "$tool" 2>/dev/null || true)" = "function" ]; then
    printf '%s: launcher function active\n' "$tool"
  else
    printf '%s: launcher function not loaded in this shell (interactive shells get it from the ai-setup rc block)\n' "$tool"
  fi
}

mise_path_status() {
  local file
  local bin_dir
  local configured
  local owner="absent"
  local match_bin="n/a"

  file="$(mise_path_config_file)"
  bin_dir="$(mise_bin_dir)"

  if [ -e "$file" ]; then
    if mise_file_is_ours "$file"; then
      owner="installed"
      configured="$(mise_configured_paths "$file" || true)"
      if printf '%s\n' "$configured" | grep -Fqx "$bin_dir"; then
        match_bin="yes"
      else
        match_bin="no"
      fi
    else
      owner="foreign"
      configured="$(mise_configured_paths "$file" || true)"
      match_bin="unknown"
    fi
  fi

  printf 'ai-config mise PATH: %s\n' "$owner"
  printf 'mise present: %s\n' "$(mise_available && printf yes || printf no)"
  printf 'config: %s\n' "$file"
  printf 'expected bin dir: %s\n' "$bin_dir"
  printf 'configured paths:\n'
  if [ -n "${configured:-}" ]; then
    printf '%s\n' "$configured" | sed 's/^/  /'
  else
    printf '  n/a\n'
  fi
  printf 'bin path configured: %s\n' "$match_bin"
  printf 'PATH entries for bin dir: %s\n' "$(mise_path_count "$bin_dir")"
  mise_tool_resolution_message herder "$bin_dir/herder"
  mise_launcher_status_message claude
  mise_launcher_status_message codex
  mise_launcher_status_message grok
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
