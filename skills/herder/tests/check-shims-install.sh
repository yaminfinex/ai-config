#!/usr/bin/env bash
# check-shims-install.sh - hermetic tests for ai-setup --shims.
#
# Uses fake HOME/XDG_CONFIG_HOME. Never touches real ~/.config/mise.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$TESTS_DIR/../../.." && pwd -P)"
AI_SETUP="$REPO/bin/ai-setup"
SHIM_DIR="$REPO/skills/herder/shims"

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

assert_not_contains() {
  local name="$1" haystack="$2" needle="$3"
  case "$haystack" in
    *"$needle"*) bad "$name" "unexpected [$needle] in [$haystack]" ;;
    *) ok "$name" ;;
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
  CONF_FILE="$XDG_DIR/mise/conf.d/ai-config-shims.toml"
  PATH_VALUE="$PATH_BASE"
  mkdir -p "$HOME_DIR" "$XDG_DIR"
}

run_setup() {
  RUN_OUT="$(env -i \
    PATH="$PATH_VALUE" HOME="$HOME_DIR" XDG_CONFIG_HOME="$XDG_DIR" SHELL=/bin/zsh \
    bash "$AI_SETUP" "$@" 2>&1)"
  RUN_RC=$?
}

expected_config() {
  cat <<EOF
# Managed by ai-config --shims. Remove with: bin/ai-setup --shims remove
[env]
_.path = ["$SHIM_DIR"]
EOF
}

# 1. install writes the managed mise conf.d file under XDG_CONFIG_HOME.
make_case install
run_setup --shims install
assert_eq "install: exit 0" "$RUN_RC" "0"
assert_eq "install: exact config" "$(cat "$CONF_FILE")" "$(expected_config)"
assert_not_contains "install: no shim args env written" "$(cat "$CONF_FILE")" "HERDER_SHIM_ARGS_CLAUDE"

# 2. status reports installed, path match, PATH count, and type-a ordering.
PATH_VALUE="$SHIM_DIR:$PATH_BASE"
run_setup --shims status
assert_eq "status installed: exit 0" "$RUN_RC" "0"
assert_contains "status installed: overall" "$RUN_OUT" "herder shims: installed"
assert_contains "status installed: path match" "$RUN_OUT" "matches this checkout: yes"
assert_contains "status installed: path count" "$RUN_OUT" "PATH entries for shim dir: 1"
assert_contains "status installed: claude first" "$RUN_OUT" "claude: shim first"
assert_contains "status installed: codex first" "$RUN_OUT" "codex: shim first"

# 3. status warns when another executable shadows the shim.
OTHERBIN="$CASE_DIR/otherbin"
mkdir -p "$OTHERBIN"
printf '#!/usr/bin/env bash\nexit 0\n' > "$OTHERBIN/claude"
chmod +x "$OTHERBIN/claude"
PATH_VALUE="$OTHERBIN:$SHIM_DIR:$PATH_BASE"
run_setup --shims status
assert_eq "status shadow: exit 0" "$RUN_RC" "0"
assert_contains "status shadow: claude shadowed" "$RUN_OUT" "claude: shadowed before shim ($OTHERBIN/claude)"

# 4. remove deletes only our managed file and is idempotent.
PATH_VALUE="$PATH_BASE"
run_setup --shims remove
assert_eq "remove: exit 0" "$RUN_RC" "0"
assert_absent "remove: file gone" "$CONF_FILE"
run_setup --shims remove
assert_eq "remove absent: exit 0" "$RUN_RC" "0"
assert_contains "remove absent: reports absent" "$RUN_OUT" "herder shims already absent"

# 5. install refuses to overwrite an unmanaged file.
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

# 6. dry-run install reports without writing.
make_case dryrun
run_setup --dry-run --shims install
assert_eq "dry-run install: exit 0" "$RUN_RC" "0"
assert_contains "dry-run install: reports file" "$RUN_OUT" "would write $CONF_FILE"
assert_contains "dry-run install: prints config" "$RUN_OUT" "_.path = [\"$SHIM_DIR\"]"
assert_absent "dry-run install: no file" "$CONF_FILE"

# 7. absent status is explicit.
run_setup --shims status
assert_eq "status absent: exit 0" "$RUN_RC" "0"
assert_contains "status absent: overall" "$RUN_OUT" "herder shims: absent"

echo
if [ "$fail" -eq 0 ]; then
  printf 'ALL GREEN - ai-setup shim installer holds.\n'
  exit 0
else
  printf 'CONTRACT DRIFT - see failures above.\n'
  exit 1
fi
