#!/usr/bin/env bash
# Shared library for the mish scenario gate harnesses (U10, spec AC-1..15,
# AC-19). Each check builds a real mish binary, runs against the real
# Backlog.md CLI, keeps all writes inside a temp workspace, and prints
# ALL GREEN only after its assertions pass.
set -euo pipefail

MISH_TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MISH_MODULE_DIR="$(cd "$MISH_TESTS_DIR/.." && pwd)"
FIXTURES="$MISH_TESTS_DIR/fixtures"
ORIG_PATH="$PATH"

export GOTOOLCHAIN=local
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_CONFIG_SYSTEM=/dev/null

fail() {
  echo "FAIL: $*" >&2
  if [ -n "${WORK:-}" ]; then
    for log in "$WORK"/*.out "$WORK"/*.err "$WORK"/git.log; do
      [ -s "$log" ] || continue
      echo "--- $log" >&2
      sed -n '1,120p' "$log" >&2
    done
  fi
  exit 1
}

ok() { echo "ok: $*"; }
step() { echo "--- $*"; }
all_green() { echo "ALL GREEN"; }

preflight() {
  local need tdecl have dep version
  need=$(awk '$1 == "go" {print $2; exit}' "$MISH_MODULE_DIR/go.mod")
  [ -n "$need" ] || fail "cannot read the toolchain pin ('go X.Y.Z') from $MISH_MODULE_DIR/go.mod"
  tdecl=$(awk '$1 == "toolchain" {print $2; exit}' "$MISH_MODULE_DIR/go.mod")
  [ -z "$tdecl" ] || [ "$tdecl" = "go$need" ] ||
    fail "go.mod declares toolchain ${tdecl} but pins go ${need}; the go directive is the authority — align or drop the toolchain directive"
  local pinned_export="export PATH=\"\$(mise where go@${need})/bin:\$PATH\" && export GOTOOLCHAIN=local"
  command -v go >/dev/null 2>&1 ||
    fail "no go on PATH; this module needs go >= ${need}. Playbook-pinned toolchain: ${pinned_export}"
  have=$(go env GOVERSION); have=${have#go}
  if [ "$(printf '%s\n' "$need" "$have" | sort -V | head -n1)" != "$need" ]; then
    fail "go ${have} on PATH is older than go.mod (${need}) and GOTOOLCHAIN=local forbids auto-download. ${pinned_export}"
  fi
  for dep in backlog git awk sed grep find sort cmp sha256sum mktemp cp mv; do
    command -v "$dep" >/dev/null 2>&1 || fail "harness dependency missing: $dep"
  done
  version=$(backlog --version 2>/dev/null | tr -d '[:space:]')
  # Exact 1.47.x is intentional: this harness pins the verified Backlog.md behaviour floor.
  case "$version" in
    1.47.*) ;;
    *) fail "Backlog.md version ${version:-unknown} is not the verified 1.47.x floor" ;;
  esac
}

setup_workspace() {
  WORK=$(mktemp -d "${TMPDIR:-/tmp}/mish-gate.XXXXXX")
  HOME_DIR="$WORK/home"
  BIN="$WORK/bin"
  MISSIONS_REPO_DIR="$WORK/missions-repo"
  INVOKE_DIR="$WORK/worktree"
  mkdir -p "$HOME_DIR" "$BIN" "$MISSIONS_REPO_DIR" "$INVOKE_DIR"
  trap cleanup_workspace EXIT
}

cleanup_workspace() {
  if [ "${MISH_GATE_KEEP:-0}" = "1" ]; then
    echo "workspace kept: $WORK" >&2
  else
    rm -rf "$WORK"
  fi
}

build_mish() {
  (cd "$MISH_MODULE_DIR" && go build -o "$BIN/mish" ./cmd/mish) || fail "go build mish"
}

mish_env() {
  local env_args=(
    env -i \
    HOME="$HOME_DIR" \
    USER="mish-u10" \
    PATH="$BIN:$ORIG_PATH" \
    GIT_CONFIG_GLOBAL=/dev/null \
    GIT_CONFIG_SYSTEM=/dev/null \
    MISSIONS_REPO="$MISSIONS_REPO_DIR"
  )
  if [ "${SESSION_OWNER_VALUE+x}" = "x" ]; then
    env_args+=(SESSION_OWNER="$SESSION_OWNER_VALUE")
  fi
  "${env_args[@]}" "$@"
}

mish_env_no_repo() {
  local env_args=(
    env -i \
    HOME="$HOME_DIR" \
    USER="mish-u10" \
    PATH="$BIN:$ORIG_PATH" \
    GIT_CONFIG_GLOBAL=/dev/null \
    GIT_CONFIG_SYSTEM=/dev/null
  )
  if [ "${SESSION_OWNER_VALUE+x}" = "x" ]; then
    env_args+=(SESSION_OWNER="$SESSION_OWNER_VALUE")
  fi
  "${env_args[@]}" "$@"
}

run_mish() {
  local cwd=$1 name=$2
  shift 2
  LAST_OUT="$WORK/${name}.out"
  LAST_ERR="$WORK/${name}.err"
  set +e
  (cd "$cwd" && mish_env mish "$@" >"$LAST_OUT" 2>"$LAST_ERR")
  LAST_STATUS=$?
  set -e
}

run_mish_no_repo() {
  local cwd=$1 name=$2
  shift 2
  LAST_OUT="$WORK/${name}.out"
  LAST_ERR="$WORK/${name}.err"
  set +e
  (cd "$cwd" && mish_env_no_repo mish "$@" >"$LAST_OUT" 2>"$LAST_ERR")
  LAST_STATUS=$?
  set -e
}

assert_status() {
  local want=$1
  [ "$LAST_STATUS" -eq "$want" ] || fail "exit status $LAST_STATUS, want $want for $LAST_OUT"
}

assert_contains() {
  local file=$1 needle=$2
  grep -F -- "$needle" "$file" >/dev/null || fail "$file does not contain: $needle"
}

assert_not_contains() {
  local file=$1 needle=$2
  if grep -F -- "$needle" "$file" >/dev/null; then
    fail "$file unexpectedly contains: $needle"
  fi
}

assert_file() {
  [ -f "$1" ] || fail "missing file: $1"
}

assert_dir() {
  [ -d "$1" ] || fail "missing dir: $1"
}

assert_no_file() {
  [ ! -e "$1" ] || fail "unexpected path exists: $1"
}

assert_eq() {
  local got=$1 want=$2 desc=$3
  [ "$got" = "$want" ] || fail "$desc: got [$got], want [$want]"
}

mission_dir() {
  echo "$MISSIONS_REPO_DIR/missions/$1"
}

new_mission() {
  local slug=$1
  shift
  local cwd="$WORK/new-cwd-$slug"
  mkdir -p "$cwd"
  run_mish "$cwd" "new-$slug" new "$slug" "$@"
  assert_status 0
}

task_files() {
  local slug=$1 dir
  for dir in "$(mission_dir "$slug")/backlog/tasks" "$(mission_dir "$slug")/backlog/completed"; do
    [ -d "$dir" ] || continue
    find "$dir" -type f -name '*.md'
  done | sort
}

single_task_file() {
  local slug=$1 count
  count=$(task_files "$slug" | wc -l | awk '{print $1}')
  [ "$count" = "1" ] || fail "expected one task file for $slug, got $count"
  task_files "$slug"
}

write_marker() {
  local dir=$1 slug=$2
  mkdir -p "$dir"
  printf '%s\n' "$slug" >"$dir/.mission"
}

tree_hash() {
  local dir=$1
  (cd "$dir" && find . -type f -print0 | sort -z | xargs -0 sha256sum) | sha256sum | awk '{print $1}'
}

git_init_repo() {
  local dir=$1
  git -C "$dir" init -q
  git -C "$dir" config user.email mish-u10@example.invalid
  git -C "$dir" config user.name "mish u10"
}

git_init_bare_origin() {
  local origin=$1
  git init -q --bare "$origin"
  git -C "$origin" symbolic-ref HEAD refs/heads/main
}

git_commit_all() {
  local dir=$1 msg=$2
  git -C "$dir" add .
  git -C "$dir" commit -q -m "$msg"
}

replace_in_file() {
  local file=$1 from=$2 to=$3
  sed -i "s/${from}/${to}/" "$file"
}
