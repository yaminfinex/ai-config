#!/usr/bin/env bash

if [ -n "${AI_CONFIG_COMMON_SH:-}" ]; then
  return 0
fi
AI_CONFIG_COMMON_SH=1

AI_CONFIG_ROOT="${AI_CONFIG_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)}"
AI_CONFIG_TIMESTAMP="${AI_CONFIG_TIMESTAMP:-$(date +%Y%m%dT%H%M%S)}"
AI_CONFIG_BACKUP_DIR="${AI_CONFIG_BACKUP_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/ai-config/backups}"

SECRET_REGEX='(sk-[A-Za-z0-9]{20,}|AKIA[0-9A-Z]{16}|ghp_[A-Za-z0-9]{36}|-----BEGIN [A-Z ]+PRIVATE KEY-----)'
HOME_PATH_REGEX='/(Users|home)/[A-Za-z0-9._-]+/'

PUSH_PATHS=(
  ".claude"
  ".agents/skills"
  "skills"
  "claude/CLAUDE.md"
  "claude/hooks"
  "claude/commands"
  "claude/statusline.sh"
  "claude/settings.shared.json"
  "claude/settings.local.example.json"
  "codex/AGENTS.md"
  "cursor/rules"
  "bin"
  "lib"
  "docs"
  "references"
  "vendor"
  "README.md"
  ".gitignore"
)

PORTABLE_SCAN_PATHS=(
  ".agents/skills"
  "skills"
  "claude/hooks"
  "claude/commands"
  "claude/statusline.sh"
  "claude/settings.shared.json"
  "claude/settings.local.example.json"
  "codex/AGENTS.md"
  "cursor/rules"
  "bin"
  "lib"
)

IGNORED_LIVE_SKILL_NAMES=(
  ".system"
  ".curated"
  ".experimental"
  ".git"
  "node_modules"
)

log_info() {
  printf 'INFO %s\n' "$*"
}

log_warn() {
  printf 'WARN %s\n' "$*" >&2
}

log_error() {
  printf 'ERROR %s\n' "$*" >&2
}

abs_path() {
  local path="$1"
  local dir
  local base

  if [ -d "$path" ]; then
    (cd "$path" && pwd -P)
    return
  fi

  dir="$(dirname "$path")"
  base="$(basename "$path")"
  printf '%s/%s\n' "$(cd "$dir" && pwd -P)" "$base"
}

