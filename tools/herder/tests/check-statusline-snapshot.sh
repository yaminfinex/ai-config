#!/usr/bin/env bash
# check-statusline-snapshot.sh — hermetic reader check for the hcom statusline
# state-file contract. The writer's path safety and atomic replace behavior are
# covered by sidecarcmd unit tests.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
STATUSLINE="$REPO_ROOT/claude/statusline.sh"

ROOT="$(mktemp -d)"
cleanup() { rm -rf "$ROOT"; }
trap cleanup EXIT

fail=0
ok()  { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=1; }

render() {
  env -i \
    PATH="/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin" \
    HCOM_DIR="$ROOT/hcom" HCOM_INSTANCE_NAME="worker-rive" "$STATUSLINE" <<'JSON'
{"workspace":{"project_dir":"/tmp/statusline-project"},"model":{"display_name":"Test Model"},"session_id":"sess-1"}
JSON
}

mkdir -p "$ROOT/hcom/statusline"

OUT="$(render 2>&1)"
case "$OUT" in
  *"✉"*|*"last "*) bad "absent snapshot omits bus segment" "out=$OUT" ;;
  *) ok "absent snapshot omits bus segment" ;;
esac

cat > "$ROOT/hcom/statusline/worker-rive.env" <<'EOF'
HCOM_UNREAD=3
HCOM_LAST_AGE_S=42
EOF
OUT="$(render 2>&1)"
case "$OUT" in
  *"✉ 3"* ) ok "fallback age snapshot shows unread" ;;
  *) bad "fallback age snapshot shows unread" "out=$OUT" ;;
esac
case "$OUT" in
  *"last 42s"* ) ok "fallback age snapshot shows age" ;;
  *) bad "fallback age snapshot shows age" "out=$OUT" ;;
esac

now="${EPOCHSECONDS:-$(date +%s)}"
cat > "$ROOT/hcom/statusline/worker-rive.env" <<EOF
HCOM_UNREAD=0
HCOM_LAST_TS=$now
HCOM_LAST_AGE_S=999
EOF
OUT="$(render 2>&1)"
case "$OUT" in
  *"✉"* ) bad "zero unread hides unread marker" "out=$OUT" ;;
  *) ok "zero unread hides unread marker" ;;
esac
if grep -Eq 'last [0-2]s' <<<"$OUT"; then
  ok "timestamp snapshot computes fresh age"
else
  bad "timestamp snapshot computes fresh age" "out=$OUT"
fi
case "$OUT" in
  *"999"* ) bad "timestamp snapshot overrides stale fallback" "out=$OUT" ;;
  *) ok "timestamp snapshot overrides stale fallback" ;;
esac

exit "$fail"
