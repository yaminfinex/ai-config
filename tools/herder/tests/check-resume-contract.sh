#!/usr/bin/env bash
# check-resume-contract.sh — lock the herder resume lifecycle contract.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$TESTS_DIR/../../.." && pwd -P)"
# Env hygiene (TASK-019): herder-spawned agents export HERDER_BIN/AI_CONFIG_ROOT
# pointing at the spawner's checkout — honoring them silently drives another
# tree's wrapper/sources. Ignore the binary override; pin the root to THIS tree.
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO"
GOLDENS="$TESTS_DIR/goldens/resume"
HRS=("$REPO/bin/herder" resume)
[[ -n "${HERDER_RESUME_BIN:-}" ]] && HRS=("$HERDER_RESUME_BIN")

WRITE=0
[[ "${1:-}" == "--write" ]] && WRITE=1

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN" "$GOLDENS"
trap 'rm -rf "$ROOT"' EXIT

cat >"$MOCKBIN/herdr" <<'MOCK_HERDR'
#!/usr/bin/env bash
set -euo pipefail
PROBE="${MOCK_PROBE_DIR:?}"
mkdir -p "$PROBE"
case "${1:-} ${2:-}" in
	"workspace list")
		jq -n '{result:{workspaces:[{workspace_id:"ws_1"},{workspace_id:"ws_target"}]}}';;
	"agent list")
    if [[ "${MOCK_LIVE_TARGET:-0}" == "1" ]]; then
      jq -n '{result:{agents:[{terminal_id:"term_ACTIVE", pane_id:"p_active", name:"active", agent_status:"idle"}]}}'
    else
      jq -n '{result:{agents:[]}}'
    fi;;
  "agent start")
    printf '%s\n' "$*" >>"$PROBE/herdr_start_argv"
		ws="ws_resumed"; prev=""
		for arg in "$@"; do [[ "$prev" == "--workspace" ]] && ws="$arg"; prev="$arg"; done
		printf '%s' "$ws" >"$PROBE/agent_workspace"
    # TASK-017: stand in for the sidecar's registry bind — a beat after the
    # pane starts, append an enrichment row carrying the new bus name so the
    # resume addendum poll finds it (real sidecars bind seconds after boot).
    if [[ -n "${MOCK_BIND_NAME:-}" ]]; then
      guid="$(sed -n 's/.*HERDER_GUID=\([^ ]*\).*/\1/p' <<<"$*")"
      ( sleep 1
        printf '{"guid":"%s","short_guid":"codex","label":"codex-me","role":"worker","agent":"codex","pane_id":"p_resumed","terminal_id":"term_RESUMED","workspace_id":"ws_resumed","cwd":"/mock/cwd","hcom_dir":"/hcom","hcom_name":"%s","hcom_tag":"worker","status":"active","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-codex","tag":"worker","batch_id":"","cwd":"/repo","workspace_id":"ws_resumed","branch":"feat/herder-go-port","ts":"2026-07-03T00:02:00Z"}}\n' \
          "$guid" "$MOCK_BIND_NAME" >>"${HERDER_STATE_DIR:?}/registry.jsonl"
      ) >/dev/null 2>&1 &
    fi
		jq -n --arg ws "$ws" '{result:{agent:{pane_id:"p_resumed", terminal_id:"term_RESUMED", workspace_id:$ws, cwd:"/mock/cwd"}}}';;
	"pane move")
		printf '%s\n' "$*" >>"$PROBE/herdr_move_argv"
		jq -n '{result:{type:"pane_moved"}}';;
	"pane get")
		if [[ "${3:-}" == "p_self" ]]; then
			jq -n '{result:{pane:{pane_id:"p_self",terminal_id:"term_SELF",workspace_id:"ws_self",cwd:"/mock/cwd"}}}'
		else
			jq -n --arg ws "$(cat "$PROBE/agent_workspace" 2>/dev/null || printf ws_resumed)" '{result:{pane:{pane_id:"p_resumed",terminal_id:"term_RESUMED",workspace_id:$ws,tab_id:"tab_new",cwd:"/mock/cwd"}}}'
		fi;;
  *)
    printf 'mock herdr (resume suite): unhandled: %s\n' "$*" >&2
    exit 64;;
esac
MOCK_HERDR
chmod +x "$MOCKBIN/herdr"

