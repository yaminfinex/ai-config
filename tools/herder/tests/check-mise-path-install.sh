#!/usr/bin/env bash
# check-mise-path-install.sh - hermetic tests for ai-setup's managed mise PATH file.
#
# Uses fake HOME/XDG_CONFIG_HOME. Never touches real ~/.config/mise.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$TESTS_DIR/../../.." && pwd -P)"
AI_SETUP="$REPO/bin/ai-setup"
BIN_DIR="$REPO/bin"
SHIM_DIR="$REPO/tools/herder/shims"

ROOT="$(mktemp -d)"
cleanup() { rm -rf "$ROOT"; }
trap cleanup EXIT

PATH_BASE="/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

fail=0
ok()  { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=1; }

assert_eq() {
  local name="$1" got="$2" want="$3"
  if [ "$got" = "$want" ]; then
    ok "$name"
  else
    bad "$name" "got [$got] want [$want]"
  fi
}

assert_contains() {
  local name="$1" haystack="$2" needle="$3"
  case "$haystack" in
    *"$needle"*) ok "$name" ;;
    *) bad "$name" "missing [$needle] in [$haystack]" ;;
  esac
}

assert_absent() {
  local name="$1" path="$2"
  [ ! -e "$path" ] && ok "$name" || bad "$name" "still exists: $path"
}

make_case() {
  local name="$1"
  CASE_DIR="$ROOT/$name"
  HOME_DIR="$CASE_DIR/home"
  XDG_DIR="$CASE_DIR/xdg"
  CONF_FILE="$XDG_DIR/mise/conf.d/ai-config.toml"
  PATH_VALUE="$PATH_BASE"
  mkdir -p "$HOME_DIR" "$XDG_DIR/mise"
}

run_setup() {
  RUN_OUT="$(env -i \
    PATH="$PATH_VALUE" HOME="$HOME_DIR" XDG_CONFIG_HOME="$XDG_DIR" SHELL=/bin/zsh \
    bash "$AI_SETUP" "$@" 2>&1)"
  RUN_RC=$?
}

expected_config() {
  cat <<EOF
# Managed by ai-config. Remove with: bin/ai-setup --shims remove
[env]
_.path = ["$BIN_DIR", "$SHIM_DIR"]
HERDER_SHIM_ARGS_CLAUDE = "--dangerously-skip-permissions"
HERDER_SHIM_ARGS_CODEX = "--dangerously-bypass-approvals-and-sandbox"
EOF
}

# 1. Default setup writes the managed mise conf.d file under XDG_CONFIG_HOME.
make_case default
run_setup
assert_eq "default setup: exit 0" "$RUN_RC" "0"
assert_eq "default setup: exact config" "$(cat "$CONF_FILE")" "$(expected_config)"

# 2. --shims install is a compatibility alias for the same file.
make_case install
run_setup --shims install
assert_eq "install alias: exit 0" "$RUN_RC" "0"
assert_eq "install alias: exact config" "$(cat "$CONF_FILE")" "$(expected_config)"

# 3. status reports installed, both path matches, PATH counts, and type-a ordering.
PATH_VALUE="$BIN_DIR:$SHIM_DIR:$PATH_BASE"
run_setup --shims status
assert_eq "status installed: exit 0" "$RUN_RC" "0"
assert_contains "status installed: overall" "$RUN_OUT" "ai-config mise PATH: installed"
assert_contains "status installed: bin configured" "$RUN_OUT" "bin path configured: yes"
assert_contains "status installed: shim configured" "$RUN_OUT" "shim path configured: yes"
assert_contains "status installed: bin path count" "$RUN_OUT" "PATH entries for bin dir: 1"
assert_contains "status installed: shim path count" "$RUN_OUT" "PATH entries for shim dir: 1"
assert_contains "status installed: herder first" "$RUN_OUT" "herder: expected first"
assert_contains "status installed: claude first" "$RUN_OUT" "claude: expected first"
assert_contains "status installed: codex first" "$RUN_OUT" "codex: expected first"

# 4. status warns when another executable shadows a managed path entry.
OTHERBIN="$CASE_DIR/otherbin"
mkdir -p "$OTHERBIN"
printf '#!/usr/bin/env bash\nexit 0\n' > "$OTHERBIN/herder"
chmod +x "$OTHERBIN/herder"
PATH_VALUE="$OTHERBIN:$BIN_DIR:$SHIM_DIR:$PATH_BASE"
run_setup --shims status
assert_eq "status shadow: exit 0" "$RUN_RC" "0"
assert_contains "status shadow: herder shadowed" "$RUN_OUT" "herder: shadowed before expected ($OTHERBIN/herder)"

# 5. remove deletes only our managed file and is idempotent.
PATH_VALUE="$PATH_BASE"
run_setup --shims remove
assert_eq "remove: exit 0" "$RUN_RC" "0"
assert_absent "remove: file gone" "$CONF_FILE"
run_setup --shims remove
assert_eq "remove absent: exit 0" "$RUN_RC" "0"
assert_contains "remove absent: reports absent" "$RUN_OUT" "ai-config mise PATH config already absent"

# 6. install refuses to overwrite an unmanaged file.
make_case foreign
mkdir -p "$(dirname "$CONF_FILE")"
printf '# user file\n[env]\n_.path = ["/tmp/user"]\n' > "$CONF_FILE"
run_setup --shims install
assert_eq "foreign install: exit 1" "$RUN_RC" "1"
assert_contains "foreign install: refuses" "$RUN_OUT" "refusing to overwrite unmanaged mise config"
assert_contains "foreign install: preserved" "$(cat "$CONF_FILE")" "/tmp/user"
run_setup --shims remove
assert_eq "foreign remove: exit 1" "$RUN_RC" "1"
assert_contains "foreign remove: refuses" "$RUN_OUT" "refusing to remove unmanaged mise config"

# 7. dry-run install reports without writing.
make_case dryrun
run_setup --dry-run --shims install
assert_eq "dry-run install: exit 0" "$RUN_RC" "0"
assert_contains "dry-run install: reports file" "$RUN_OUT" "would write $CONF_FILE"
assert_contains "dry-run install: prints config" "$RUN_OUT" "_.path = [\"$BIN_DIR\", \"$SHIM_DIR\"]"
assert_absent "dry-run install: no file" "$CONF_FILE"

# 8. absent status is explicit.
run_setup --shims status
assert_eq "status absent: exit 0" "$RUN_RC" "0"
assert_contains "status absent: overall" "$RUN_OUT" "ai-config mise PATH: absent"

# 9. no mise on PATH and no mise config dir is a hard failure for install/default.
make_case no_mise
rmdir "$XDG_DIR/mise"
PATH_VALUE="/usr/bin:/bin"
run_setup --shims install
assert_eq "missing mise install: exit 1" "$RUN_RC" "1"
assert_contains "missing mise install: explains prerequisite" "$RUN_OUT" "mise is required for ai-config PATH setup"
run_setup
assert_eq "missing mise default: exit 1" "$RUN_RC" "1"
assert_contains "missing mise default: explains prerequisite" "$RUN_OUT" "mise is required for ai-config PATH setup"

echo
if [ "$fail" -eq 0 ]; then
  printf 'ALL GREEN - ai-setup mise PATH management holds.\n'
  exit 0
else
  printf 'CONTRACT DRIFT - see failures above.\n'
  exit 1
fi
