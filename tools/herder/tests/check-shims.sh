#!/usr/bin/env bash
# check-shims.sh - hermetic contract tests for the herder PATH shims.
#
# No live hcom, claude, codex, grok, or herder calls. Each scenario copies the real shim scripts
# into a temp repo layout, installs mock bin/herder and real binaries, then drives the shim
# through env -i.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SHIMS_DIR="$TESTS_DIR/../shims"
# Env hygiene (TASK-019): herder-spawned agents export HERDER_BIN/AI_CONFIG_ROOT
# pointing at the spawner's checkout — honoring them silently drives another
# tree's wrapper/sources. Ignore the binary override; pin the root to THIS tree.
unset HERDER_BIN
export AI_CONFIG_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"

ROOT="$(mktemp -d)"
cleanup() { rm -rf "$ROOT"; }
trap cleanup EXIT

PATH_BASE="/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

fail=0
ok()  { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s - %s\n' "$1" "$2"; fail=1; }

# Keep the shell mock's refusal byte-identical to the product constant. This
# probe is fail-closed before identity/state work and runs under throwaway roots.
mkdir -p "$ROOT/product-home" "$ROOT/product-state" "$ROOT/product-cache"
PRODUCT_GROK_REFUSAL="$(env -i PATH="$PATH_BASE" HOME="$ROOT/product-home" \
  XDG_CACHE_HOME="$ROOT/product-cache" HERDER_STATE_DIR="$ROOT/product-state" \
  AI_CONFIG_ROOT="$AI_CONFIG_ROOT" "$AI_CONFIG_ROOT/bin/herder" launch grok 2>&1)"
PRODUCT_GROK_RC=$?
if [[ "$PRODUCT_GROK_RC" -ne 0 && "$PRODUCT_GROK_REFUSAL" == *"XAI_API_KEY is absent or empty"* ]]; then
  ok "grok auth refusal drift guard: product constant captured"
else
  bad "grok auth refusal drift guard: product constant captured" "rc=$PRODUCT_GROK_RC output=$PRODUCT_GROK_REFUSAL"
fi

assert_eq() {
  local name="$1" got="$2" want="$3"
  if [[ "$got" == "$want" ]]; then
    ok "$name"
  else
    bad "$name" "got [$got] want [$want]"
  fi
}

assert_file_eq() {
  local name="$1" file="$2" want="$3" got
  got="$(cat "$file" 2>/dev/null || true)"
  assert_eq "$name" "$got" "$want"
}

assert_file_missing() {
  local name="$1" file="$2"
  if [[ ! -e "$file" ]]; then
    ok "$name"
  else
    bad "$name" "unexpected file exists: $file"
  fi
}

make_case() {
  local name="$1"
  CASE_DIR="$ROOT/$name"
  REPO_CASE="$CASE_DIR/repo"
  SHIM_CASE="$REPO_CASE/tools/herder/shims"
  HERDER_BIN="$REPO_CASE/bin"
  REALBIN="$CASE_DIR/realbin"
  OTHERBIN="$CASE_DIR/otherbin"
  PROBE="$CASE_DIR/probe"
  mkdir -p "$SHIM_CASE" "$HERDER_BIN" "$REALBIN" "$OTHERBIN" "$PROBE"
  cp "$SHIMS_DIR/claude" "$SHIM_CASE/claude"
  cp "$SHIMS_DIR/codex" "$SHIM_CASE/codex"
  cp "$SHIMS_DIR/grok" "$SHIM_CASE/grok"
  cp "$SHIMS_DIR/hcom" "$SHIM_CASE/hcom"

  cat > "$HERDER_BIN/herder" <<'MOCK_HERDER_LAUNCH'
#!/usr/bin/env bash
set -euo pipefail
: "${PROBE:?}"
printf '%s\n' "$@" >"$PROBE/herder_argv"
# Record the recursion-guard handoff so tests can assert the shim resolved the
# REAL hcom (not itself) before forwarding to `herder hook`.
printf '%s\n' "${HERDER_HOOK_HCOM-}" >"$PROBE/herder_hook_hcom"
if [[ "${MOCK_HERDER_REFUSE_GROK:-}" == "1" && "${1:-}" == "launch" && "${2:-}" == "grok" ]]; then
  : "${MOCK_GROK_AUTH_ERROR:?}"
  printf '%s\n' "$MOCK_GROK_AUTH_ERROR" >&2
  exit 1
fi
MOCK_HERDER_LAUNCH

  cat > "$REALBIN/claude" <<'MOCK_REAL_CLAUDE'
#!/usr/bin/env bash
set -euo pipefail
: "${PROBE:?}"
count="$(cat "$PROBE/real_claude_count" 2>/dev/null || printf '0')"
count=$((count + 1))
printf '%s\n' "$count" >"$PROBE/real_claude_count"
printf '%s\n' "$@" >"$PROBE/real_claude_argv"
MOCK_REAL_CLAUDE

  cat > "$REALBIN/codex" <<'MOCK_REAL_CODEX'
#!/usr/bin/env bash
set -euo pipefail
: "${PROBE:?}"
count="$(cat "$PROBE/real_codex_count" 2>/dev/null || printf '0')"
count=$((count + 1))
printf '%s\n' "$count" >"$PROBE/real_codex_count"
printf '%s\n' "$@" >"$PROBE/real_codex_argv"
MOCK_REAL_CODEX

  cat > "$REALBIN/hcom" <<'MOCK_REAL_HCOM'
#!/usr/bin/env bash
set -euo pipefail
: "${PROBE:?}"
printf '%s\n' "$@" >"$PROBE/real_hcom_argv"
MOCK_REAL_HCOM

  cat > "$REALBIN/grok" <<'MOCK_REAL_GROK'
#!/usr/bin/env bash
set -euo pipefail
: "${PROBE:?}"
count="$(cat "$PROBE/real_grok_count" 2>/dev/null || printf '0')"
count=$((count + 1))
printf '%s\n' "$count" >"$PROBE/real_grok_count"
printf '%s\n' "$@" >"$PROBE/real_grok_argv"
MOCK_REAL_GROK

  chmod +x "$SHIM_CASE/claude" "$SHIM_CASE/codex" "$SHIM_CASE/grok" "$SHIM_CASE/hcom" "$HERDER_BIN/herder" \
    "$REALBIN/claude" "$REALBIN/codex" "$REALBIN/grok" "$REALBIN/hcom"
}

run_with_timeout() {
  local seconds="$1" pid watch rc
  shift
  "$@" &
  pid=$!
  (
    sleep "$seconds"
    if kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
    fi
  ) &
  watch=$!
  wait "$pid"
  rc=$?
  kill "$watch" 2>/dev/null || true
  wait "$watch" 2>/dev/null || true
  return "$rc"
}

# 1. Plain invocation execs bin/herder launch with tool name + user args.
make_case plain
env -i PATH="$SHIM_CASE:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  "$SHIM_CASE/claude" --model opus "two words"
rc=$?
assert_eq "plain claude: exit 0" "$rc" "0"
assert_file_eq "plain claude: herder launch argv order" "$PROBE/herder_argv" \
  "$(printf '%s\n' launch claude --model opus "two words")"

# 2. Per-tool default args are deliberately whitespace-split and prepended.
make_case defaults_claude
env -i PATH="$SHIM_CASE:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  HERDER_SHIM_ARGS_CLAUDE="--dangerously-skip-permissions --print" \
  "$SHIM_CASE/claude" user-arg
rc=$?
assert_eq "defaults claude: exit 0" "$rc" "0"
assert_file_eq "defaults claude: prepended before user args" "$PROBE/herder_argv" \
  "$(printf '%s\n' launch claude --dangerously-skip-permissions --print user-arg)"

make_case defaults_codex
env -i PATH="$SHIM_CASE:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  HERDER_SHIM_ARGS_CODEX="--dangerously-bypass-approvals-and-sandbox" \
  "$SHIM_CASE/codex" --ask-for-approval never
rc=$?
assert_eq "defaults codex: exit 0" "$rc" "0"
assert_file_eq "defaults codex: prepended before user args" "$PROBE/herder_argv" \
  "$(printf '%s\n' launch codex --dangerously-bypass-approvals-and-sandbox --ask-for-approval never)"

# 3. Recursion guard skips the shim even when PATH names it through a symlink.
make_case recursion
ln -s "$SHIM_CASE" "$CASE_DIR/shimlink"
run_with_timeout 3 env -i \
  PATH="$CASE_DIR/shimlink:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  HCOM_LAUNCH_INFLIGHT=1 "$SHIM_CASE/claude" --real
rc=$?
assert_eq "recursion guard: exits through real binary" "$rc" "0"
assert_file_eq "recursion guard: real binary called once" "$PROBE/real_claude_count" "1"
assert_file_eq "recursion guard: real argv preserved" "$PROBE/real_claude_argv" \
  "$(printf '%s\n' --real)"
assert_file_missing "recursion guard: herder launch not called" "$PROBE/herder_argv"

# 4. Absolute invocation still uses repo-local bin/herder when shim dir is not first on PATH.
make_case not_first
env -i PATH="$OTHERBIN:$SHIM_CASE:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  "$SHIM_CASE/codex" run-here
rc=$?
assert_eq "not first on PATH: exit 0" "$rc" "0"
assert_file_eq "not first on PATH: repo launch used" "$PROBE/herder_argv" \
  "$(printf '%s\n' launch codex run-here)"

# 5. Missing bin/herder is a loud error, not silent fallthrough.
make_case missing_sibling
rm -f "$HERDER_BIN/herder"
err="$PROBE/stderr"
env -i PATH="$SHIM_CASE:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  "$SHIM_CASE/claude" hello 2>"$err"
rc=$?
if [[ "$rc" -ne 0 ]]; then
  ok "missing bin/herder: nonzero exit"
else
  bad "missing bin/herder: nonzero exit" "rc=0"
fi
if grep -q 'missing executable bin/herder' "$err"; then
  ok "missing bin/herder: loud diagnostic"
else
  bad "missing bin/herder: loud diagnostic" "stderr=$(cat "$err" 2>/dev/null)"
fi
assert_file_missing "missing bin/herder: no real binary fallback" "$PROBE/real_claude_count"

# 6. hcom shim forwards to `bin/herder hook` and exports HERDER_HOOK_HCOM to the
#    REAL hcom (found behind itself on PATH), so `herder hook` never recurses.
make_case hcom_forward
run_with_timeout 5 env -i \
  PATH="$SHIM_CASE:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  "$SHIM_CASE/hcom" sessionstart --extra flag
rc=$?
assert_eq "hcom shim: exit 0" "$rc" "0"
assert_file_eq "hcom shim: forwards to herder hook with args" "$PROBE/herder_argv" \
  "$(printf '%s\n' hook sessionstart --extra flag)"
assert_file_eq "hcom shim: exports HERDER_HOOK_HCOM to real hcom" "$PROBE/herder_hook_hcom" \
  "$(cd "$REALBIN" && pwd -P)/hcom"
assert_file_missing "hcom shim: real hcom not run directly (herder hook owns it)" "$PROBE/real_hcom_argv"

# 7. Recursion guard: even when PATH names the shim dir through a symlink, the
#    shim skips itself and resolves the real hcom rather than looping.
make_case hcom_recursion
ln -s "$SHIM_CASE" "$CASE_DIR/shimlink"
run_with_timeout 5 env -i \
  PATH="$CASE_DIR/shimlink:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  "$SHIM_CASE/hcom" send @luna -- hi
rc=$?
assert_eq "hcom recursion: exit 0 (no loop)" "$rc" "0"
assert_file_eq "hcom recursion: still resolves real hcom past the symlink" "$PROBE/herder_hook_hcom" \
  "$(cd "$REALBIN" && pwd -P)/hcom"

# 8. hcom shim with NO real hcom on PATH still forwards (herder hook degrades to
#    exit 0 itself); HERDER_HOOK_HCOM is left empty. PATH is a coreutils-only base
#    with no hcom, so the "no real hcom" condition is hermetic.
make_case hcom_no_real
run_with_timeout 5 env -i \
  PATH="$SHIM_CASE:/usr/bin:/bin" HOME="$HOME" PROBE="$PROBE" \
  "$SHIM_CASE/hcom" post
rc=$?
assert_eq "hcom no-real: exit 0" "$rc" "0"
assert_file_eq "hcom no-real: forwards to herder hook anyway" "$PROBE/herder_argv" \
  "$(printf '%s\n' hook post)"
assert_file_eq "hcom no-real: HERDER_HOOK_HCOM left empty" "$PROBE/herder_hook_hcom" ""

# 9. Sibling shim dirs on one PATH (worktree checkout + machine-wide main checkout):
#    the hcom shim must export HERDER_HOOK_HCOM to the REAL hcom, never to the
#    sibling shim — resolving each other made the two `herder hook`s ping-pong forever.
make_case hcom_sibling
SIBLING_REPO="$CASE_DIR/sibling"
SIBLING_SHIMS="$SIBLING_REPO/tools/herder/shims"
mkdir -p "$SIBLING_SHIMS"
cp "$SHIMS_DIR/claude" "$SHIMS_DIR/codex" "$SHIMS_DIR/hcom" "$SIBLING_SHIMS/"
chmod +x "$SIBLING_SHIMS/claude" "$SIBLING_SHIMS/codex" "$SIBLING_SHIMS/hcom"
run_with_timeout 5 env -i \
  PATH="$SHIM_CASE:$SIBLING_SHIMS:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  "$SHIM_CASE/hcom" post
rc=$?
assert_eq "hcom sibling: exit 0" "$rc" "0"
assert_file_eq "hcom sibling: forwards to herder hook" "$PROBE/herder_argv" \
  "$(printf '%s\n' hook post)"
assert_file_eq "hcom sibling: HERDER_HOOK_HCOM skips the sibling shim, hits real hcom" \
  "$PROBE/herder_hook_hcom" "$(cd "$REALBIN" && pwd -P)/hcom"

# 10. Same sibling layout under HCOM_LAUNCH_INFLIGHT=1: the claude shim must exec
#     the REAL claude once — exec'ing the sibling shim instead loops forever.
make_case claude_sibling
SIBLING_REPO="$CASE_DIR/sibling"
SIBLING_SHIMS="$SIBLING_REPO/tools/herder/shims"
mkdir -p "$SIBLING_SHIMS"
cp "$SHIMS_DIR/claude" "$SHIMS_DIR/codex" "$SHIMS_DIR/hcom" "$SIBLING_SHIMS/"
chmod +x "$SIBLING_SHIMS/claude" "$SIBLING_SHIMS/codex" "$SIBLING_SHIMS/hcom"
run_with_timeout 5 env -i \
  PATH="$SHIM_CASE:$SIBLING_SHIMS:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  HCOM_LAUNCH_INFLIGHT=1 "$SHIM_CASE/claude" --real
rc=$?
assert_eq "claude sibling inflight: exit 0 (no loop)" "$rc" "0"
assert_file_eq "claude sibling inflight: real binary called once" "$PROBE/real_claude_count" "1"
assert_file_eq "claude sibling inflight: real argv preserved" "$PROBE/real_claude_argv" \
  "$(printf '%s\n' --real)"
assert_file_missing "claude sibling inflight: herder launch not called" "$PROBE/herder_argv"

# 10a. A mise-generated `claude` shim (in a mise/shims dir) ahead of the real
#      binary is skipped, not dispatched — mise re-resolves claude via PATH and
#      loops back to a herder shim, freezing the pane. Regression: a stale mise
#      claude registration wedged panes twice; mise regenerates the shim on every
#      reshim, so the herder shim must be immune to it.
make_case mise_skip_claude
MISE_SHIMS="$CASE_DIR/mise/shims"
mkdir -p "$MISE_SHIMS"
cat > "$MISE_SHIMS/claude" <<'MOCK_MISE_CLAUDE'
#!/usr/bin/env bash
: "${PROBE:?}"
printf 'invoked\n' >>"$PROBE/mise_invoked"
exec claude "$@"
MOCK_MISE_CLAUDE
chmod +x "$MISE_SHIMS/claude"
run_with_timeout 5 env -i \
  PATH="$SHIM_CASE:$MISE_SHIMS:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  HCOM_LAUNCH_INFLIGHT=1 "$SHIM_CASE/claude" --real
rc=$?
assert_eq "mise skip (path): exit 0 (no loop)" "$rc" "0"
assert_file_eq "mise skip (path): real binary called once" "$PROBE/real_claude_count" "1"
assert_file_missing "mise skip (path): mise shim never dispatched" "$PROBE/mise_invoked"

# 10b. Detection also fires when a `codex` on PATH is a symlink resolving to the
#      mise multiplexer, even outside a mise/shims path (readlink-target branch).
make_case mise_skip_codex_symlink
mkdir -p "$CASE_DIR/fakemise" "$CASE_DIR/misebin"
cat > "$CASE_DIR/fakemise/mise" <<'MOCK_MISE_CODEX'
#!/usr/bin/env bash
: "${PROBE:?}"
printf 'invoked\n' >>"$PROBE/mise_invoked"
exec codex "$@"
MOCK_MISE_CODEX
chmod +x "$CASE_DIR/fakemise/mise"
ln -s "$CASE_DIR/fakemise/mise" "$CASE_DIR/misebin/codex"
run_with_timeout 5 env -i \
  PATH="$SHIM_CASE:$CASE_DIR/misebin:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  HCOM_LAUNCH_INFLIGHT=1 "$SHIM_CASE/codex" go
rc=$?
assert_eq "mise skip (symlink): exit 0 (no loop)" "$rc" "0"
assert_file_eq "mise skip (symlink): real codex called once" "$PROBE/real_codex_count" "1"
assert_file_missing "mise skip (symlink): mise not dispatched" "$PROBE/mise_invoked"

# 11. A bare Grok resolved to the shim enters only the launch contract. The shim
#     never searches for or invokes a vendor binary itself.
make_case grok_plain
env -i PATH="$SHIM_CASE:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  "$SHIM_CASE/grok" --model grok-4.5 "two words"
rc=$?
assert_eq "grok shim: exit 0" "$rc" "0"
assert_file_eq "grok shim: routes to launch contract" "$PROBE/herder_argv" \
  "$(printf '%s\n' launch grok --model grok-4.5 "two words")"
assert_file_missing "grok shim: vendor binary not invoked" "$PROBE/real_grok_count"

# 12. The pane credential precondition remains fail-closed through the shim
#     with launch's exact cause+remedy refusal; no vendor fallback is attempted.
make_case grok_missing_auth
err="$PROBE/stderr"
env -i PATH="$SHIM_CASE:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  MOCK_HERDER_REFUSE_GROK=1 MOCK_GROK_AUTH_ERROR="$PRODUCT_GROK_REFUSAL" \
  "$SHIM_CASE/grok" hello 2>"$err"
rc=$?
if [[ "$rc" -ne 0 ]]; then
  ok "grok missing auth: nonzero exit"
else
  bad "grok missing auth: nonzero exit" "rc=0"
fi
assert_file_eq "grok missing auth: launch refusal byte-identical to product constant" "$err" "$PRODUCT_GROK_REFUSAL"
if grep -Fq 'login-shell profile such as $HOME/.profile' "$err"; then
  ok "grok missing auth: login-profile remedy named"
else
  bad "grok missing auth: login-profile remedy named" "stderr=$(cat "$err" 2>/dev/null)"
fi
assert_file_missing "grok missing auth: no vendor fallback" "$PROBE/real_grok_count"

# 13. GROK is the explicit one-shot escape hatch. It must be absolute, invoke
#     exactly that vendor binary, and never enter the herder contract.
make_case grok_explicit_bypass
env -i PATH="$SHIM_CASE:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  GROK="$REALBIN/grok" "$SHIM_CASE/grok" --raw-vendor
rc=$?
assert_eq "grok explicit bypass: exit 0" "$rc" "0"
assert_file_eq "grok explicit bypass: vendor invoked once" "$PROBE/real_grok_count" "1"
assert_file_eq "grok explicit bypass: argv preserved" "$PROBE/real_grok_argv" \
  "$(printf '%s\n' --raw-vendor)"
assert_file_missing "grok explicit bypass: launch contract not entered" "$PROBE/herder_argv"

# 14. A relative GROK bypass is ambiguous and must refuse before either herder
#     or a vendor executable is invoked.
make_case grok_bypass_relative
err="$PROBE/stderr"
env -i PATH="$SHIM_CASE:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  GROK="relative/grok" "$SHIM_CASE/grok" --raw-vendor 2>"$err"
rc=$?
if [[ "$rc" -ne 0 ]]; then
  ok "grok relative bypass: nonzero exit"
else
  bad "grok relative bypass: nonzero exit" "rc=0"
fi
if grep -Fq 'must be an absolute executable path' "$err"; then
  ok "grok relative bypass: absolute-path remedy"
else
  bad "grok relative bypass: absolute-path remedy" "stderr=$(cat "$err" 2>/dev/null)"
fi
assert_file_missing "grok relative bypass: launch contract not entered" "$PROBE/herder_argv"
assert_file_missing "grok relative bypass: vendor not invoked" "$PROBE/real_grok_count"

# 15. Pointing GROK at the selected shim itself must refuse instead of execing
#     back into an infinite recursion loop.
make_case grok_bypass_self
err="$PROBE/stderr"
run_with_timeout 5 env -i PATH="$SHIM_CASE:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  GROK="$SHIM_CASE/grok" "$SHIM_CASE/grok" --raw-vendor 2>"$err"
rc=$?
if [[ "$rc" -ne 0 ]]; then
  ok "grok self bypass: nonzero exit"
else
  bad "grok self bypass: nonzero exit" "rc=0"
fi
if grep -Fq 'points to a herder shim, not a vendor binary' "$err"; then
  ok "grok self bypass: recursion refusal"
else
  bad "grok self bypass: recursion refusal" "stderr=$(cat "$err" 2>/dev/null)"
fi
assert_file_missing "grok self bypass: launch contract not entered" "$PROBE/herder_argv"
assert_file_missing "grok self bypass: vendor not invoked" "$PROBE/real_grok_count"

# 16. PATH retains ordinary shadowing semantics: an explicit vendor directory
#     placed before the herder shim wins, so the shim cannot steal an intended
#     raw vendor invocation.
make_case grok_vendor_first
env -i PATH="$REALBIN:$SHIM_CASE:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  /bin/bash -c 'grok --raw-vendor'
rc=$?
assert_eq "grok vendor first: exit 0" "$rc" "0"
assert_file_eq "grok vendor first: vendor invoked once" "$PROBE/real_grok_count" "1"
assert_file_eq "grok vendor first: argv preserved" "$PROBE/real_grok_argv" \
  "$(printf '%s\n' --raw-vendor)"
assert_file_missing "grok vendor first: launch contract not entered" "$PROBE/herder_argv"

# 17. Multiple checkout shim dirs cannot recurse: the selected shim uses only
#     its repository-local herder, and never resolves a sibling as a vendor.
make_case grok_sibling
SIBLING_REPO="$CASE_DIR/sibling"
SIBLING_SHIMS="$SIBLING_REPO/tools/herder/shims"
mkdir -p "$SIBLING_SHIMS"
cp "$SHIMS_DIR/grok" "$SIBLING_SHIMS/grok"
chmod +x "$SIBLING_SHIMS/grok"
run_with_timeout 5 env -i \
  PATH="$SHIM_CASE:$SIBLING_SHIMS:$REALBIN:$PATH_BASE" HOME="$HOME" PROBE="$PROBE" \
  "$SHIM_CASE/grok" --contract
rc=$?
assert_eq "grok sibling: exit 0 (no loop)" "$rc" "0"
assert_file_eq "grok sibling: repo-local launch contract used" "$PROBE/herder_argv" \
  "$(printf '%s\n' launch grok --contract)"
assert_file_missing "grok sibling: vendor binary not invoked" "$PROBE/real_grok_count"

echo
if [[ "$fail" -eq 0 ]]; then
  printf 'ALL GREEN - shim contract holds.\n'
  exit 0
else
  printf 'CONTRACT DRIFT - see failures above.\n'
  exit 1
fi
