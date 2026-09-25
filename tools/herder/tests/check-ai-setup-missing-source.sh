#!/usr/bin/env bash
# check-ai-setup-missing-source.sh - hermetic contract: a portable link spec
# whose repo source is missing is skipped with a warning, and its live target
# is neither backed up nor replaced by a dangling link.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$TESTS_DIR/../../.." && pwd -P)"
AI_SETUP="$REPO/bin/ai-setup"

ROOT="$(mktemp -d)"
trap 'rm -rf -- "$ROOT"' EXIT

# mise and hcom stubs: ai-setup requires mise and applies hcom launch notes.
mkdir -p "$ROOT/bin"
printf '#!/usr/bin/env bash\nexit 0\n' >"$ROOT/bin/hcom"
cat >"$ROOT/bin/mise" <<'STUB'
#!/usr/bin/env bash
if [[ ${1:-} == exec ]]; then
  shift
  [[ ${1:-} == -- ]] && shift
  exec "$@"
fi
exit 0
STUB
chmod +x "$ROOT/bin/mise" "$ROOT/bin/hcom"

fail=0
ok()  { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=1; }

assert_eq() {
  local name="$1" got="$2" want="$3"
  if [ "$got" = "$want" ]; then ok "$name"; else bad "$name" "got [$got] want [$want]"; fi
}

assert_contains() {
  local name="$1" haystack="$2" needle="$3"
  case "$haystack" in *"$needle"*) ok "$name" ;; *) bad "$name" "missing [$needle] in [$haystack]" ;; esac
}

assert_not_contains() {
  local name="$1" haystack="$2" needle="$3"
  case "$haystack" in *"$needle"*) bad "$name" "unexpected [$needle] in output" ;; *) ok "$name" ;; esac
}

assert_not_exists() {
  local name="$1" path="$2"
  if [ ! -e "$path" ] && [ ! -L "$path" ]; then ok "$name"; else bad "$name" "unexpected path exists: $path"; fi
}

assert_plain_file() {
  local name="$1" path="$2" want="$3"
  if [ -L "$path" ]; then
    bad "$name" "became a symlink: $path -> $(readlink "$path")"
  elif [ "$(cat "$path" 2>/dev/null)" != "$want" ]; then
    bad "$name" "content changed: $path"
  else
    ok "$name"
  fi
}

make_case() {
  CASE_DIR="$ROOT/$1"
  HOME_DIR="$CASE_DIR/home"
  BACKUPS="$CASE_DIR/backups"
  mkdir -p "$HOME_DIR/.codex" "$HOME_DIR/.claude" "$BACKUPS" "$CASE_DIR/xdg"
}

# $1 is the AI_CONFIG_ROOT the run links from; the rest are ai-setup args.
run_setup() {
  local config_root="$1"
  shift
  RUN_OUT="$(env -i \
    PATH="$ROOT/bin:/usr/bin:/bin" HOME="$HOME_DIR" SHELL=/bin/bash \
    XDG_CONFIG_HOME="$CASE_DIR/xdg" AI_CONFIG_ROOT="$config_root" \
    AI_CONFIG_BACKUP_DIR="$BACKUPS" AI_CONFIG_TIMESTAMP=20260925T000000 \
    bash "$AI_SETUP" "$@" 2>&1)"
  RUN_RC=$?
}

# 1. The real tree no longer links codex/AGENTS.md: a live ~/.codex/AGENTS.md
#    survives both a dry run and a full run untouched and unbacked-up.
make_case codex_agents
printf 'live codex agents\n' >"$HOME_DIR/.codex/AGENTS.md"
run_setup "$REPO" --dry-run
assert_eq "codex dry-run: exit 0" "$RUN_RC" "0"
assert_not_contains "codex dry-run: AGENTS.md not mentioned" "$RUN_OUT" "codex/AGENTS.md"
run_setup "$REPO"
assert_eq "codex full run: exit 0" "$RUN_RC" "0"
assert_not_contains "codex full run: AGENTS.md not mentioned" "$RUN_OUT" "codex/AGENTS.md"
assert_plain_file "codex full run: live AGENTS.md untouched" "$HOME_DIR/.codex/AGENTS.md" "live codex agents"
assert_not_exists "codex full run: no AGENTS.md backup" "$BACKUPS/20260925T000000/.codex/AGENTS.md"

# 2. Guard: a copy of the repo with two linked sources deleted. One target
#    already exists as a real file, the other is absent.
FAKE="$ROOT/fake-root"
mkdir -p "$FAKE/tools/fleet"
cp -R "$REPO/bin" "$REPO/lib" "$REPO/claude" "$REPO/codex" "$FAKE/"
printf '#!/usr/bin/env bash\nexit 0\n' >"$FAKE/tools/fleet/apply-hcom-notes.sh"
chmod +x "$FAKE/tools/fleet/apply-hcom-notes.sh"
mkdir -p "$FAKE/skills"
rm -f "$FAKE/claude/statusline.sh" "$FAKE/claude/hooks/notes-refresh.sh"

for mode in --dry-run "" --heal-only; do
  label="guard ${mode:-full}"
  make_case "guard${mode:-full}"
  printf 'live statusline\n' >"$HOME_DIR/.claude/statusline.sh"
  # shellcheck disable=SC2086 # empty $mode means no flag
  run_setup "$FAKE" $mode
  assert_eq "$label: exit 0" "$RUN_RC" "0"
  assert_contains "$label: warns on missing statusline source" "$RUN_OUT" \
    "WARN skip link: repo source missing: $FAKE/claude/statusline.sh"
  assert_contains "$label: warns on missing hook source" "$RUN_OUT" \
    "WARN skip link: repo source missing: $FAKE/claude/hooks/notes-refresh.sh"
  assert_contains "$label: setup still completes" "$RUN_OUT" "ai-setup complete"
  assert_plain_file "$label: existing target untouched" "$HOME_DIR/.claude/statusline.sh" "live statusline"
  assert_not_exists "$label: existing target not backed up" "$BACKUPS/20260925T000000/.claude/statusline.sh"
  assert_not_exists "$label: absent target not created" "$HOME_DIR/.claude/hooks/notes-refresh.sh"
done

echo
if [ "$fail" -eq 0 ]; then
  printf 'ALL GREEN - ai-setup refuses links to missing repo sources.\n'
  exit 0
else
  printf 'CONTRACT DRIFT - see failures above.\n'
  exit 1
fi
