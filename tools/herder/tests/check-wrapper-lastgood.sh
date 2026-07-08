#!/usr/bin/env bash
# check-wrapper-lastgood.sh - the bin/herder self-building launcher must serve
# the last successfully built binary for a checkout when a rebuild fails against
# a mid-edit-broken tree (TASK-037), emitting ONE quiet line and NO compiler
# spew on the hook path. A never-built checkout still fails loud.
#
# Drives the REAL bin/herder against a throwaway minimal Go module in a fake
# AI_CONFIG_ROOT, with an isolated HOME/XDG_CACHE_HOME/TMPDIR so the cache and
# the last-good pointer never touch the real ones. This is the live-ish smoke:
# a real build breaks, serves last-good quietly, then rebuilds after the fix.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Env hygiene (TASK-019): ignore a spawn-exported HERDER_BIN; pin the root to
# THIS tree so we copy this checkout's wrapper + lib.
unset HERDER_BIN
REPO="$(cd "$TESTS_DIR/../../.." && pwd -P)"

ROOT="$(mktemp -d)"
cleanup() { rm -rf "$ROOT"; }
trap cleanup EXIT

fail=0
ok()  { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=1; }

assert_eq() {
  local name="$1" got="$2" want="$3"
  if [ "$got" = "$want" ]; then ok "$name"; else bad "$name" "got [$got] want [$want]"; fi
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

# A go toolchain is required to exercise the build path; skip cleanly if none is
# resolvable (the wrapper itself needs go, so there is nothing to test without
# it). The gate always has one.
if ! command -v go >/dev/null 2>&1 && ! { command -v mise >/dev/null 2>&1 && mise where go >/dev/null 2>&1; }; then
  printf 'SKIP  no go toolchain resolvable - wrapper build path not exercised\n'
  exit 0
fi

# Build a fake AI_CONFIG_ROOT: this checkout's wrapper + lib, plus a minimal
# buildable Go module at tools/herder that prints a tag we can flip per build.
make_root() { # make_root DIR TAG
  local dir="$1" tag="$2"
  mkdir -p "$dir/bin" "$dir/lib" "$dir/tools/herder/cmd/herder" "$dir/tools/herder/internal"
  cp "$REPO/bin/herder" "$dir/bin/herder"
  cp "$REPO/lib/common.sh" "$dir/lib/common.sh"
  chmod +x "$dir/bin/herder"
  cat > "$dir/tools/herder/go.mod" <<'GOMOD'
module example.com/h

go 1.21
GOMOD
  cat > "$dir/tools/herder/cmd/herder/main.go" <<'MAIN'
package main

import (
	"fmt"

	"example.com/h/internal"
)

func main() { fmt.Println("TAG=" + internal.Tag) }
MAIN
  write_tag "$dir" "$tag"
}
write_tag()  { printf 'package internal\n\nconst Tag = "%s"\n' "$2" > "$1/tools/herder/internal/ver.go"; }
break_tag()  { printf 'package internal\n\nconst Tag = \n' > "$1/tools/herder/internal/ver.go"; }

# Each root gets an isolated HOME/XDG_CACHE_HOME/TMPDIR so caches + the last-good
# pointer are per-case and never touch the real ones. PATH is preserved so go /
# mise resolve exactly as in real use.
run_wrapper() { # run_wrapper DIR ARGS...  -> sets OUT/ERR/RC
  local dir="$1"; shift
  local h="$dir/.env"
  mkdir -p "$h/tmp" "$h/.cache"
  local errf="$dir/.stderr"
  OUT="$(HOME="$h" XDG_CACHE_HOME="$h/.cache" XDG_CONFIG_HOME="$h/.config" \
    TMPDIR="$h/tmp" AI_CONFIG_ROOT="$dir" \
    bash "$dir/bin/herder" "$@" 2>"$errf")"
  RC=$?
  ERR="$(cat "$errf" 2>/dev/null || true)"
}

# --- lifecycle: build good, break -> serve last-good, fix -> rebuild ---------
LR="$ROOT/lifecycle"
make_root "$LR" V1

# 1. First invocation builds V1 cleanly.
run_wrapper "$LR" args-ignored
assert_eq       "build V1: exit 0"         "$RC"  "0"
assert_eq       "build V1: prints tag"     "$OUT" "TAG=V1"
assert_eq       "build V1: quiet stderr"   "$ERR" ""

# 2. Break the source (hash changes, package no longer compiles): the wrapper
#    serves last-good V1 with ONE quiet line and NO compiler spew.
break_tag "$LR"
run_wrapper "$LR" args-ignored
assert_eq       "broken: exit 0 (served)"        "$RC"  "0"
assert_eq       "broken: serves last-good V1"     "$OUT" "TAG=V1"
assert_contains "broken: quiet last-good line"    "$ERR" "herder: rebuild failed, serving last-good "
assert_eq       "broken: stderr is ONLY that line" "$(printf '%s\n' "$ERR" | grep -c .)" "1"
assert_not_contains "broken: no compiler spew (path)"   "$ERR" "internal/ver.go"
assert_not_contains "broken: no compiler spew (syntax)" "$ERR" "syntax error"

# 3. Fix the source to a NEW state: the wrapper rebuilds cleanly (no serve line).
write_tag "$LR" V2
run_wrapper "$LR" args-ignored
assert_eq       "fixed: exit 0"            "$RC"  "0"
assert_eq       "fixed: rebuilds to V2"    "$OUT" "TAG=V2"
assert_eq       "fixed: quiet stderr"      "$ERR" ""

# --- never-built checkout: broken from the start must fail LOUD --------------
NR="$ROOT/neverbuilt"
make_root "$NR" V1
break_tag "$NR"
run_wrapper "$NR" args-ignored
if [ "$RC" -ne 0 ]; then ok "never-built broken: nonzero exit"; else bad "never-built broken: nonzero exit" "rc=0"; fi
assert_eq       "never-built broken: no stdout"      "$OUT" ""
assert_not_contains "never-built broken: not served" "$ERR" "serving last-good"
# Loud = the real compiler output reaches stderr.
assert_contains "never-built broken: compiler output shown" "$ERR" "ver.go"

echo
if [ "$fail" -eq 0 ]; then
  printf 'ALL GREEN - wrapper serves last-good on broken rebuild, fails loud when never built.\n'
  exit 0
else
  printf 'CONTRACT DRIFT - see failures above.\n'
  exit 1
fi
