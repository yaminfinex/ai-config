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

assert_not_exists() {
  local name="$1" path="$2"
  [ ! -e "$path" ] && [ ! -L "$path" ] && ok "$name" || bad "$name" "unexpected path exists: $path"
}

assert_symlink() {
  local name="$1" path="$2"
  [ -L "$path" ] && ok "$name" || bad "$name" "not a symlink: $path"
}

make_case() {
  local name="$1"
  local codex_dir="${2:-with-codex}"
  CASE_DIR="$ROOT/$name"
  HOME_DIR="$CASE_DIR/home"
  BACKUPS="$CASE_DIR/backups"
  XDG_DIR="$CASE_DIR/xdg"
  mkdir -p "$HOME_DIR" "$BACKUPS" "$XDG_DIR"
  [ "$codex_dir" = "with-codex" ] && mkdir -p "$HOME_DIR/.codex"
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

# 1. default setup skips Codex config when ~/.codex did not already exist, while explicit install creates it.
make_case no_codex_default no-codex
run_setup
assert_eq "no codex default: exit 0" "$RUN_RC" "0"
assert_contains "no codex default: explains skip" "$RUN_OUT" "did not exist before setup"
assert_not_exists "no codex default: no config fabricated" "$HOME_DIR/.codex/config.toml"

run_setup --codex-config install
assert_eq "no codex explicit: exit 0" "$RUN_RC" "0"
assert_file_contains "no codex explicit: config created" "$HOME_DIR/.codex/config.toml" "$status_line"

# 2. default setup can be explicitly opted out even when ~/.codex exists.
make_case skip_flag
cat > "$HOME_DIR/.codex/config.toml" <<'TOML'
model = "user-model"
TOML
run_setup --skip-codex-config
assert_eq "skip flag: exit 0" "$RUN_RC" "0"
assert_contains "skip flag: explains skip" "$RUN_OUT" "--skip-codex-config requested"
assert_file_not_contains "skip flag: no status_line" "$HOME_DIR/.codex/config.toml" "status_line ="

# 3. install merges into an existing [tui] table, preserves user keys, backs up, and is idempotent.
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

# 4. default setup applies the shared Codex config when ~/.codex already existed.
make_case default
run_setup --dry-run
assert_eq "default dry-run: exit 0" "$RUN_RC" "0"
assert_contains "default dry-run: reports codex merge" "$RUN_OUT" "would merge"

# 5. symlinked config stays a symlink; writes and backups target the resolved file.
make_case symlink
mkdir -p "$CASE_DIR/target"
cat > "$CASE_DIR/target/config.toml" <<'TOML'
model = "user-model"

[tui]
status_line = ["model"]
TOML
ln -s "$CASE_DIR/target/config.toml" "$HOME_DIR/.codex/config.toml"
run_setup --codex-config install
assert_eq "symlink: exit 0" "$RUN_RC" "0"
assert_symlink "symlink: link preserved" "$HOME_DIR/.codex/config.toml"
assert_file_contains "symlink: target managed" "$CASE_DIR/target/config.toml" "$status_line"
assert_file_contains "symlink: target backed up" "$BACKUPS/20260708T000000/${CASE_DIR#/}/target/config.toml" 'status_line = ["model"]'

make_case dangling
ln -s "$CASE_DIR/missing/config.toml" "$HOME_DIR/.codex/config.toml"
run_setup --codex-config install
assert_eq "dangling: exit 1" "$RUN_RC" "1"
assert_contains "dangling: guidance" "$RUN_OUT" "dangling symlink"
assert_symlink "dangling: link preserved" "$HOME_DIR/.codex/config.toml"
assert_not_exists "dangling: target not created" "$CASE_DIR/missing/config.toml"

# 6. remove restores pre-install values recorded by install.
make_case restore
cat > "$HOME_DIR/.codex/config.toml" <<'TOML'
model = "user-model"

[tui]
notifications = false
status_line = ["model"]
terminal_title = ["project"]
TOML
run_setup --codex-config install
assert_eq "restore install: exit 0" "$RUN_RC" "0"
assert_file_contains "restore install: state recorded" "$HOME_DIR/.codex/ai-config-codex-config.state" "status_line"
run_setup --codex-config remove
assert_eq "restore remove: exit 0" "$RUN_RC" "0"
assert_file_contains "restore remove: status restored" "$HOME_DIR/.codex/config.toml" 'status_line = ["model"]'
assert_file_contains "restore remove: title restored" "$HOME_DIR/.codex/config.toml" 'terminal_title = ["project"]'
assert_not_exists "restore remove: state removed" "$HOME_DIR/.codex/ai-config-codex-config.state"

# 7. remove deletes keys that install created.
make_case remove_created
cat > "$HOME_DIR/.codex/config.toml" <<TOML
model = "user-model"

[tui]
notifications = false
TOML
before_user="$(user_markers)"
run_setup --codex-config install
assert_eq "remove created install: exit 0" "$RUN_RC" "0"
run_setup --codex-config remove
assert_eq "remove created: exit 0" "$RUN_RC" "0"
assert_file_not_contains "remove created: status_line gone" "$HOME_DIR/.codex/config.toml" "$status_line"
assert_file_not_contains "remove created: terminal_title gone" "$HOME_DIR/.codex/config.toml" "$terminal_title"
assert_eq "remove created: user markers preserved" "$(user_markers)" "$before_user"

# 8. remove leaves user-modified footer values alone when no ai-config state exists.
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

# 9. unsafe managed values abort safely and explain why.
make_case multiline
cat > "$HOME_DIR/.codex/config.toml" <<'TOML'
[tui]
status_line = [
  "model",
]
TOML
before="$(cat "$HOME_DIR/.codex/config.toml")"
run_setup --codex-config install
assert_eq "multiline abort: exit 0" "$RUN_RC" "0"
assert_contains "multiline abort: explains" "$RUN_OUT" "multi-line or unsupported"
assert_eq "multiline abort: unchanged" "$(cat "$HOME_DIR/.codex/config.toml")" "$before"

make_case inline_comment
cat > "$HOME_DIR/.codex/config.toml" <<'TOML'
[tui]
status_line = ["model"] # keep this comment
TOML
before="$(cat "$HOME_DIR/.codex/config.toml")"
run_setup --codex-config install
assert_eq "inline comment abort: exit 0" "$RUN_RC" "0"
assert_contains "inline comment abort: explains" "$RUN_OUT" "inline comments"
assert_eq "inline comment abort: unchanged" "$(cat "$HOME_DIR/.codex/config.toml")" "$before"

make_case crlf
printf '[tui]\r\nstatus_line = ["model"]\r\n' > "$HOME_DIR/.codex/config.toml"
before_sum="$(cksum "$HOME_DIR/.codex/config.toml")"
run_setup --codex-config install
assert_eq "crlf abort: exit 0" "$RUN_RC" "0"
assert_contains "crlf abort: explains" "$RUN_OUT" "CRLF"
assert_eq "crlf abort: unchanged" "$(cksum "$HOME_DIR/.codex/config.toml")" "$before_sum"

echo
if [ "$fail" -eq 0 ]; then
  printf 'ALL GREEN - ai-setup Codex config management holds.\n'
  exit 0
else
  printf 'CONTRACT DRIFT - see failures above.\n'
  exit 1
fi
