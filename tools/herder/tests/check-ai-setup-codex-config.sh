#!/usr/bin/env bash
# check-ai-setup-codex-config.sh - hermetic tests for shared Codex config.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AI_SETUP="$TESTS_DIR/../../../bin/ai-setup"
unset HERDER_BIN
export AI_CONFIG_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"

ROOT="$(mktemp -d)"
cleanup() { rm -rf "$ROOT"; }
trap cleanup EXIT

PATH_BASE="/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

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

assert_file_contains() {
  local name="$1" file="$2" needle="$3"
  if grep -Fq "$needle" "$file" 2>/dev/null; then ok "$name"; else bad "$name" "missing [$needle] in $file"; fi
}

assert_file_not_contains() {
  local name="$1" file="$2" needle="$3"
  if grep -Fq "$needle" "$file" 2>/dev/null; then bad "$name" "unexpected [$needle] in $file"; else ok "$name"; fi
}

make_case() {
  local name="$1"
  CASE_DIR="$ROOT/$name"
  HOME_DIR="$CASE_DIR/home"
  BACKUPS="$CASE_DIR/backups"
  XDG_DIR="$CASE_DIR/xdg"
  mkdir -p "$HOME_DIR/.codex" "$BACKUPS" "$XDG_DIR"
}

run_setup() {
  RUN_OUT="$(env -i \
    PATH="$PATH_BASE" HOME="$HOME_DIR" SHELL=/bin/zsh \
    XDG_CONFIG_HOME="$XDG_DIR" \
    AI_CONFIG_BACKUP_DIR="$BACKUPS" AI_CONFIG_TIMESTAMP=20260708T000000 \
    bash "$AI_SETUP" "$@" 2>&1)"
  RUN_RC=$?
}

user_markers() {
  grep -E '^(model = "user-model"|notifications = false|approval_policy = "never")$' "$HOME_DIR/.codex/config.toml" || true
}

status_line='status_line = ["model-with-reasoning", "context-remaining", "git-branch", "current-dir"]'
terminal_title='terminal_title = ["spinner", "project", "git-branch", "model", "status"]'

# 1. install merges into an existing [tui] table, preserves user keys, backs up, and is idempotent.
make_case install
cat > "$HOME_DIR/.codex/config.toml" <<'TOML'
model = "user-model"
approval_policy = "never"

[tui]
notifications = false
status_line = ["model"]

[features]
hooks = true
TOML

before_user="$(user_markers)"
run_setup --codex-config install
assert_eq "install: exit 0" "$RUN_RC" "0"
assert_file_contains "install: status_line managed" "$HOME_DIR/.codex/config.toml" "$status_line"
assert_file_contains "install: terminal_title managed" "$HOME_DIR/.codex/config.toml" "$terminal_title"
assert_file_contains "install: user tui key preserved" "$HOME_DIR/.codex/config.toml" "notifications = false"
assert_file_contains "install: later table preserved" "$HOME_DIR/.codex/config.toml" "[features]"
assert_eq "install: user markers preserved" "$(user_markers)" "$before_user"
assert_file_contains "install: backup exists" "$BACKUPS/20260708T000000/.codex/config.toml" 'status_line = ["model"]'

run_setup --codex-config install
assert_eq "install twice: exit 0" "$RUN_RC" "0"
assert_eq "install twice: one status_line" "$(grep -c '^status_line =' "$HOME_DIR/.codex/config.toml")" "1"
assert_eq "install twice: one terminal_title" "$(grep -c '^terminal_title =' "$HOME_DIR/.codex/config.toml")" "1"

run_setup --codex-config status
assert_contains "status installed" "$RUN_OUT" "Codex config: installed"

# 2. default setup also applies the shared Codex config.
make_case default
run_setup --dry-run
assert_eq "default dry-run: exit 0" "$RUN_RC" "0"
assert_contains "default dry-run: reports codex merge" "$RUN_OUT" "would merge"

# 3. remove drops only managed lines and preserves user content.
make_case remove
cat > "$HOME_DIR/.codex/config.toml" <<TOML
model = "user-model"

[tui]
notifications = false
$status_line
$terminal_title
TOML
before_user="$(user_markers)"
run_setup --codex-config remove
assert_eq "remove: exit 0" "$RUN_RC" "0"
assert_file_not_contains "remove: status_line gone" "$HOME_DIR/.codex/config.toml" "$status_line"
assert_file_not_contains "remove: terminal_title gone" "$HOME_DIR/.codex/config.toml" "$terminal_title"
assert_eq "remove: user markers preserved" "$(user_markers)" "$before_user"

# 4. remove leaves user-modified footer values alone.
make_case custom
cat > "$HOME_DIR/.codex/config.toml" <<'TOML'
[tui]
status_line = ["model", "git-branch"]
terminal_title = ["project"]
TOML
run_setup --codex-config remove
assert_eq "custom remove: exit 0" "$RUN_RC" "0"
assert_file_contains "custom remove: status_line kept" "$HOME_DIR/.codex/config.toml" 'status_line = ["model", "git-branch"]'
assert_file_contains "custom remove: terminal_title kept" "$HOME_DIR/.codex/config.toml" 'terminal_title = ["project"]'

echo
if [ "$fail" -eq 0 ]; then
  printf 'ALL GREEN - ai-setup Codex config management holds.\n'
  exit 0
else
  printf 'CONTRACT DRIFT - see failures above.\n'
  exit 1
fi
