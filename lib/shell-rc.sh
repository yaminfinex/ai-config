#!/usr/bin/env bash
# lib/shell-rc.sh - managed rc block installing the interactive agent
# launchers (lib/launchers.sh) into the user's shell rc files.
#
# The block is the ONLY rc-file state ai-config owns: it exports
# AI_CONFIG_ROOT and sources lib/launchers.sh. No PATH edits — name
# interception happens via shell functions (see lib/launchers.sh header), so
# rc-file PATH-ordering blocks are exactly the machinery this replaces.
# Idempotent: install replaces an existing block in place, remove deletes it,
# both leave every other line untouched.

if [ -n "${AI_CONFIG_SHELL_RC_SH:-}" ]; then
  return 0
fi
AI_CONFIG_SHELL_RC_SH=1

shell_rc_begin_marker() {
  printf '%s\n' "# >>> ai-config launchers >>> (managed; remove with: bin/ai-setup --rc remove)"
}

shell_rc_end_marker() {
  printf '%s\n' "# <<< ai-config launchers <<<"
}

# ~/.bashrc always (created if absent — bash reads it for interactive shells);
# ~/.zshrc only when it already exists (never force zsh onto a machine).
shell_rc_targets() {
  printf '%s\n' "$HOME/.bashrc"
  [ -f "$HOME/.zshrc" ] && printf '%s\n' "$HOME/.zshrc"
  return 0
}

shell_rc_render_block() {
  shell_rc_begin_marker
  printf 'export AI_CONFIG_ROOT="%s"\n' "$(abs_path "$AI_CONFIG_ROOT")"
  # shellcheck disable=SC2016 — single quotes deliberate: expansion happens in
  # the user's shell at source time, not at render time.
  printf '%s\n' '[ -r "$AI_CONFIG_ROOT/lib/launchers.sh" ] && . "$AI_CONFIG_ROOT/lib/launchers.sh"'
  shell_rc_end_marker
}

shell_rc_has_block() {
  local file="$1"
  [ -f "$file" ] && grep -Fq "$(shell_rc_begin_marker)" "$file"
}

# Print FILE with the managed block (if any) removed. Tolerates a missing end
# marker by dropping through end-of-file — a truncated block is never left
# half-installed.
shell_rc_strip_block() {
  local file="$1"
  awk -v begin="$(shell_rc_begin_marker)" -v end="$(shell_rc_end_marker)" '
    $0 == begin { inblock = 1; next }
    inblock && $0 == end { inblock = 0; next }
    !inblock { print }
  ' "$file"
}

shell_rc_write() {
  local file="$1" content="$2" tmp
  tmp="$(mktemp "$file.aic.XXXXXX")" || return 1
  printf '%s' "$content" > "$tmp"
  # Preserve original permissions when replacing an existing rc.
  if [ -f "$file" ]; then
    chmod --reference="$file" "$tmp" 2>/dev/null || true
  fi
  mv "$tmp" "$file"
}

shell_rc_install() {
  local file stripped block updated
  block="$(shell_rc_render_block)"
  while IFS= read -r file; do
    if [ -f "$file" ]; then
      stripped="$(shell_rc_strip_block "$file")"
    else
      stripped=""
    fi
    updated="${stripped:+$stripped
}$block
"
    if [ "${dry_run:-0}" -eq 1 ]; then
      log_info "would install launcher rc block in $file"
      continue
    fi
    shell_rc_write "$file" "$updated" || {
      log_error "could not write $file"
      return 1
    }
    log_info "installed launcher rc block: $file"
  done < <(shell_rc_targets)
}

shell_rc_remove() {
  local file stripped
  while IFS= read -r file; do
    shell_rc_has_block "$file" || continue
    if [ "${dry_run:-0}" -eq 1 ]; then
      log_info "would remove launcher rc block from $file"
      continue
    fi
    stripped="$(shell_rc_strip_block "$file")"
    shell_rc_write "$file" "${stripped:+$stripped
}" || {
      log_error "could not write $file"
      return 1
    }
    log_info "removed launcher rc block: $file"
  done < <(shell_rc_targets)
}

shell_rc_status() {
  local file
  while IFS= read -r file; do
    if shell_rc_has_block "$file"; then
      if grep -Fq "AI_CONFIG_ROOT=\"$(abs_path "$AI_CONFIG_ROOT")\"" "$file"; then
        printf 'launcher rc block: installed (%s)\n' "$file"
      else
        printf 'launcher rc block: installed but points at another checkout (%s)\n' "$file"
      fi
    else
      printf 'launcher rc block: absent (%s)\n' "$file"
    fi
  done < <(shell_rc_targets)
}

shell_rc_main() {
  local action="$1"
  case "$action" in
    install) shell_rc_install ;;
    remove) shell_rc_remove ;;
    status) shell_rc_status ;;
    *)
      log_error "unknown rc action: $action"
      return 2
      ;;
  esac
}
