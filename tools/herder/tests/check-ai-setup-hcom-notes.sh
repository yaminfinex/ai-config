#!/usr/bin/env bash
# Hermetic contract for ai-setup applying hcom launch notes.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$TESTS_DIR/../../.." && pwd -P)"
ROOT="$(mktemp -d)"
trap 'rm -rf -- "$ROOT"' EXIT
mkdir -p "$ROOT/home/.codex" "$ROOT/bin" "$ROOT/hcom-bin" "$ROOT/config"

cat >"$ROOT/bin/mise" <<'EOF'
#!/usr/bin/env bash
if [[ ${1:-} == exec ]]; then
  shift
  [[ ${1:-} == -- ]] && shift
  PATH="$HCOM_TEST_BIN:$PATH" exec "$@"
fi
exit 0
EOF
cat >"$ROOT/hcom-bin/hcom" <<'EOF'
#!/usr/bin/env bash
printf '%q ' "$@" >>"$HCOM_TEST_LOG"
printf '\n' >>"$HCOM_TEST_LOG"
EOF
chmod +x "$ROOT/bin/mise" "$ROOT/hcom-bin/hcom"
export HCOM_TEST_LOG="$ROOT/hcom.log"
export HCOM_TEST_BIN="$ROOT/hcom-bin"

run_setup() {
  env -i HOME="$ROOT/home" PATH="$ROOT/bin:/usr/bin:/bin" \
    XDG_CONFIG_HOME="$ROOT/config" HCOM_TEST_LOG="$HCOM_TEST_LOG" \
    HCOM_TEST_BIN="$HCOM_TEST_BIN" \
    bash "$REPO/bin/ai-setup" "$@" 2>&1
}

fail=0
OUT="$(run_setup)"
RC=$?
[[ $RC -eq 0 ]] || { printf 'FAIL  default setup rc=%s\n' "$RC"; fail=1; }
grep -F 'config notes ' "$HCOM_TEST_LOG" >/dev/null \
  && printf 'PASS  default setup applies hcom launch notes\n' \
  || { printf 'FAIL  default setup did not apply hcom launch notes\n'; fail=1; }

: >"$HCOM_TEST_LOG"
OUT="$(run_setup --dry-run)"
[[ $OUT == *"DRY mise exec -- $REPO/tools/fleet/apply-hcom-notes.sh"* ]] \
  && printf 'PASS  dry-run reports hcom launch notes apply\n' \
  || { printf 'FAIL  dry-run omitted hcom launch notes apply\n'; fail=1; }
[[ ! -s $HCOM_TEST_LOG ]] \
  && printf 'PASS  dry-run does not call hcom\n' \
  || { printf 'FAIL  dry-run called hcom\n'; fail=1; }

((fail == 0)) || exit 1
printf 'ALL GREEN - ai-setup hcom launch notes contract holds.\n'
