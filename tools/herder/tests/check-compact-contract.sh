#!/usr/bin/env bash
# check-compact-contract.sh — lock the herder compact contract (TASK-022) with
# committed golden fixtures, plus the grep gates that pin the transport ruling.
#
# herder compact is the ONE ruled exception to bus-only transport (TASK-003
# FINDING 2): input automation on the CALLER'S OWN pane, reusing spawn's
# package-private boot-paste engine to queue a real `/compact <steer>` line
# that fires at turn end. It takes no target and must refuse whenever
# self-identity cannot be proven. This suite drives the REAL herder compact CLI
# against a hermetic mock `herdr` (mock-herdr-compact) and diffs stderr, exit
# code, and the recorded mutating herdr calls (WHICH pane got typed into)
# against goldens/compact/<scenario>.txt.
#
# The grep gates then pin that the paste engine stays unreachable as a general
# transport: no reference to it outside internal/spawncmd, no exported paste
# API, no keystroke verbs in any other package, and no target/pane flag on the
# compact command surface.
#
# Usage:
#   check-compact-contract.sh            # verify current worktree vs goldens
#   check-compact-contract.sh --write    # (re)generate goldens
#   HERDER_COMPACT_BIN=/path/to/herder-compact check-compact-contract.sh [--write]
#
# Determinism: per-case tempdirs normalized to <CASE>, repo paths to <REPO>.
# `sleep` is a no-op on the mock PATH (poll loops advance by iteration).

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
# Env hygiene (TASK-019): herder-spawned agents export HERDER_BIN/AI_CONFIG_ROOT
# pointing at the spawner's checkout — honoring them silently drives another
# tree's wrapper/sources. Ignore the binary override; pin the root to THIS tree.
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"
GOLDENS="$TESTS_DIR/goldens/compact"
HC=("$REPO_ROOT/bin/herder" compact)
[[ -n "${HERDER_COMPACT_BIN:-}" ]] && HC=("$HERDER_COMPACT_BIN")

WRITE=0
[[ "${1:-}" == "--write" ]] && WRITE=1

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN" "$GOLDENS"
trap 'rm -rf "$ROOT"' EXIT

ln -s "$TESTS_DIR/mock-herdr-compact" "$MOCKBIN/herdr"
printf '#!/usr/bin/env bash\nexit 0\n' >"$MOCKBIN/sleep"
chmod +x "$MOCKBIN/sleep"
cat >"$MOCKBIN/hcom" <<'MOCK_HCOM'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-} ${2:-}" == "list --json" ]]; then
  printf '%s\n' "${MOCK_HCOM_ROWS:-[]}"
  exit 0
fi
exit 64
MOCK_HCOM
chmod +x "$MOCKBIN/hcom"

# Separate mock PATH for the detached-sender (`herder compact-then`) scenarios:
# it needs a mock `hcom` (status polling + bus delivery) and no mock herdr.
MOCKBIN_THEN="$ROOT/bin-then"
mkdir -p "$MOCKBIN_THEN"
ln -s "$TESTS_DIR/mock-hcom-then" "$MOCKBIN_THEN/hcom"
printf '#!/usr/bin/env bash\nexit 0\n' >"$MOCKBIN_THEN/sleep"
chmod +x "$MOCKBIN_THEN/sleep"

# Same wrapper-build hardening as check-spawn-contract.sh: real go toolchain
# ahead of system dirs, wrapper pinned to THIS worktree, run-private hash cache.
GO_TOOLCHAIN_DIR=""
if command -v go >/dev/null 2>&1; then
  GO_TOOLCHAIN_DIR="$(go env GOROOT 2>/dev/null)/bin"
  [[ -x "$GO_TOOLCHAIN_DIR/go" ]] || GO_TOOLCHAIN_DIR=""
fi
GOCACHE_SHARED="${GOCACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/herder/go-build}"
PATH_HERMETIC="$MOCKBIN${GO_TOOLCHAIN_DIR:+:$GO_TOOLCHAIN_DIR}:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"
THEN_PATH_HERMETIC="$MOCKBIN_THEN${GO_TOOLCHAIN_DIR:+:$GO_TOOLCHAIN_DIR}:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

fail=0