resolve_link_target() {
  local link_path="$1"
  local target

  target="$(readlink "$link_path")"
  case "$target" in
    /*) printf '%s\n' "$target" ;;
    *) printf '%s/%s\n' "$(cd "$(dirname "$link_path")" && pwd -P)" "$target" ;;
  esac
}

symlink_points_to() {
  local link_path="$1"
  local desired="$2"
  local actual
  local desired_abs

  [ -L "$link_path" ] || return 1
  actual="$(resolve_link_target "$link_path")"
  desired_abs="$(abs_path "$desired")"
  [ "$actual" = "$desired_abs" ]
}

repo_skill_names() {
  local skill_dir

  [ -d "$AI_CONFIG_ROOT/skills" ] || return 0
  for skill_dir in "$AI_CONFIG_ROOT"/skills/*; do
    [ -d "$skill_dir" ] || continue
    [ -f "$skill_dir/SKILL.md" ] || continue
    basename "$skill_dir"
  done | sort
}

repo_skill_exists() {
  local name="$1"
  [ -f "$AI_CONFIG_ROOT/skills/$name/SKILL.md" ]
}

is_ignored_live_skill_name() {
  local name="$1"
  local ignored

  for ignored in "${IGNORED_LIVE_SKILL_NAMES[@]}"; do
    [ "$name" = "$ignored" ] && return 0
  done
  return 1
}

portable_link_specs() {
  printf '%s\n' \
    "claude/CLAUDE.md|$HOME/.claude/CLAUDE.md" \
    "claude/hooks|$HOME/.claude/hooks" \
    "claude/commands|$HOME/.claude/commands" \
    "claude/statusline.sh|$HOME/.claude/statusline.sh" \
    "codex/AGENTS.md|$HOME/.codex/AGENTS.md" \
    "cursor/rules|$HOME/.cursor/rules"
}

skill_root_specs() {
  printf '%s\n' "claude|$HOME/.claude|$HOME/.claude/skills"
  printf '%s\n' "codex|$HOME/.codex|$HOME/.codex/skills"

  if [ -d "$HOME/.agents" ] || [ "${AI_CONFIG_INCLUDE_AGENTS_SKILLS:-}" = "1" ]; then
    printf '%s\n' "agents|$HOME/.agents|$HOME/.agents/skills"
  fi
}

project_skill_root_specs() {
  local dir

  dir="$(pwd -P)"
  while [ "$dir" != "/" ]; do
    [ "$dir" = "$HOME" ] && break

    [ -d "$dir/.claude/skills" ] && printf '%s\n' "project-claude|$dir|$dir/.claude/skills"
    [ -d "$dir/.agents/skills" ] && printf '%s\n' "project-agents|$dir|$dir/.agents/skills"
    [ -d "$dir/.codex/skills" ] && printf '%s\n' "project-codex|$dir|$dir/.codex/skills"

    dir="$(dirname "$dir")"
  done
}

tracked_or_existing_path() {
  local path="$1"

  if [ -e "$AI_CONFIG_ROOT/$path" ]; then
    return 0
  fi

  if git -C "$AI_CONFIG_ROOT" ls-files -- "$path" "$path/" | grep -q .; then
    return 0
  fi

  return 1
}

existing_scan_paths() {
  local path

  for path in "$@"; do
    [ -e "$AI_CONFIG_ROOT/$path" ] || continue
    printf '%s\n' "$AI_CONFIG_ROOT/$path"
  done
}

scan_secret_regex_paths() {
  local paths=("$@")
  [ "${#paths[@]}" -gt 0 ] || return 0

  grep -RInE "$SECRET_REGEX" "${paths[@]}" 2>/dev/null || true
}

scan_portability_paths() {
  local paths=("$@")
  [ "${#paths[@]}" -gt 0 ] || return 0

  grep -RInE "$HOME_PATH_REGEX" "${paths[@]}" 2>/dev/null || true
}

staged_files() {
  git -C "$AI_CONFIG_ROOT" diff --cached --name-only --diff-filter=ACMRT
}

portable_staged_files() {
  local file

  staged_files | while IFS= read -r file; do
    case "$file" in
      .agents/skills/*|.claude/skills/*|skills/*|claude/hooks/*|claude/commands/*|claude/statusline.sh|claude/settings.shared.json|claude/settings.local.example.json|codex/AGENTS.md|cursor/rules/*|bin/*|lib/*)
        printf '%s/%s\n' "$AI_CONFIG_ROOT" "$file"
        ;;
    esac
  done
}

scan_staged_secret_regex() {
  local files=()
  local file

  while IFS= read -r file; do
    [ -f "$AI_CONFIG_ROOT/$file" ] || continue
    files+=("$AI_CONFIG_ROOT/$file")
  done < <(staged_files)

  [ "${#files[@]}" -gt 0 ] || return 0
  grep -InE "$SECRET_REGEX" "${files[@]}" 2>/dev/null || true
}

scan_staged_portability() {
  local files=()
  local file

  while IFS= read -r file; do
    [ -f "$file" ] || continue
    files+=("$file")
  done < <(portable_staged_files)

  [ "${#files[@]}" -gt 0 ] || return 0
  grep -InE "$HOME_PATH_REGEX" "${files[@]}" 2>/dev/null || true
}

git_dirty() {
  ! git -C "$AI_CONFIG_ROOT" diff --quiet ||
    ! git -C "$AI_CONFIG_ROOT" diff --cached --quiet ||
    [ -n "$(git -C "$AI_CONFIG_ROOT" ls-files --others --exclude-standard)" ]
}

backup_target() {
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
  mv "$target" "$dest"
  printf '%s\n' "$dest"
}
