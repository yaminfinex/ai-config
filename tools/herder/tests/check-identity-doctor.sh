#!/usr/bin/env bash
# check-identity-doctor.sh - hermetic identity-bearing-shell warning contract.
#
# The fixture is a clean throwaway git checkout with only the health surfaces
# ai-doctor needs. That keeps unrelated workstation warnings out of the result,
# so the --strict case proves this situational nudge is deliberately exempt from
# repo-health escalation.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$TESTS_DIR/../../.." && pwd -P)"
AI_DOCTOR="$REPO/bin/ai-doctor"
ROOT="$(mktemp -d)"
cleanup() { rm -rf "$ROOT"; }
trap cleanup EXIT

fail=0
ok()  { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=1; }

assert_rc() {
  local name="$1" got="$2" want="$3"
  if [ "$got" -eq "$want" ]; then
    ok "$name"
  else
    bad "$name" "rc=$got want=$want"
  fi
}

assert_contains() {
  local name="$1" haystack="$2" needle="$3"
  case "$haystack" in
    *"$needle"*) ok "$name" ;;
    *) bad "$name" "missing [$needle]" ;;
  esac
}

assert_not_contains() {
  local name="$1" haystack="$2" needle="$3"
  case "$haystack" in
    *"$needle"*) bad "$name" "unexpected [$needle]" ;;
    *) ok "$name" ;;
  esac
}

for dep in git jq mktemp; do
  command -v "$dep" >/dev/null 2>&1 || {
    printf 'FAIL  harness dependency missing: %s\n' "$dep" >&2
    exit 1
  }
done

FIXTURE="$ROOT/repo"
ORIGIN="$ROOT/origin.git"
HOME_DIR="$ROOT/home"
XDG_CONFIG="$ROOT/config"
XDG_STATE="$ROOT/state"
XDG_CACHE="$ROOT/cache"
VENDORBIN="$ROOT/vendorbin"
MOCKBIN="$ROOT/mockbin"
mkdir -p "$FIXTURE/bin" "$FIXTURE/lib" "$FIXTURE/tools/herder/shims" \
  "$HOME_DIR/.claude" "$XDG_CONFIG/mise/conf.d" "$XDG_STATE" "$XDG_CACHE" \
  "$VENDORBIN" "$MOCKBIN"

cp "$REPO/lib/common.sh" "$REPO/lib/mise-path.sh" "$REPO/lib/grok-health.sh" "$FIXTURE/lib/"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$FIXTURE/bin/herder"
for tool in claude codex grok; do
  printf '%s\n' '#!/bin/sh' '# herder-path-shim' 'exit 0' > "$FIXTURE/tools/herder/shims/$tool"
done
printf '%s\n' '#!/bin/sh' 'exit 0' > "$VENDORBIN/grok"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$MOCKBIN/mise"
chmod +x "$FIXTURE/bin/herder" "$FIXTURE/tools/herder/shims/claude" \
  "$FIXTURE/tools/herder/shims/codex" "$FIXTURE/tools/herder/shims/grok" \
  "$VENDORBIN/grok" "$MOCKBIN/mise"

printf '%s\n' '{"statusLine":{"command":"$HOME/.claude/statusline.sh"}}' > "$HOME_DIR/.claude/settings.json"
{
  printf '%s\n' '# Managed by ai-config. Remove with: bin/ai-setup --shims remove'
  printf '%s\n' '[env]'
  printf '_.path = ["%s", "%s"]\n' "$FIXTURE/bin" "$FIXTURE/tools/herder/shims"
} > "$XDG_CONFIG/mise/conf.d/ai-config.toml"

git init --bare "$ORIGIN" >/dev/null 2>&1
git -C "$FIXTURE" init -b main >/dev/null 2>&1
git -C "$FIXTURE" config user.name fixture
git -C "$FIXTURE" config user.email fixture@example.invalid
git -C "$FIXTURE" add .
git -C "$FIXTURE" commit -m fixture >/dev/null
git -C "$FIXTURE" remote add origin "$ORIGIN"
git -C "$FIXTURE" push -u origin main >/dev/null 2>&1

PATH_VALUE="$FIXTURE/bin:$FIXTURE/tools/herder/shims:$VENDORBIN:$MOCKBIN:/usr/bin:/bin"
BASE_ENV=(
  "PATH=$PATH_VALUE"
  "HOME=$HOME_DIR"
  "XDG_CONFIG_HOME=$XDG_CONFIG"
  "XDG_STATE_HOME=$XDG_STATE"
  "XDG_CACHE_HOME=$XDG_CACHE"
  "AI_CONFIG_ROOT=$FIXTURE"
  "XAI_API_KEY=present"
)

doctor() {
  (cd "$FIXTURE" && env -i "${BASE_ENV[@]}" "$@" bash "$AI_DOCTOR" --quick 2>&1)
}

doctor_strict() {
  (cd "$FIXTURE" && env -i "${BASE_ENV[@]}" "$@" bash "$AI_DOCTOR" --quick --strict 2>&1)
}

OUT="$(doctor)"
RC=$?
assert_rc "clean shell: warning tier rc 0" "$RC" 0
assert_not_contains "clean shell: identity warning silent" "$OUT" "current shell carries managed-agent identity"
assert_not_contains "clean shell: no unrelated warning masks contract" "$OUT" "WARN "
assert_contains "clean shell: doctor healthy" "$OUT" "INFO ai-doctor ok"

OUT="$(doctor HCOM_SESSION_ID= HERDER_GUID= HERDR_PANE_ID=)"
RC=$?
assert_rc "empty identity keys: rc 0" "$RC" 0
assert_not_contains "empty identity keys: warning silent" "$OUT" "current shell carries managed-agent identity"
assert_not_contains "empty identity keys: doctor stays warning-free" "$OUT" "WARN "

OUT="$(doctor HCOM_SESSION_ID=present HERDER_GUID=present HERDR_PANE_ID=present)"
RC=$?
assert_rc "identity shell: warning tier rc 0" "$RC" 0
assert_contains "identity shell: warning fires" "$OUT" "WARN current shell carries managed-agent identity"
assert_contains "identity shell: hcom session key named" "$OUT" "HCOM_SESSION_ID"
assert_contains "identity shell: herder guid key named" "$OUT" "HERDER_GUID"
assert_contains "identity shell: pane key named" "$OUT" "HERDR_PANE_ID"
assert_contains "identity shell: counted as one warning" "$OUT" "WARN ai-doctor found 1 warning(s)"

OUT="$(doctor_strict HCOM_SESSION_ID=present HERDER_GUID=present HERDR_PANE_ID=present)"
RC=$?
assert_rc "identity shell strict: situational warning does not fail repo health" "$RC" 0
assert_contains "identity shell strict: warning remains visible" "$OUT" "WARN current shell carries managed-agent identity"
assert_contains "identity shell strict: warning count remains visible" "$OUT" "WARN ai-doctor found 1 warning(s)"

echo
if [ "$fail" -eq 0 ]; then
  printf 'ALL GREEN - identity-bearing-shell doctor contract holds.\n'
  exit 0
fi
printf 'CONTRACT DRIFT - see failures above.\n'
exit 1