# Registry rows the scenarios seed (see run_compact's COMPACT_SEED_REGISTRY).
ROW_SELF='{"kind":"session","guid":"guid-me-0000","event":"seated","state":"seated","label":"me","role":"worker","tool":"claude","seat":{"kind":"herdr","terminal_id":"term_ME","pane_id":"w1-2","hcom_name":"me-bus"},"provenance":{"tag":"worker"}}'
ROW_SELF_REG='{"kind":"session","guid":"guid-me-0000","event":"seated","state":"seated","label":"me","role":"worker","tool":"claude","seat":{"kind":"herdr","terminal_id":"term_REG","pane_id":"w1-5","hcom_name":"me-bus"},"provenance":{"tag":"worker"}}'
ROW_SELF_BASH='{"kind":"session","guid":"guid-me-0000","event":"seated","state":"seated","label":"me","role":"worker","tool":"bash","seat":{"kind":"herdr","terminal_id":"term_ME","pane_id":"w1-2"}}'
ROW_SELF_SESS='{"kind":"session","guid":"guid-me-0000","event":"seated","state":"seated","label":"me","role":"worker","tool":"claude","seat":{"kind":"herdr","terminal_id":"term_ME","pane_id":"w1-2","hcom_name":"me-bus"},"provenance":{"mechanism":"enroll","spawned_by":"user","tool_session_id":"sess-me","tag":"worker","cwd":"/x","workspace_id":"w1","branch":"main","ts":"2026-07-07T00:00:00Z"}}'
ROW_SELF_REG_SESS='{"kind":"session","guid":"guid-me-0000","event":"seated","state":"seated","label":"me","role":"worker","tool":"claude","seat":{"kind":"herdr","terminal_id":"term_REG","pane_id":"w1-5","hcom_name":"me-bus"},"provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-me","tag":"worker","cwd":"/x","workspace_id":"w1","branch":"main","ts":"2026-07-07T00:00:00Z"}}'
ROW_PARENT='{"kind":"session","guid":"guid-par-0000","event":"seated","state":"seated","label":"parent","role":"orchestrator","tool":"claude","seat":{"kind":"herdr","terminal_id":"term_OTHER","pane_id":"w1-3","hcom_name":"parent-bus"},"provenance":{"tag":"orchestrator"}}'
ROW_OTHER_SESS='{"kind":"session","guid":"guid-oth-0000","event":"seated","state":"seated","label":"other","role":"worker","tool":"claude","seat":{"kind":"herdr","terminal_id":"term_OTHER","pane_id":"w1-3","hcom_name":"other-bus"},"provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-x","tag":"worker","cwd":"/x","workspace_id":"w1","branch":"main","ts":"2026-07-07T00:00:00Z"}}'

# run_compact <scenario> <env-mode> <args...>
#   env-mode: guid | session | guid_session | parentguid | guid_conflict |
#             positional | positional_badcwd | noguidrow | outside | nopaneid
run_compact() {
  local scen="$1" envmode="$2"; shift 2
  mkdir -p "$CASE/state" "$CASE/mock" "$CASE/probe" "$CASE/cwd"
  [[ -n "${COMPACT_SEED_REGISTRY:-}" ]] && printf '%s\n' "$COMPACT_SEED_REGISTRY" >"$CASE/state/registry.jsonl"
  local guid="" sess="" herdrenv=1 paneid=p_env cwdval="$CASE/cwd"
  local hcom_rows='[{"name":"me-bus","joined":true,"launch_context":{"pane_id":"w1-2"}}]'
  [[ -n "${MOCK_HCOM_ROWS:-}" ]] && hcom_rows="$MOCK_HCOM_ROWS"
  case "$envmode" in
    guid)              guid="guid-me-0000";;
    session)           sess="sess-me";;
    guid_session)      guid="guid-me-0000"; sess="sess-me";;
    parentguid)        guid="guid-par-0000";;
    guid_conflict)     guid="guid-me-0000"; sess="sess-x";;
    positional)        ;;
    positional_badcwd) cwdval="/mock/elsewhere";;
    noguidrow)         guid="guid-ghost-0000";;
    outside)           herdrenv="";;
    nopaneid)          paneid="";;
  esac
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(cd "$CASE/cwd" && env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$ROOT/home" \
    XDG_CACHE_HOME="$ROOT/xdg-cache" \
    GOCACHE="$GOCACHE_SHARED" \
    AI_CONFIG_ROOT="$REPO_ROOT" \
    HERDR_ENV="$herdrenv" HERDR_PANE_ID="$paneid" \
    HERDER_GUID="$guid" HCOM_SESSION_ID="$sess" \
    HERDER_STATE_DIR="$CASE/state" \
    HERDER_COMPACT_THEN_DRYRUN=1 \
    MOCK_HCOM_ROWS="$hcom_rows" \
    MOCK_COMPACT_SCENARIO="$scen" MOCK_COMPACT_STATE="$CASE/mock" \
    MOCK_PROBE_DIR="$CASE/probe" MOCK_COMPACT_CWD="$cwdval" \
    "${HC[@]}" "$@" 2>"$RUN_ERR_F")"
  RUN_RC=$?
}

