#!/usr/bin/env bash
# check-grok-doctor.sh - hermetic Grok setup/doctor drift checks.
#
# Uses only a mock Grok binary and throwaway HOME/state/config/cache roots. The
# mock leaves an execution marker so the doctor contract proves it only reports
# the PATH-resolved vendor executable and never starts it.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$TESTS_DIR/../../.." && pwd -P)"
AI_DOCTOR="$REPO/bin/ai-doctor"
GO_VERSION="$(awk '/^go /{print $2; exit}' "$REPO/tools/herder/go.mod")"
GO_BIN="$(mise where "go@$GO_VERSION")/bin"
ROOT="$(mktemp -d)"
cleanup() { rm -rf "$ROOT"; }
trap cleanup EXIT

fail=0
ok()  { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=1; }

assert_contains() {
  local name="$1" haystack="$2" needle="$3"
  case "$haystack" in
    *"$needle"*) ok "$name" ;;
    *) bad "$name" "missing [$needle]" ;;
  esac
}

HOME_DIR="$ROOT/home"
LIVE_GROK="$HOME_DIR/.grok"
STATE_DIR="$ROOT/state/herder"
XDG_CONFIG="$ROOT/config"
XDG_CACHE="$ROOT/cache"
MOCKBIN="$ROOT/mockbin"
VENDORBIN="$ROOT/vendorbin"
mkdir -p "$LIVE_GROK" "$STATE_DIR" "$XDG_CONFIG/mise/conf.d" "$XDG_CACHE" \
  "$MOCKBIN" "$VENDORBIN/downloads"
printf '%s\n' 'live-user-state' > "$LIVE_GROK/sentinel"
printf '%s\n' '#!/bin/sh' 'printf executed > "$(dirname "$(dirname "$0")")/executed"' 'exit 93' > "$VENDORBIN/downloads/grok-current"
ln -s downloads/grok-current "$VENDORBIN/grok"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$MOCKBIN/mise"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$MOCKBIN/hcom"
chmod +x "$VENDORBIN/downloads/grok-current" "$MOCKBIN/mise" "$MOCKBIN/hcom"

cat > "$XDG_CONFIG/mise/conf.d/ai-config.toml" <<EOF
# Managed by ai-config. Remove with: bin/ai-setup --shims remove
[env]
_.path = ["$REPO/bin", "$REPO/tools/herder/shims"]
[tools]
"github:aannoo/hcom" = "0.7.23"
EOF

PATH_VALUE="$GO_BIN:$REPO/bin:$REPO/tools/herder/shims:$VENDORBIN:$MOCKBIN:/usr/bin:/bin"
OUT="$(env -i \
  PATH="$PATH_VALUE" HOME="$HOME_DIR" XDG_CONFIG_HOME="$XDG_CONFIG" \
  XDG_STATE_HOME="$ROOT/state" XDG_CACHE_HOME="$XDG_CACHE" \
  AI_CONFIG_ROOT="$REPO" HERDER_BIN="$REPO/bin/herder" HERDER_STATE_DIR="$STATE_DIR" \
  GROK_HOME="$ROOT/ambient-grok-home" \
  bash "$AI_DOCTOR" 2>&1)"
RC=$?

if [ "$RC" -eq 0 ]; then
  ok "doctor drift case: warnings do not auto-fix"
else
  bad "doctor drift case: warnings do not auto-fix" "rc=$RC output=$OUT"
fi
assert_contains "doctor: auth precondition reported by name" "$OUT" "XAI_API_KEY is absent or empty"
assert_contains "doctor: auth remedy names login profile" "$OUT" 'login-shell profile such as $HOME/.profile'
assert_contains "doctor: ambient home override reported" "$OUT" "herder launches deliberately unset it"
assert_contains "doctor: default live home reported" "$OUT" "$HOME_DIR/.grok"
assert_contains "doctor: PATH vendor reported without execution" "$OUT" "Grok vendor binary resolved after herder PATH shims (not executed): $VENDORBIN/grok"

HERDER_DOC="$(cat "$REPO/tools/herder/README.md")"
assert_contains "docs: default home named" "$HERDER_DOC" 'live vendor home at `~/.grok`'
assert_contains "docs: owner config never rewritten" "$HERDER_DOC" 'never rewrites the owner'
assert_contains "docs: PATH resolution named" "$HERDER_DOC" 'first executable after all herder shims'
assert_contains "docs: project MCP config named" "$HERDER_DOC" 'cwd-bound project MCP config'
assert_contains "docs: owner verification path" "$HERDER_DOC" 'manual-verification path is `herder launch grok`'
assert_contains "docs: shared-home fleet" "$HERDER_DOC" "Claude, Codex, and Grok share the user's live homes"
assert_contains "docs: future isolation option" "$HERDER_DOC" 'could provide multi-account isolation'
assert_contains "docs: manual guest is not a registered spawn" "$HERDER_DOC" 'bounded manual guest, not a registered spawn'
assert_contains "docs: explicit raw-vendor escape" "$HERDER_DOC" 'GROK=/absolute/path/to/grok grok'
if [[ "$HERDER_DOC" != *"Grok and Pi use the fully herder-managed model"* ]]; then
  ok "docs: no invented Pi managed-home claim"
else
  bad "docs: no invented Pi managed-home claim" "stale Pi family claim remains"
fi

if [ "$(cat "$LIVE_GROK/sentinel")" = "live-user-state" ]; then
  ok "doctor: live home untouched"
else
  bad "doctor: live home untouched" "doctor changed throwaway planted state"
fi

if [ ! -e "$VENDORBIN/executed" ]; then
  ok "doctor: vendor binary was not executed"
else
  bad "doctor: vendor binary was not executed" "execution marker exists"
fi

echo
if [ "$fail" -eq 0 ]; then
  printf 'ALL GREEN - Grok doctor contract holds.\n'
  exit 0
else
  printf 'CONTRACT DRIFT - see failures above.\n'
  exit 1
fi
