#!/usr/bin/env bash
# check-codex-update.sh - hermetic contract for bin/codex-update and the
# ai-doctor sibling-helper tripwire. No network: installs from a fake package
# dir into a throwaway CODEX_BIN_DIR with stub executables.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$TESTS_DIR/../../.." && pwd -P)"
UPD="$REPO/bin/codex-update"
DOCTOR="$REPO/bin/ai-doctor"
ROOT="$(mktemp -d)"
cleanup() { rm -rf "$ROOT"; }
trap cleanup EXIT

fail=0
ok()  { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=1; }
assert_rc() { if [ "$2" -eq "$3" ]; then ok "$1"; else bad "$1" "rc=$2 want=$3"; fi; }
assert_contains() { case "$2" in *"$3"*) ok "$1" ;; *) bad "$1" "missing [$3] in: $2" ;; esac; }

stub() { # stub <path> <version-string>
  printf '#!/bin/sh\necho "codex-cli %s"\n' "$2" > "$1"; chmod +x "$1"
}
make_pkg() { # make_pkg <dir> <version> [--no-helper]
  mkdir -p "$1/bin"
  stub "$1/bin/codex" "$2"
  [ "${3:-}" = --no-helper ] || { printf '#!/bin/sh\nexit 0\n' > "$1/bin/codex-code-mode-host"; chmod +x "$1/bin/codex-code-mode-host"; }
}

BIN="$ROOT/bin"
export CODEX_BIN_DIR="$BIN"

# 1. --check on an empty dir: none installed, helper missing, rc 1.
out=$("$UPD" --check --quick 2>&1); rc=$?
assert_rc "check empty dir rc 1" "$rc" 1
assert_contains "check empty reports none" "$out" "codex: none"
assert_contains "check empty reports missing helper" "$out" "MISSING helper(s) beside codex: codex-code-mode-host"

# 2. Install from a package: both files land, executable, version reported.
make_pkg "$ROOT/pkg1" 1.0.0
out=$("$UPD" --from-package "$ROOT/pkg1" 2>&1); rc=$?
assert_rc "install from package rc 0" "$rc" 0
assert_contains "install reports transition" "$out" "codex none -> 1.0.0"
[ -x "$BIN/codex" ] && ok "codex installed executable" || bad "codex installed executable" "missing"
[ -x "$BIN/codex-code-mode-host" ] && ok "helper installed executable" || bad "helper installed executable" "missing"
ls "$BIN" | grep -q codex-update && bad "no staging leftovers" "$(ls "$BIN")" || ok "no staging leftovers"

# 3. --check after install: rc 0, helpers present.
out=$("$UPD" --check --quick 2>&1); rc=$?
assert_rc "check after install rc 0" "$rc" 0
assert_contains "check reports version" "$out" "codex: 1.0.0"
assert_contains "check reports helpers" "$out" "helpers present: codex-code-mode-host"

# 4. Atomic swap: a process holding the old binary keeps its inode; the path
#    now serves the new version.
old_inode=$(stat -c %i "$BIN/codex")
make_pkg "$ROOT/pkg2" 2.0.0
( exec 3<"$BIN/codex"; "$UPD" --from-package "$ROOT/pkg2" >/dev/null 2>&1; new_inode=$(stat -c %i "$BIN/codex"); held=$(stat -L -c %i /dev/fd/3); [ "$held" = "$old_inode" ] && [ "$new_inode" != "$old_inode" ] ) \
  && ok "swap is atomic (old inode held, new inode at path)" || bad "swap is atomic" "inode contract broken"
assert_contains "path serves new version" "$("$BIN/codex" --version)" "2.0.0"

# 5. A package without the required helper is refused, install untouched.
make_pkg "$ROOT/pkg3" 3.0.0 --no-helper
out=$("$UPD" --from-package "$ROOT/pkg3" 2>&1); rc=$?
assert_rc "package missing helper refused" "$rc" 1
assert_contains "refusal names helper" "$out" "missing required helper bin/codex-code-mode-host"
assert_contains "refused install leaves old version" "$("$BIN/codex" --version)" "2.0.0"

# 6. ai-doctor tripwire: helper removed -> warning naming the fix; present -> silent.
rm -f "$BIN/codex-code-mode-host"
out=$("$DOCTOR" --quick 2>&1 || true)
assert_contains "doctor warns on missing helper" "$out" "codex at $BIN/codex is missing sibling helper(s): codex-code-mode-host"
assert_contains "doctor names the fix" "$out" "run bin/codex-update"
out=$("$UPD" --check --quick 2>&1); rc=$?
assert_rc "check flags removed helper rc 1" "$rc" 1
printf '#!/bin/sh\nexit 0\n' > "$BIN/codex-code-mode-host"; chmod +x "$BIN/codex-code-mode-host"
out=$("$DOCTOR" --quick 2>&1 || true)
case "$out" in *"missing sibling helper"*) bad "doctor silent when helper present" "still warns" ;; *) ok "doctor silent when helper present" ;; esac

if [ "$fail" -eq 0 ]; then echo "ALL GREEN"; else echo "FAILURES"; exit 1; fi