# run_then_child drives the detached sender directly (`herder compact-then …`)
# against a hermetic mock hcom that flips the caller status active→listening and
# acks the delivery — the "sent shape" the parent's arm would produce live.
# poll/grace/timeout are tiny so the Go loop's internal sleeps stay sub-second.
run_then_child() {
  local scen="$1"; shift
  mkdir -p "$CASE/mock" "$CASE/hcomstate"
  RUN_ERR_F="$CASE/stderr"
  RUN_OUT="$(env -i \
    PATH="$THEN_PATH_HERMETIC" \
    HOME="$ROOT/home" \
    XDG_CACHE_HOME="$ROOT/xdg-cache" \
    GOCACHE="$GOCACHE_SHARED" \
    AI_CONFIG_ROOT="$REPO_ROOT" \
    HERDR_ENV=1 \
    HERDER_LABEL=me \
    HERDER_COMPACT_THEN_POLL_MS=1 \
    HERDER_COMPACT_THEN_GRACE_MS=0 \
    HERDER_COMPACT_THEN_TIMEOUT_MS="${THEN_TIMEOUT_MS:-2000}" \
    MOCK_THEN_SCENARIO="$scen" MOCK_THEN_STATE="$CASE/hcomstate" \
    "$REPO_ROOT/bin/herder" compact-then \
      --name me-bus --message 'continue: run the gate, then report DONE' "$@" 2>"$RUN_ERR_F")"
  RUN_RC=$?
}

block_for() {
  local block
  block="$(printf '=== STDERR ===\n%s\n=== STDOUT ===\n%s\n=== EXIT ===\n%s\n' \
    "$(cat "$RUN_ERR_F")" "$RUN_OUT" "$RUN_RC")"
  block+="$(printf '\n=== HERDR MUTATING CALLS ===\n%s' "$(cat "$CASE/probe/calls" 2>/dev/null)")"
  block="${block//$CASE/<CASE>}"
  block="${block//$REPO_ROOT/<REPO>}"
  printf '%s' "$block"
}

check_one() {
  local name="$1" block gold
  block="$(block_for)"
  gold="$GOLDENS/$name.txt"
  if [[ "$WRITE" -eq 1 ]]; then
    printf '%s\n' "$block" > "$gold"
    printf 'WROTE  %s\n' "$name"
    return
  fi
  if [[ ! -f "$gold" ]]; then
    printf 'MISSING GOLDEN  %s (run --write first)\n' "$name"; fail=1; return
  fi
  if diff -u "$gold" <(printf '%s\n' "$block") >/tmp/hc_diff.$$ 2>&1; then
    printf 'PASS  %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"; cat /tmp/hc_diff.$$; fail=1
  fi
  rm -f /tmp/hc_diff.$$
}

scenario() {  # scenario <name> <mock scen> <env-mode> <args...>
  local name="$1" scen="$2" envmode="$3"; shift 3
  CASE="$ROOT/$name"
  run_compact "$scen" "$envmode" "$@"
  check_one "$name"
}

STEER='focus on the open unit, keep gate commands and thread names'