# TASK-017: the addendum send rides the real bus engine — record what it sends
# and ack the delivery receipt so verify=delivered without a poll stall. Live
# receipt shape (TASK-032): JSONL with a monotone `id`; the engine snapshots
# the newest id pre-send and requires a STRICTLY newer one, so the mock's id
# grows per call (snapshot sees N, the first poll sees N+1 ⇒ delivered).
cat >"$MOCKBIN/hcom" <<'MOCK_HCOM'
#!/usr/bin/env bash
if [[ "${1:-} ${2:-}" == "list --json" ]]; then
  identity="${MOCK_HCOM_IDENTITY:-}"
  if [[ -z "$identity" ]]; then
    identity='[{"name":"resume-child","joined":true,"session_id":"sess-resume","launch_context":{"pane_id":"p_resumed"}}]'
  fi
  printf '%s\n' "$identity"
  exit 0
fi
case "${1:-}" in
  send)   printf '%s\n' "$*" >>"${MOCK_PROBE_DIR:?}/hcom_send_argv" ;;
  events) n=$(( $(cat "${MOCK_PROBE_DIR:?}/events_calls" 2>/dev/null || echo 0) + 1 ))
          printf '%s' "$n" >"${MOCK_PROBE_DIR:?}/events_calls"
          printf '{"id":%s,"data":{"context":"deliver:sender"},"type":"status"}\n' "$n" ;;
esac
exit 0
MOCK_HCOM
chmod +x "$MOCKBIN/hcom"

PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"
fail=0

seed_registry() {
  mkdir -p "$CASE/state"
  cat >"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-resume-0000","short_guid":"resume","label":"resume-me","role":"worker","agent":"claude","terminal_id":"term_OLD","pane_id":"p_old","hcom_dir":"/hcom","hcom_name":"resume-rive","hcom_tag":"worker","status":"active","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-resume","tag":"worker","batch_id":"","cwd":"/repo","workspace_id":"ws_1","branch":"feat/herder-go-port","ts":"2026-07-03T00:00:00Z"}}
{"guid":"guid-resume-0000","short_guid":"resume","label":"resume-me","role":"worker","agent":"claude","terminal_id":"term_OLD","pane_id":"p_old","hcom_dir":"/hcom","hcom_name":"resume-rive","hcom_tag":"worker","status":"closed","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"","tag":"worker","batch_id":"","cwd":"/repo","workspace_id":"ws_1","branch":"feat/herder-go-port","ts":"2026-07-03T00:01:00Z"}}
{"kind":"session","guid":"guid-active-0000","event":"seated","recorded_at":"2026-07-03T00:00:00Z","state":"seated","label":"active-live","role":"worker","tool":"claude","seat":{"kind":"herdr","terminal_id":"term_ACTIVE","pane_id":"p_active","hcom_name":"active-rive","namespace":"/hcom"},"provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-active","tag":"worker","cwd":"/repo","workspace_id":"ws_1","branch":"feat/herder-go-port","ts":"2026-07-03T00:00:00Z"}}
{"guid":"guid-nosess-0000","short_guid":"nosess","label":"no-session","role":"worker","agent":"codex","terminal_id":"term_NOSESS","pane_id":"p_nosess","hcom_dir":"/hcom","hcom_name":"","hcom_tag":"worker","status":"closed","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"","tag":"worker","batch_id":"","cwd":"/repo","workspace_id":"ws_1","branch":"feat/herder-go-port","ts":"2026-07-03T00:00:00Z"}}
{"guid":"guid-collide-0000","short_guid":"collide","label":"taken","role":"worker","agent":"claude","terminal_id":"term_COLLIDE","pane_id":"p_collide","hcom_dir":"/hcom","hcom_name":"collide-rive","hcom_tag":"worker","status":"closed","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-collide","tag":"worker","batch_id":"","cwd":"/repo","workspace_id":"ws_1","branch":"feat/herder-go-port","ts":"2026-07-03T00:00:00Z"}}
{"guid":"guid-other-0000","short_guid":"other","label":"taken","role":"worker","agent":"claude","terminal_id":"term_OTHER","pane_id":"p_other","status":"active"}
JSONL
  if [[ -n "${SEED_DORMANT:-}" ]]; then
    cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-dormant-0000","short_guid":"dormant","label":"dormant-live-terminal","role":"worker","agent":"claude","terminal_id":"term_ACTIVE","pane_id":"p_dormant","hcom_dir":"/hcom","hcom_name":"dormant-bus","hcom_tag":"worker","status":"active","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-dormant","tag":"worker","cwd":"/repo","workspace_id":"ws_1","branch":"feat/herder-go-port","ts":"2026-07-03T00:00:00Z"}}
