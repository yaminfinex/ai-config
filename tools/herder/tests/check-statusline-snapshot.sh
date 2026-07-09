#!/usr/bin/env bash
# check-statusline-snapshot.sh — hermetic reader check for the hcom statusline
# state-file contract. The writer's path safety and atomic replace behavior are
# covered by sidecarcmd unit tests.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
STATUSLINE="$REPO_ROOT/claude/statusline.sh"

ROOT="$(mktemp -d)"
TMP_RENDER_TEST="$REPO_ROOT/tools/herder/internal/sidecarcmd/statusline_snapshot_render_tmp_test.go"
cleanup() {
  rm -rf "$ROOT"
  rm -f "$TMP_RENDER_TEST"
}
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

render_ctx() {
  env -i \
    PATH="/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin" \
    HCOM_DIR="$ROOT/hcom" HCOM_INSTANCE_NAME="worker-rive" "$STATUSLINE" <<'JSON'
{"workspace":{"project_dir":"/tmp/statusline-project"},"model":{"display_name":"Test Model"},"session_id":"sess-1","context_window":{"used_percentage":24,"total_input_tokens":61768,"context_window_size":258400}}
JSON
}

mkdir -p "$ROOT/hcom/statusline"

OUT="$(render 2>&1)"
case "$OUT" in
  *"✉"*|*"last "*) bad "absent snapshot omits bus segment" "out=$OUT" ;;
  *) ok "absent snapshot omits bus segment" ;;
esac
rm -f "$ROOT/hcom/statusline/worker-rive.env"
OUT="$(render_ctx 2>&1)"
case "$OUT" in
  *"mktemp"*|*"CTX_"*) bad "context render without snapshot is quiet" "out=$OUT" ;;
  *) ok "context render without snapshot is quiet" ;;
esac
if [[ -e "$ROOT/hcom/statusline/worker-rive.env" ]]; then
  bad "context render does not recreate removed snapshot" "$(cat "$ROOT/hcom/statusline/worker-rive.env")"
else
  ok "context render does not recreate removed snapshot"
fi

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
OUT="$(render_ctx 2>&1)"
if grep -q '^CTX_PCT=24$' "$ROOT/hcom/statusline/worker-rive.env"; then
  ok "context render updates existing snapshot"
else
  bad "context render updates existing snapshot" "$(cat "$ROOT/hcom/statusline/worker-rive.env")"
fi

cat > "$TMP_RENDER_TEST" <<'GO'
package sidecarcmd

import (
	"os"
	"testing"
	"time"
)

func TestRenderStatuslineSnapshotForShellContract(t *testing.T) {
	out := os.Getenv("STATUSLINE_RENDER_OUT")
	if out == "" {
		t.Fatal("STATUSLINE_RENDER_OUT is required")
	}
	content := renderStatuslineSnapshot(hcomRow{Name: "worker-rive", UnreadCount: 4, StatusAgeS: 1}, time.Now())
	if err := os.WriteFile(out, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
GO
if (cd "$REPO_ROOT/tools/herder" && STATUSLINE_RENDER_OUT="$ROOT/hcom/statusline/worker-rive.env" go test ./internal/sidecarcmd -run TestRenderStatuslineSnapshotForShellContract -count=1 >/dev/null); then
  OUT="$(render 2>&1)"
  case "$OUT" in
    *"✉ 4"* ) ok "writer-rendered snapshot shows unread" ;;
    *) bad "writer-rendered snapshot shows unread" "out=$OUT" ;;
  esac
  if grep -Eq 'last [0-5]s' <<<"$OUT"; then
    ok "writer-rendered snapshot computes age"
  else
    bad "writer-rendered snapshot computes age" "out=$OUT"
  fi
else
  bad "writer-rendered snapshot generated" "go test failed"
fi

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