# Happy paths: mid-turn (composer-empty evidence), honest queued fallback,
# idle transcript echo, session-id identity, positional identity + cwd proof.
COMPACT_SEED_REGISTRY="$ROW_SELF"
scenario stop_delivered      midturn         guid       --stop "$STEER"
scenario queued_fallback     queued_slow     guid       --stop "$STEER"
scenario idle_delivered      idle            guid       --stop "$STEER"
scenario bare_no_steer       midturn         guid
scenario dryrun              midturn         guid       --dry-run "$STEER"
scenario steer_after_ddash   midturn         guid       --stop -- --dry-run is my steer
COMPACT_SEED_REGISTRY="$ROW_SELF_SESS"
scenario session_identity    midturn         session    --stop "$STEER"
COMPACT_SEED_REGISTRY="$ROW_SELF"
scenario positional_ok       midturn         positional --stop "$STEER"
scenario positional_badcwd   midturn         positional_badcwd --stop "$STEER"

# Preflight: visible-only (old scrollback noise must NOT refuse; a live visible
# modal MUST).
scenario scrollback_noise    scrollback_noise guid      --stop "$STEER"
scenario polluted_clear      polluted_clear   guid      --stop "$STEER"
scenario polluted_still      polluted_still   guid      --stop "$STEER"
scenario blocked_modal       blocked          guid      --stop "$STEER"

# Self-pane proof failures.
COMPACT_SEED_REGISTRY=""
scenario refuse_noidentity   midturn         positional "$STEER"
COMPACT_SEED_REGISTRY="$ROW_SELF"
scenario refuse_ghost_guid   midturn         noguidrow  "$STEER"
COMPACT_SEED_REGISTRY="$ROW_SELF_BASH"
scenario refuse_bash         midturn         guid       "$STEER"
COMPACT_SEED_REGISTRY="$ROW_SELF"
scenario refuse_term_dead    term_dead       guid       --stop "$STEER"

# Pane-id churn vs stale identity (codex review P1): a durable key whose
# terminal disagrees with the live env pane REFUSES unless a second self
# signal (session id matching the row) corroborates it — a stale/inherited
# HERDER_GUID is indistinguishable from drift by the guid alone.
COMPACT_SEED_REGISTRY="$ROW_SELF_REG"
scenario guid_drift          guid_drift      guid          --stop "$STEER"
COMPACT_SEED_REGISTRY="$ROW_SELF_REG_SESS"
scenario drift_corroborated  guid_drift      guid_session  --stop "$STEER"
# Stale inherited guid: the row's terminal belongs to a LIVE neighbour pane —
# compact must type NOWHERE (no mutating calls at all).
COMPACT_SEED_REGISTRY="$ROW_PARENT"
scenario stale_guid          midturn         parentguid    --stop "$STEER"
# HERDER_GUID and HCOM_SESSION_ID resolving to different identities: refuse.
COMPACT_SEED_REGISTRY="$ROW_SELF"$'\n'"$ROW_OTHER_SESS"
scenario key_conflict        midturn         guid_conflict "$STEER"
COMPACT_SEED_REGISTRY="$ROW_SELF"

# Codex review P2: payload lands, composer cleared BEFORE the Enter — the
# pre-Enter sample must disarm composer-empty evidence; verify degrades to
# not_delivered (exit 1), never a false delivered.
scenario clear_before_enter  clear_landed    guid          --stop "$STEER"

# Environment/usage refusals.
scenario refuse_outside      midturn         outside    "$STEER"
scenario refuse_nopaneid     midturn         nopaneid   "$STEER"
scenario usage_unknown_flag  midturn         guid       --pane w1-3 "$STEER"
scenario usage_multiline     midturn         guid       $'line one\nline two'