JSONL
  fi
  # TASK-017: resumable codex row, seeded only for the codex addendum cases so
  # the pre-existing goldens' REGISTRY sections stay byte-identical.
  if [[ -n "${SEED_CODEX:-}" ]]; then
    cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"guid":"guid-codex-0000","short_guid":"codex","label":"codex-me","role":"worker","agent":"codex","terminal_id":"term_CODEX","pane_id":"p_codex","hcom_dir":"/hcom","hcom_name":"codex-vibe","hcom_tag":"worker","status":"closed","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-codex","tag":"worker","batch_id":"","cwd":"/repo","workspace_id":"ws_1","branch":"feat/herder-go-port","ts":"2026-07-03T00:00:00Z"}}
JSONL
  fi
	if [[ -n "${SEED_SENDER:-}" ]]; then
		cat >>"$CASE/state/registry.jsonl" <<'JSONL'
{"kind":"session","guid":"guid-sender-0000","event":"seated","recorded_at":"2026-07-03T00:00:00Z","state":"seated","label":"sender","role":"dispatcher","tool":"codex","seat":{"kind":"herdr","terminal_id":"term_SELF","pane_id":"p_self","hcom_name":"dispatcher-rive","namespace":"/hcom"},"provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-sender","tag":"dispatcher","cwd":"/repo","workspace_id":"ws_self","branch":"feat/herder-go-port","ts":"2026-07-03T00:00:00Z"}}
JSONL
	fi
	# Recorded cwd values are real directories so the preflight tests the normal
	# launch path. Individual refusal cases replace one with a missing path.
	sed -i "s#\"cwd\":\"/repo\"#\"cwd\":\"$REPO\"#g" "$CASE/state/registry.jsonl"
}

run_case() {
  local name="$1" live="$2"; shift 2
  CASE="$ROOT/$name"
  mkdir -p "$CASE/home" "$CASE/probe"
  seed_registry
  RUN_ERR_F="$CASE/stderr"
  # Pin the runner cwd to $REPO so resume's os.Getwd()-derived child cwd is a
  # stable fixture value regardless of where this suite is invoked from.
  RUN_OUT="$(cd "$REPO" && env -i \
    PATH="$PATH_HERMETIC" \
    HOME="$CASE/home" \
    AI_CONFIG_ROOT="$REPO" \
    HERDR_ENV=1 HERDR_PANE_ID=p_self \
    HERDER_STATE_DIR="$CASE/state" \
    HERDER_LIFECYCLE_SETTLE_MS=0 \
    HERDER_LIFECYCLE_FOCUS="${HERDER_LIFECYCLE_FOCUS:-}" \
    HERDER_ADDENDUM_SETTLE_MS="${HERDER_ADDENDUM_SETTLE_MS:-10000}" \
    MOCK_PROBE_DIR="$CASE/probe" \
    MOCK_LIVE_TARGET="$live" \
    MOCK_BIND_NAME="${MOCK_BIND_NAME:-}" \
    MOCK_HCOM_IDENTITY="${MOCK_HCOM_IDENTITY:-}" \
    "${HRS[@]}" "$@" 2>"$RUN_ERR_F")"
  RUN_RC=$?
}