# ---- TASK-034: compact --then (compact-then-continue) ----
# Rows for the --then preconditions: a claude self row with NO bus name (cannot
# deliver a continuation) and a codex self row (--then is claude-only).
ROW_SELF_NOBUS='{"kind":"session","guid":"guid-me-0000","event":"seated","state":"seated","label":"me","role":"worker","tool":"claude","seat":{"kind":"herdr","terminal_id":"term_ME","pane_id":"w1-2"},"provenance":{"tag":"worker"}}'
ROW_SELF_CODEX='{"kind":"session","guid":"guid-me-0000","event":"seated","state":"seated","label":"me","role":"worker","tool":"codex","seat":{"kind":"herdr","terminal_id":"term_ME","pane_id":"w1-2","hcom_name":"me-bus"},"provenance":{"tag":"worker"}}'
ROW_SELF_WRONG_STOPPED='{"kind":"session","guid":"guid-me-0000","event":"seated","state":"seated","label":"me","role":"worker","tool":"claude","seat":{"kind":"herdr","terminal_id":"term_ME","pane_id":"w1-2","hcom_name":"stopped-name"},"provenance":{"tag":"worker"}}'
ROW_SELF_WRONG_LIVE='{"kind":"session","guid":"guid-me-0000","event":"seated","state":"seated","label":"me","role":"worker","tool":"claude","seat":{"kind":"herdr","terminal_id":"term_ME","pane_id":"w1-2","hcom_name":"live-neighbor"},"provenance":{"tag":"worker"}}'
# Field shape: a manual pinned-env re-enroll written by the older repair path,
# then recognised without launch_context or the newer hcom_verified seat bit.
ROW_SELF_REPAIRED='{"kind":"session","guid":"guid-me-0000","event":"recognised","state":"seated","label":"me","role":"worker","tool":"claude","seat":{"kind":"herdr","terminal_id":"term_ME","pane_id":"w1-2","hcom_name":"me-bus","confirmed_at":"2026-07-15T04:00:00Z"},"sids":[{"sid":"sess-me","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"enroll","tool_session_id":"sess-me","tag":"worker"}}'
ROW_SELF_BOUND_UNVERIFIED='{"kind":"session","guid":"guid-me-0000","event":"recognised","state":"seated","label":"me","role":"worker","tool":"claude","seat":{"kind":"herdr","terminal_id":"term_ME","pane_id":"w1-2","hcom_name":"me-bus","hcom_verified":false},"sids":[{"sid":"sess-me","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"enroll","tool_session_id":"sess-me","tag":"worker"}}'
CONT='run the pinned gate, then report DONE on thread unit-w'

# Parent arm/abort shapes (HERDER_COMPACT_THEN_DRYRUN=1 in run_compact keeps the
# arm hermetic — it describes the sender instead of forking one).
COMPACT_SEED_REGISTRY="$ROW_SELF"
scenario then_armed          midturn       guid   "$STEER" --then "$CONT"
scenario then_dryrun         midturn       guid   --dry-run "$STEER" --then "$CONT"
COMPACT_SEED_REGISTRY="$ROW_SELF_REPAIRED"
MOCK_HCOM_ROWS='[{"name":"me-bus","joined":true,"session_id":"sess-me","launch_context":{}}]'
scenario then_dryrun_repaired midturn      guid   --dry-run "$STEER" --then "$CONT"
unset MOCK_HCOM_ROWS
COMPACT_SEED_REGISTRY="$ROW_SELF_BOUND_UNVERIFIED"
MOCK_HCOM_ROWS='[{"name":"me-bus","joined":true,"session_id":"sess-me","launch_context":{}}]'
scenario then_refuse_bound_unverified midturn guid --dry-run "$STEER" --then "$CONT"
unset MOCK_HCOM_ROWS
COMPACT_SEED_REGISTRY="$ROW_SELF"
MOCK_HCOM_ROWS='[{"name":"me-bus","joined":true,"launch_context":{"pane_id":"p_env"}}]'
scenario then_armed_rekeyed_pane midturn    guid   "$STEER" --then "$CONT"
unset MOCK_HCOM_ROWS
# Unverified /compact paste => --then must NOT arm (AC#2 ordering floor).
scenario then_abort_unverified clear_landed guid  "$STEER" --then "$CONT"
scenario then_abort_blocked  blocked       guid   "$STEER" --then "$CONT"
# Preconditions refuse BEFORE anything is typed (no mutating herdr calls).
COMPACT_SEED_REGISTRY="$ROW_SELF_NOBUS"
scenario then_refuse_nobus   midturn       guid   "$STEER" --then "$CONT"
COMPACT_SEED_REGISTRY="$ROW_SELF_WRONG_STOPPED"
scenario then_refuse_stopped_name midturn  guid   "$STEER" --then "$CONT"
COMPACT_SEED_REGISTRY="$ROW_SELF_WRONG_LIVE"
MOCK_HCOM_ROWS='[{"name":"me-bus","joined":true,"launch_context":{"pane_id":"w1-2"}},{"name":"live-neighbor","joined":true,"session_id":"sess-neighbor","launch_context":{"pane_id":"p_other"}}]'
scenario then_refuse_live_neighbor midturn guid   "$STEER" --then "$CONT"
unset MOCK_HCOM_ROWS
COMPACT_SEED_REGISTRY="$ROW_SELF_CODEX"
scenario then_refuse_codex   midturn       guid   "$STEER" --then "$CONT"
scenario codex_bare_refusal  midturn       guid   "$STEER"
COMPACT_SEED_REGISTRY="$ROW_SELF"
# Usage: empty / missing continuation.
scenario then_usage_empty    midturn       guid   "$STEER" --then ""
scenario then_usage_badtimeout midturn     guid   "$STEER" --then "$CONT" --then-timeout nope

# Detached-sender "sent shapes": drive `herder compact-then` directly against a
# mock hcom that ends the turn and acks (sent), leaves the target busy (queued),
# or never ends the turn (timeout — must give up loudly, never deliver).
then_child_scenario() {  # then_child_scenario <name> <mock scen> <extra args...>
  local name="$1" scen="$2"; shift 2
  CASE="$ROOT/$name"
  run_then_child "$scen" "$@"
  check_one "$name"
}
then_child_scenario then_sent       sent
then_child_scenario then_queued     queued_busy
# Armed-late: "active" never sampled → turn end PROVEN via the hcom event history
# (proof (b)), then delivered. A naked sampled "listening" must NOT be enough.
then_child_scenario then_armed_late armed_late
# Independent fallback: live status stays unknown, but a strict post-arm event
# under the queried identity proves turn end and the continuation is sent once.
then_child_scenario then_unknown_status_event unknown_event
THEN_TIMEOUT_MS=50 then_child_scenario then_timeout stuck
# Fail-open guard (codex review P1 residual): the arm-time event snapshot FAILS
# → proof (b) DISABLED; a naked "listening" (no observed transition) must fail
# closed and deliver nothing, never trust a possibly-pre-arm event.
THEN_TIMEOUT_MS=50 then_child_scenario then_snapshot_fail snap_fail

# ---- grep gates: the ruled exception stays a ruled exception ----
if [[ "$WRITE" -eq 0 ]]; then
  ok()  { printf 'PASS  %s\n' "$1"; }
  bad() { printf 'FAIL  %s\n%s\n' "$1" "$2"; fail=1; }

  GO_SRC="$REPO_ROOT/tools/herder"

  # 1. The paste engine is referenced nowhere outside internal/spawncmd.
  hits="$(grep -rn "bootPaster" "$GO_SRC" --include='*.go' | grep -v "internal/spawncmd/" || true)"
  [[ -z "$hits" ]] && ok "grep-gate: bootPaster confined to internal/spawncmd" \
    || bad "grep-gate: bootPaster confined to internal/spawncmd" "$hits"

  # 2. No exported identifier in bootpaste.go — no exported paste API exists.
  hits="$(grep -En '^func [A-Z]|^type [A-Z]|^var [A-Z]|^const [A-Z]' "$GO_SRC/internal/spawncmd/bootpaste.go" || true)"
  [[ -z "$hits" ]] && ok "grep-gate: bootpaste.go exports nothing" \
    || bad "grep-gate: bootpaste.go exports nothing" "$hits"

  # 3. The keystroke verbs (herdr agent send / pane send-keys) appear in no Go
  #    package other than spawncmd — nobody quietly rebuilt a transport.
  hits="$(grep -rn '"send-keys"\|"agent", "send"' "$GO_SRC/internal" --include='*.go' | grep -v "internal/spawncmd/" || true)"
  [[ -z "$hits" ]] && ok "grep-gate: keystroke verbs confined to internal/spawncmd" \
    || bad "grep-gate: keystroke verbs confined to internal/spawncmd" "$hits"

  # 4. The compact surface has no target/pane addressing flag.
  hits="$(grep -En -- '--pane|--target|--to\b' "$GO_SRC/internal/spawncmd/compact.go" || true)"
  [[ -z "$hits" ]] && ok "grep-gate: compact has no target/pane flag" \
    || bad "grep-gate: compact has no target/pane flag" "$hits"
fi

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "${HC[*]}"; exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — compact contract matches goldens.\n'; exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'; exit 1
fi