block_for() {
  local block
block="$(printf '=== STDERR ===\n%s\n=== STDOUT ===\n%s\n=== EXIT ===\n%s\n=== HERDR START ARGV ===\n%s\n=== HERDR MOVE ARGV ===\n%s\n=== REGISTRY ===\n%s' \
    "$(cat "$RUN_ERR_F")" "$RUN_OUT" "$RUN_RC" \
    "$(cat "$CASE/probe/herdr_start_argv" 2>/dev/null)" \
    "$(cat "$CASE/probe/herdr_move_argv" 2>/dev/null)" \
    "$(cat "$CASE/state/registry.jsonl" 2>/dev/null)")"
  # TASK-017: codex cases capture the addendum send verbatim (pins doctrine
  # content at the delivery surface); section absent on non-codex cases so
  # their goldens stay byte-identical.
  if [[ -f "$CASE/probe/hcom_send_argv" ]]; then
    block+="$(printf '\n=== HCOM SEND ARGV ===\n%s' "$(cat "$CASE/probe/hcom_send_argv")")"
  fi
  block="${block//$REPO/<REPO>}"
  block="$(sed -E 's/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/<GUID>/g; s/"hostname":"[^"]*"/"hostname":"<HOST>"/g' <<<"$block")"
  block="$(sed -E 's/[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z/<TS>/g' <<<"$block")"
  printf '%s' "$block"
}

check_one() {
  local name="$1" block gold
  block="$(block_for)"
  gold="$GOLDENS/$name.txt"
  if [[ "$WRITE" -eq 1 ]]; then
    printf '%s\n' "$block" >"$gold"
    printf 'WROTE  %s\n' "$name"
    return
  fi
  if [[ ! -f "$gold" ]]; then
    printf 'MISSING GOLDEN  %s (run --write first)\n' "$name"; fail=1; return
  fi
  if diff -u "$gold" <(printf '%s\n' "$block") >/tmp/hrs_diff.$$ 2>&1; then
    printf 'PASS  %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"; cat /tmp/hrs_diff.$$; fail=1
  fi
  rm -f /tmp/hrs_diff.$$
}

run_case happy 0 resume --json
check_one happy
HERDER_LIFECYCLE_FOCUS=--focus run_case explicit_focus 0 resume --json
check_one explicit_focus
run_case closed_row_full_guid 0 guid-resume-0000 --json
check_one closed_row_full_guid
run_case explicit_split 0 resume --split down --json
check_one explicit_split
run_case explicit_workspace 0 resume --workspace ws_target --json
check_one explicit_workspace
run_case refuse_live 1 active
check_one refuse_live
SEED_DORMANT=1 run_case dormant_live_terminal 1 dormant --json
check_one dormant_live_terminal
run_case label_collision 0 collide
check_one label_collision
run_case unknown 0 nope
check_one unknown
run_case missing_session 0 nosess
check_one missing_session
# TASK-017: resumed codex sessions lose the launch-seam addendum (hcom strips
# user developer_instructions on resume/fork), so resume re-delivers it over
# the bus once the sidecar binds the new instance's bus name in the registry.
SEED_CODEX=1 SEED_SENDER=1 MOCK_BIND_NAME=codex-vibe \
  MOCK_HCOM_IDENTITY='[{"name":"dispatcher-rive","joined":true,"launch_context":{"pane_id":"p_self"}},{"name":"codex-vibe","joined":true,"session_id":"sess-codex","launch_context":{"pane_id":"p_resumed"}}]' \
  run_case codex_addendum 0 codex --json
check_one codex_addendum
# No bind inside the window -> canonical completion refuses the registry row
# while preserving the live child for sidecar or in-seat enrollment recovery.
SEED_CODEX=1 HERDER_ADDENDUM_SETTLE_MS=1 MOCK_HCOM_IDENTITY='[]' \
  run_case codex_addendum_timeout 0 codex
check_one codex_addendum_timeout

# A deleted recorded checkout is rejected before any pane starts. An explicit
# replacement cwd is the supported recovery path.
CASE="$ROOT/missing_cwd"
mkdir -p "$CASE/home" "$CASE/probe"
seed_registry
sed -i "s#\"cwd\":\"$REPO\"#\"cwd\":\"$CASE/missing-worktree\"#g" "$CASE/state/registry.jsonl"
RUN_ERR_F="$CASE/stderr"
RUN_OUT="$(cd "$REPO" && env -i PATH="$PATH_HERMETIC" HOME="$CASE/home" AI_CONFIG_ROOT="$REPO" HERDR_ENV=1 HERDR_PANE_ID=p_self HERDER_STATE_DIR="$CASE/state" HERDER_LIFECYCLE_SETTLE_MS=0 HERDER_ADDENDUM_SETTLE_MS=0 MOCK_PROBE_DIR="$CASE/probe" MOCK_LIVE_TARGET=0 "$REPO/bin/herder" resume resume 2>"$RUN_ERR_F")"
RUN_RC=$?
if [[ "$RUN_RC" -ne 0 ]] && grep -q '\[cwd_unavailable\]' "$RUN_ERR_F" && grep -q -- '--cwd' "$RUN_ERR_F" && [[ ! -f "$CASE/probe/herdr_start_argv" ]]; then
	printf 'PASS  missing cwd refuses before launch\n'
else
	printf 'FAIL  missing cwd refusal — rc=%s err=%s\n' "$RUN_RC" "$(cat "$RUN_ERR_F")"
	fail=1
fi

if [[ "$WRITE" -eq 1 ]]; then
  printf '\nGoldens written from: %s\n' "${HRS[*]}"; exit 0
fi
if [[ "$fail" -eq 0 ]]; then
  printf '\nALL GREEN — resume contract matches goldens.\n'; exit 0
else
  printf '\nCONTRACT DRIFT — see diffs above.\n'; exit 1
fi
