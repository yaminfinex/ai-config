#!/usr/bin/env bash
# red-check-slimdown-fixtures.sh — RED BY DESIGN.
#
# This suite asserts the herder slim-down TARGET behavior (charter:
# napkins/herder-slimdown-charter.md, decision 1 — silent self-heal on occupant
# match; refuse only on positive mismatch or no occupant) for three recorded
# field refusal incidents. It is EXPECTED TO FAIL today: each RED fixture
# reproduces an incident's state, runs the real herder binary, and asserts the
# post-slim-down outcome — which the current repair-ladder code answers with
# the recorded refusal instead. The red output prints that refusal verbatim, so
# a red run is itself documentation of the incident. The deletion passes
# (TASK-268 / TASK-041 / TASK-262 re-scopes, deletion map
# napkins/herder-slimdown-deletion-map.md §2 §4 §9) must flip the REDs green
# WITHOUT breaking the GREEN keep-list negatives in the same file (charter
# decision 6: positive-mismatch refuses, no-occupant refuses, multi-match and
# label conflicts fail closed) — the fence is two-sided.
#
# Deliberately NOT wired into the default battery: the battery discovers suites
# by the glob `tools/herder/tests/check-*.sh` (README.md "Gates"), so this file
# carries the `red-` prefix instead of `check-` — the least-surprising exclusion
# mechanism (no runner edits, no skip flags; renaming it to check-*.sh is the
# single act that wires it in once the slim-down lands and it must stay green).
#
# Fixture rows (occupant-probe contract, napkins/occupant-probe-contract.md §4):
#   row  8  incident-268-adopt-circle          (TASK-268)
#   row  9  incident-041-compact-self-location (TASK-041)
#   row 10  incident-262-uncorroborated-bus    (TASK-262)
#
# Probe substrate provided for the coming implementation (contract §3.1/§4):
#   - fake $HOME/.claude/projects/<mungeCwd>/<sid>.jsonl transcript artifacts
#     (mungeCwd = [^A-Za-z0-9] -> "-", sesh correlate_linux.go:349-351)
#   - a synthetic proc root at $CASE/proc (comm/cmdline/cwd-symlink/environ/
#     status per pid), exported as HERDER_PROBE_PROC_ROOT — the proposed
#     ProcRoot injection hook (sesh's c.Root precedent); adjust here if the
#     implementation picks a different hook name
#   - mock `herdr` answering `pane process-info` / `pane process_info` with the
#     occupant claude pid+cwd (check-observer-contract.sh:32-37 precedent). If
#     the probe lands socket-shaped instead of CLI-shaped, steal the socket
#     server from check-observer-contract.sh:110-175.
#
# Usage: bash tools/herder/tests/red-check-slimdown-fixtures.sh
# Exit: 1 while any RED fixture still refuses (expected today) or any GREEN
# keep-list negative breaks; 0 only when reds self-heal AND greens still refuse.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
# Env hygiene (TASK-019): ignore a spawner's binary override; pin this tree.
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"
HERDER=("$REPO_ROOT/bin/herder")

ROOT="$(mktemp -d)"
trap 'rm -rf "$ROOT"' EXIT

# Same wrapper-build hardening as check-spawn-contract.sh: real go toolchain
# ahead of system dirs, wrapper pinned to THIS worktree, run-private hash cache.
GO_TOOLCHAIN_DIR=""
if command -v go >/dev/null 2>&1; then
  GO_TOOLCHAIN_DIR="$(go env GOROOT 2>/dev/null)/bin"
  [[ -x "$GO_TOOLCHAIN_DIR/go" ]] || GO_TOOLCHAIN_DIR=""
fi
GOCACHE_SHARED="${GOCACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/herder/go-build}"
SYS_PATH="/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

# ---------------------------------------------------------------------------
# Lane A mocks — adopt/enroll (incident 268 + label keep-fence)
# ---------------------------------------------------------------------------
MOCKA="$ROOT/bin-adopt"
mkdir -p "$MOCKA"

cat >"$MOCKA/herdr" <<'MOCK_HERDR_A'
#!/usr/bin/env bash
# Replacement pane w9-4 @ term_NEW, occupant claude pid 4242.
set -euo pipefail
CWDVAL="${MOCK_ADOPT_CWD:-/mock/adopt-cwd}"
case "${1:-} ${2:-}" in
  "pane get")
    case "${3:-}" in
      p_new|w9-4)
        jq -n --arg cwd "$CWDVAL" '{result:{pane:{pane_id:"w9-4", terminal_id:"term_NEW", workspace_id:"ws9", cwd:$cwd, foreground_cwd:$cwd}}}';;
      *) jq -n '{result:{}}';;
    esac;;
  "agent list")
    jq -n '{result:{agents:[{pane_id:"w9-4", terminal_id:"term_NEW", agent:"claude", agent_status:"idle"}]}}';;
  "pane list")
    jq -n '{result:{panes:[{pane_id:"w9-4", terminal_id:"term_NEW"}]}}';;
  "pane process-info"|"pane process_info")
    jq -n --arg cwd "$CWDVAL" '{result:{process_info:{pane_id:"w9-4", shell_pid:4000, foreground_processes:[{pid:4242, name:"claude", argv:["claude"], cwd:$cwd}]}}}';;
  "agent rename")
    jq -n '{result:{ok:true}}';;
  *)
    printf 'mock herdr (slimdown-red, adopt lane): unhandled: %s\n' "$*" >&2
    exit 64;;
esac
MOCK_HERDR_A
chmod +x "$MOCKA/herdr"

cat >"$MOCKA/hcom" <<'MOCK_HCOM_A'
#!/usr/bin/env bash
# Stateful roster: starts from $MOCK_HCOM_STATE_FILE (json array); a
# `hcom start --as NAME` reclaim appends a joined row carrying
# $MOCK_HCOM_RECLAIM_SID — the replacement occupant's fresh transcript sid —
# and a launch_context naming the replacement pane (a real `hcom start` from
# inside the pane records its HERDR_PANE_ID), so the adopt bus + completion
# legs can finish hermetically (verified: the HERDER_LABEL temp-override green
# below drives the full composite to rc=0 through these mocks).
set -euo pipefail
STATEF="${MOCK_HCOM_STATE_FILE:?}"
rows() { cat "$STATEF" 2>/dev/null || printf '[]'; }
case "${1:-} ${2:-}" in
  "list --json")
    rows;;
  "list "*)
    jq -e --arg name "${2:-}" 'map(select(.name==$name and (.joined // true))) | length >= 1' <(rows) >/dev/null
    exit;;
  "start --as")
    name="${3:?}"
    rows | jq --arg n "$name" --arg sid "${MOCK_HCOM_RECLAIM_SID:-}" \
      '. + [{name:$n, joined:true, session_id:$sid, launch_context:{pane_id:"w9-4"}}]' >"$STATEF.tmp"
    mv "$STATEF.tmp" "$STATEF"
    printf 'started as %s\n' "$name";;
  *)
    printf 'mock hcom (slimdown-red, adopt lane): unhandled: %s\n' "$*" >&2
    exit 64;;
esac
MOCK_HCOM_A
chmod +x "$MOCKA/hcom"

# ---------------------------------------------------------------------------
# Lane B mocks — compact (incident 041 + positive-mismatch + pane-gone).
# Thin dispatcher over the existing mock-herdr-compact: adds the occupant
# process-info answer and the pane-gone override, delegates everything else.
# ---------------------------------------------------------------------------
MOCKB="$ROOT/bin-compact"
mkdir -p "$MOCKB"

cat >"$MOCKB/herdr" <<'MOCK_HERDR_B'
#!/usr/bin/env bash
set -euo pipefail
if [[ -n "${MOCK_RED_PANE_GONE:-}" && "${1:-} ${2:-}" == "pane get" ]]; then
  jq -n '{result:{}}'
  exit 0
fi
if [[ "${1:-} ${2:-}" == "pane process-info" || "${1:-} ${2:-}" == "pane process_info" ]]; then
  if [[ -n "${MOCK_PROCESS_INFO_NO_SHELL_PID:-}" ]]; then
    jq -n --arg cwd "${MOCK_COMPACT_CWD:-/mock/self-cwd}" \
      '{result:{process_info:{pane_id:"w1-2", foreground_processes:[{pid:4242, name:"claude", argv:["claude"], cwd:$cwd}]}}}'
  else
    jq -n --arg cwd "${MOCK_COMPACT_CWD:-/mock/self-cwd}" \
      '{result:{process_info:{pane_id:"w1-2", shell_pid:4000, foreground_processes:[{pid:4242, name:"claude", argv:["claude"], cwd:$cwd}]}}}'
  fi
  exit 0
fi
exec "@TESTS_DIR@/mock-herdr-compact" "$@"
MOCK_HERDR_B
sed -i "s|@TESTS_DIR@|$TESTS_DIR|" "$MOCKB/herdr"
chmod +x "$MOCKB/herdr"

cat >"$MOCKB/hcom" <<'MOCK_HCOM_B'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-} ${2:-}" == "list --json" ]]; then
  printf '%s\n' "${MOCK_HCOM_ROWS:-[]}"
  exit 0
fi
exit 64
MOCK_HCOM_B
chmod +x "$MOCKB/hcom"
printf '#!/usr/bin/env bash\nexit 0\n' >"$MOCKB/sleep"
chmod +x "$MOCKB/sleep"

# ---------------------------------------------------------------------------
# Lane C mocks — spawn (incident 262 + sender multi-match). Dispatcher over
# mock-herdr-spawn (adds process-info); hcom is mock-hcom-spawn verbatim.
# ---------------------------------------------------------------------------
MOCKC="$ROOT/bin-spawn"
mkdir -p "$MOCKC"

cat >"$MOCKC/herdr" <<'MOCK_HERDR_C'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-} ${2:-}" == "pane process-info" || "${1:-} ${2:-}" == "pane process_info" ]]; then
  jq -n --arg cwd "${MOCK_SPAWNER_CWD:-/mock/cwd}" \
    '{result:{process_info:{pane_id:"p_orch", shell_pid:4000, foreground_processes:[{pid:4242, name:"claude", argv:["claude"], cwd:$cwd}]}}}'
  exit 0
fi
exec "@TESTS_DIR@/mock-herdr-spawn" "$@"
MOCK_HERDR_C
sed -i "s|@TESTS_DIR@|$TESTS_DIR|" "$MOCKC/herdr"
chmod +x "$MOCKC/herdr"
ln -s "$TESTS_DIR/mock-hcom-spawn" "$MOCKC/hcom"
printf '#!/usr/bin/env bash\nexit 0\n' >"$MOCKC/sleep"
chmod +x "$MOCKC/sleep"

PATH_A="$MOCKA${GO_TOOLCHAIN_DIR:+:$GO_TOOLCHAIN_DIR}:$SYS_PATH"
PATH_B="$MOCKB${GO_TOOLCHAIN_DIR:+:$GO_TOOLCHAIN_DIR}:$SYS_PATH"
PATH_C="$MOCKC${GO_TOOLCHAIN_DIR:+:$GO_TOOLCHAIN_DIR}:$SYS_PATH"

# ---------------------------------------------------------------------------
# Probe substrate — transcript artifact + synthetic proc entry (contract §4).
# ---------------------------------------------------------------------------
slug() { printf '%s' "$1" | sed -E 's/[^A-Za-z0-9]/-/g'; }

seed_probe_substrate() {  # <case-dir> <sid> <cwd> <pid> <pane-id>
  local casedir="$1" sid="$2" cwd="$3" pid="$4" paneid="$5"
  local cohort="$casedir/home/.claude/projects/$(slug "$cwd")"
  mkdir -p "$cohort" "$cwd"
  printf '{"type":"user","sessionId":"%s","cwd":"%s"}\n' "$sid" "$cwd" >"$cohort/$sid.jsonl"
  local p="$casedir/proc/$pid"
  mkdir -p "$p/fd" "$p/task/$pid"
  printf 'claude\n' >"$p/comm"
  printf 'claude\0' >"$p/cmdline"
  ln -sfn "$cwd" "$p/cwd"
  printf 'HERDR_PANE_ID=%s\0' "$paneid" >"$p/environ"
  printf 'Name:\tclaude\nPid:\t%s\nPPid:\t1\n' "$pid" >"$p/status"
  # SelfProbe's ancestry fence needs the synthetic caller beneath the tool
  # process. The production path uses os.Getpid; fixtures inject this pid.
  local selfpid=4243 selfp="$casedir/proc/4243"
  mkdir -p "$selfp"
  printf 'Name:\therder\nPid:\t%s\nPPid:\t%s\n' "$selfpid" "$pid" >"$selfp/status"
  printf 'herder\n' >"$selfp/comm"
}

# ---------------------------------------------------------------------------
# Bookkeeping
# ---------------------------------------------------------------------------
red_open=0     # red fixtures still refusing (expected today)
red_green=0    # red fixtures already self-healing (post-deletion state)
green_ok=0
green_bad=0

show_capture() {
  printf -- '--- captured current behavior (rc=%s) ---\n' "$RUN_RC"
  sed 's/^/    /' "$RUN_ERR_F"
  printf -- '--- end capture ---\n'
}

red_verdict() {  # <name> <target-ok 0|1> <incident-marker-regex>
  local name="$1" ok="$2" marker="$3"
  if [[ "$ok" -eq 0 ]]; then
    printf 'GREEN (red fixture self-heals)  %s\n' "$name"
    red_green=$((red_green + 1))
    return
  fi
  printf 'RED   %s — target behavior not yet implemented (expected until the slim-down deletions land)\n' "$name"
  show_capture
  if ! grep -Eq "$marker" "$RUN_ERR_F"; then
    printf 'note: captured refusal no longer matches the recorded incident shape (/%s/) — wording drifted; update napkins/red-fixtures-notes.md\n' "$marker"
  fi
  red_open=$((red_open + 1))
}

green_verdict() {  # <name> <ok 0|1> <detail>
  local name="$1" ok="$2" detail="${3:-}"
  if [[ "$ok" -eq 0 ]]; then
    printf 'PASS  %s\n' "$name"
    green_ok=$((green_ok + 1))
  else
    printf 'FAIL  %s — keep-list fence broken: %s\n' "$name" "$detail"
    show_capture
    green_bad=$((green_bad + 1))
  fi
}

# ===========================================================================
# RED 8 — incident-268-adopt-circle (TASK-268, field-proven 2026-07-17)
#
# Recorded refusal (verbatim shape, fleet escalation): running the exact adopt
# command that adopt/enroll refusals prescribe, from the replacement pane,
# fails in the enroll leg with
#   "label X is held by guid <adoptee> in state unseated (dead/unseated); from
#    the replacement pane run 'herder adopt <adoptee>' ..."
# — the label holder IS the adopt target and the remedy is the refusing
# command. Bites every degraded row whose replacement still carries the
# spawn-time HERDER_LABEL (env label == stored label, the common case).
#
# State: adoptee row unseated holding label "wayfinder" with recorded sid
# sess-old; caller runs in replacement pane p_new (occupant claude, fresh sid
# sess-new, transcript artifact present, old pane vacant); env
# HERDER_LABEL=wayfinder.
#
# TARGET (charter decisions 1+4, deletion map §2/§4): adopt completes silently —
# replacement seated under a fresh guid, label transferred by the take-leg,
# adoptee retired. No label-conflict refusal for a label held by the ADOPT
# TARGET itself.
# ===========================================================================
ROW_NODE='{"kind":"node","event":"node_registered","node_id":"11111111-1111-1111-1111-111111111111","recorded_at":"2026-07-01T00:00:00Z"}'
ROW_ADOPTEE_268='{"kind":"session","guid":"guid-adoptee-0000","event":"unseated","recorded_at":"2026-07-16T00:00:00Z","node":"11111111-1111-1111-1111-111111111111","state":"unseated","label":"wayfinder","role":"worker","tool":"claude","continuity":"confirmed","sids":[{"sid":"sess-old","observed_at":"2026-07-15T00:00:00Z","source":"harvest"}],"provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-old","tag":"worker","cwd":"/old/cwd","workspace_id":"ws9","ts":"2026-07-10T00:00:00Z"}}'

CASE="$ROOT/incident-268-adopt-circle"
mkdir -p "$CASE/home" "$CASE/state"
printf '%s\n%s\n' "$ROW_NODE" "$ROW_ADOPTEE_268" >"$CASE/state/registry.jsonl"
printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
printf '[]' >"$CASE/hcomrows.json"
seed_probe_substrate "$CASE" "sess-new" "$CASE/cwd" 4242 p_new
RUN_ERR_F="$CASE/stderr"
RUN_OUT="$(cd "$CASE/cwd" && env -i \
  PATH="$PATH_A" \
  HOME="$CASE/home" \
  XDG_CACHE_HOME="$ROOT/xdg-cache" \
  GOCACHE="$GOCACHE_SHARED" \
  AI_CONFIG_ROOT="$REPO_ROOT" \
  HERDR_ENV=1 HERDR_PANE_ID=p_new \
  HERDER_LABEL=wayfinder HERDER_ROLE=worker \
  HERDER_STATE_DIR="$CASE/state" \
  HCOM_DIR="$CASE/home/.hcom" \
  MOCK_ADOPT_CWD="$CASE/cwd" \
  MOCK_HCOM_STATE_FILE="$CASE/hcomrows.json" \
  MOCK_HCOM_RECLAIM_SID=sess-new \
  HERDER_PROBE_PROC_ROOT="$CASE/proc" \
  "${HERDER[@]}" adopt guid-adoptee-0000 2>"$RUN_ERR_F")"
RUN_RC=$?

target_ok=1
if [[ "$RUN_RC" -eq 0 ]] \
  && jq -s -e '[.[] | select(.kind=="session" and .state=="seated" and .label=="wayfinder" and .guid!="guid-adoptee-0000")] | length >= 1' "$CASE/state/registry.jsonl" >/dev/null 2>&1 \
  && jq -s -e '[.[] | select(.kind=="session" and .guid=="guid-adoptee-0000")] | last | .state == "retired"' "$CASE/state/registry.jsonl" >/dev/null 2>&1; then
  target_ok=0
fi
red_verdict "incident-268-adopt-circle (adopt completes silently; label transfers via take-leg)" "$target_ok" \
  "is held by guid guid-adoptee-0000 in state unseated.*herder adopt guid-adoptee-0000"

# ---------------------------------------------------------------------------
# GREEN — adopt/enroll keep-fence: a label held by a FOREIGN row (not the
# adopt target) still refuses, in every era (charter decision 6: label
# uniqueness + explicit transfer). Uses plain `herder enroll` so the assertion
# does not depend on how adopt's enroll leg sources its label post-deletion.
# ---------------------------------------------------------------------------
ROW_FOREIGN_LABEL='{"kind":"session","guid":"guid-other-0000","event":"unseated","recorded_at":"2026-07-16T00:00:00Z","node":"11111111-1111-1111-1111-111111111111","state":"unseated","label":"other-agent","role":"worker","tool":"claude","continuity":"confirmed","sids":[{"sid":"sess-other","observed_at":"2026-07-15T00:00:00Z","source":"harvest"}],"provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-other","tag":"worker","cwd":"/old/cwd","workspace_id":"ws9","ts":"2026-07-10T00:00:00Z"}}'

CASE="$ROOT/green-label-foreign-owner"
mkdir -p "$CASE/home" "$CASE/state"
printf '%s\n%s\n' "$ROW_NODE" "$ROW_FOREIGN_LABEL" >"$CASE/state/registry.jsonl"
printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
printf '[]' >"$CASE/hcomrows.json"
seed_probe_substrate "$CASE" "sess-fresh" "$CASE/cwd" 4242 p_new
seeded_lines="$(wc -l <"$CASE/state/registry.jsonl")"
RUN_ERR_F="$CASE/stderr"
RUN_OUT="$(cd "$CASE/cwd" && env -i \
  PATH="$PATH_A" \
  HOME="$CASE/home" \
  XDG_CACHE_HOME="$ROOT/xdg-cache" \
  GOCACHE="$GOCACHE_SHARED" \
  AI_CONFIG_ROOT="$REPO_ROOT" \
  HERDR_ENV=1 HERDR_PANE_ID=p_new \
  HERDER_LABEL=other-agent \
  HERDER_STATE_DIR="$CASE/state" \
  HCOM_DIR="$CASE/home/.hcom" \
  MOCK_ADOPT_CWD="$CASE/cwd" \
  MOCK_HCOM_STATE_FILE="$CASE/hcomrows.json" \
  HERDER_PROBE_PROC_ROOT="$CASE/proc" \
  "${HERDER[@]}" enroll --json 2>"$RUN_ERR_F")"
RUN_RC=$?

ok=1
if [[ "$RUN_RC" -ne 0 ]] \
  && grep -q 'other-agent' "$RUN_ERR_F" \
  && [[ "$(wc -l <"$CASE/state/registry.jsonl")" -eq "$seeded_lines" ]]; then
  ok=0
fi
green_verdict "keep-fence: enroll refuses a label held by a foreign row (no mint, registry untouched)" "$ok" \
  "rc=$RUN_RC lines=$(wc -l <"$CASE/state/registry.jsonl") (seeded $seeded_lines)"

# ---------------------------------------------------------------------------
# GREEN — TASK-268's field-validated workaround, pinned: HERDER_LABEL=<temp>
# on the adopt invocation sidesteps the env-label conflict and the take-label
# leg restores the real label. Passes today AND after the slim-down (once the
# circular fence is deleted the override is simply unnecessary). Doubles as
# the mechanics proof that the FULL adopt composite (enroll -> transfer ->
# retire -> bus reclaim -> seat completion) finishes rc=0 through this suite's
# lane-A mocks — i.e. the RED 268 fixture's target state is mock-reachable.
# ---------------------------------------------------------------------------
CASE="$ROOT/green-adopt-temp-label-workaround"
mkdir -p "$CASE/home" "$CASE/state"
printf '%s\n%s\n' "$ROW_NODE" "$ROW_ADOPTEE_268" >"$CASE/state/registry.jsonl"
printf '11111111-1111-1111-1111-111111111111\n' >"$CASE/state/node_id"
printf '[]' >"$CASE/hcomrows.json"
seed_probe_substrate "$CASE" "sess-new" "$CASE/cwd" 4242 p_new
RUN_ERR_F="$CASE/stderr"
RUN_OUT="$(cd "$CASE/cwd" && env -i \
  PATH="$PATH_A" \
  HOME="$CASE/home" \
  XDG_CACHE_HOME="$ROOT/xdg-cache" \
  GOCACHE="$GOCACHE_SHARED" \
  AI_CONFIG_ROOT="$REPO_ROOT" \
  HERDR_ENV=1 HERDR_PANE_ID=p_new \
  HERDER_LABEL=temp-replacement HERDER_ROLE=worker \
  HERDER_STATE_DIR="$CASE/state" \
  HCOM_DIR="$CASE/home/.hcom" \
  MOCK_ADOPT_CWD="$CASE/cwd" \
  MOCK_HCOM_STATE_FILE="$CASE/hcomrows.json" \
  MOCK_HCOM_RECLAIM_SID=sess-new \
  HERDER_PROBE_PROC_ROOT="$CASE/proc" \
  "${HERDER[@]}" adopt guid-adoptee-0000 2>"$RUN_ERR_F")"
RUN_RC=$?

ok=1
if [[ "$RUN_RC" -eq 0 ]] \
  && jq -s -e '[.[] | select(.kind=="session" and .state=="seated" and .label=="wayfinder" and .guid!="guid-adoptee-0000")] | length >= 1' "$CASE/state/registry.jsonl" >/dev/null 2>&1 \
  && jq -s -e '[.[] | select(.kind=="session" and .guid=="guid-adoptee-0000")] | last | .state == "retired"' "$CASE/state/registry.jsonl" >/dev/null 2>&1; then
  ok=0
fi
green_verdict "workaround: HERDER_LABEL temp-override adopt completes; take-leg restores the stored label" "$ok" \
  "rc=$RUN_RC"

# ===========================================================================
# RED 9 — incident-041-compact-self-location (TASK-041, hits 2/3, 2026-07-08)
#
# Recorded refusal (hera, post-046, verbatim shape):
#   "terminal term_65612408bc9034 not live in herdr agent list"
# and later, improved diagnosis, same disease:
#   "no HERDER_GUID, no session match, no active row for terminal ... Nothing
#    was typed" — a detection-lost-but-alive caller (pane alive and readable,
# agent absent from the agent list) refused with correct registry coordinates.
# Field workaround both times: manual ctrl+u + send-text + Enter into the OWN
# pane — exactly what compact should have done.
#
# State: seated row guid-me-0000 with terminal term_ME / pane w1-2 / recorded
# sid sess-me; env pane p_env resolves live to w1-2@term_ME; the herdr AGENT
# list does NOT contain term_ME (detection lost); occupant claude with cwd and
# transcript artifact proving sess-me == the recorded sid.
#
# TARGET (charter decision 1, deletion map §9): compact proceeds — self =
# probe own pane occupant -> transcript -> sid -> row; pastes /compact into the
# OWN live pane (w1-2) and exits 0. The agent-list detour cannot dead-end it.
# ===========================================================================
ROW_041='{"kind":"session","guid":"guid-me-0000","event":"seated","state":"seated","label":"me","role":"worker","tool":"claude","seat":{"kind":"herdr","terminal_id":"term_ME","pane_id":"w1-2","hcom_name":"me-bus","hcom_verified":true},"sids":[{"sid":"sess-me","observed_at":"2026-07-07T00:00:00Z","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"enroll","spawned_by":"user","tool_session_id":"sess-me","tag":"worker","cwd":"/x","workspace_id":"w1","branch":"main","ts":"2026-07-07T00:00:00Z"}}'
STEER='focus on the open unit, keep gate commands and thread names'

CASE="$ROOT/incident-041-compact-self-location"
mkdir -p "$CASE/home" "$CASE/state" "$CASE/mock" "$CASE/probe"
printf '%s\n' "$ROW_041" >"$CASE/state/registry.jsonl"
seed_probe_substrate "$CASE" "sess-me" "$CASE/cwd" 4242 p_env
RUN_ERR_F="$CASE/stderr"
RUN_OUT="$(cd "$CASE/cwd" && env -i \
  PATH="$PATH_B" \
  HOME="$CASE/home" \
  XDG_CACHE_HOME="$ROOT/xdg-cache" \
  GOCACHE="$GOCACHE_SHARED" \
  AI_CONFIG_ROOT="$REPO_ROOT" \
  HERDR_ENV=1 HERDR_PANE_ID=p_env \
  HERDER_GUID=guid-me-0000 HCOM_SESSION_ID=sess-me \
  HERDER_STATE_DIR="$CASE/state" \
  MOCK_COMPACT_SCENARIO=term_dead MOCK_COMPACT_STATE="$CASE/mock" \
  MOCK_PROBE_DIR="$CASE/probe" MOCK_COMPACT_CWD="$CASE/cwd" \
  MOCK_HCOM_ROWS='[{"name":"me-bus","joined":true,"session_id":"sess-me","launch_context":{}}]' \
  HERDER_PROBE_PROC_ROOT="$CASE/proc" \
  HERDER_PROBE_SELF_PID=4243 \
  "${HERDER[@]}" compact --stop "$STEER" 2>"$RUN_ERR_F")"
RUN_RC=$?

target_ok=1
if [[ "$RUN_RC" -eq 0 ]] \
  && grep -q '^pane send-text w1-2 /compact' "$CASE/probe/calls" 2>/dev/null \
  && grep -q '^pane send-keys w1-2 Enter' "$CASE/probe/calls" 2>/dev/null; then
  target_ok=0
fi
# Marker realigned 2026-08-23 (post ea6e1c0 merge): the pre-merge refusal keyed on
# the stored-terminal fossil ("terminal term_ME is not live in herdr agent list");
# the merged binary self-locates by pid and refuses in the self-probe shape. Same
# incident class — compact cannot prove the caller's own pane — new wording. The
# regex pins the class-specific clause, not a generic refusal.
red_verdict "incident-041-compact-self-location (compact pastes into own live pane despite empty agent list)" "$target_ok" \
  "cannot prove which pane is yours.*no live pane.s process tree contains this process"

# ---------------------------------------------------------------------------
# GREEN — compact keep-fence: POSITIVE MISMATCH still refuses. A stale or
# inherited HERDER_GUID names a row whose seat (and recorded sid sess-parent)
# belongs to a live NEIGHBOUR; the caller's own pane occupant proves an
# UNRECORDED sid (sess-imposter). Nothing may be typed anywhere — today via
# the terminal-disagreement gate, post-deletion via occupant-sid mismatch.
# ---------------------------------------------------------------------------
ROW_PARENT_041='{"kind":"session","guid":"guid-par-0000","event":"seated","state":"seated","label":"parent","role":"orchestrator","tool":"claude","seat":{"kind":"herdr","terminal_id":"term_OTHER","pane_id":"w1-3","hcom_name":"parent-bus","hcom_verified":true},"sids":[{"sid":"sess-parent","observed_at":"2026-07-07T00:00:00Z","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-parent","tag":"orchestrator","cwd":"/x","workspace_id":"w1","branch":"main","ts":"2026-07-07T00:00:00Z"}}'

CASE="$ROOT/green-compact-positive-mismatch"
mkdir -p "$CASE/home" "$CASE/state" "$CASE/mock" "$CASE/probe"
printf '%s\n' "$ROW_PARENT_041" >"$CASE/state/registry.jsonl"
seed_probe_substrate "$CASE" "sess-imposter" "$CASE/cwd" 4242 p_env
RUN_ERR_F="$CASE/stderr"
RUN_OUT="$(cd "$CASE/cwd" && env -i \
  PATH="$PATH_B" \
  HOME="$CASE/home" \
  XDG_CACHE_HOME="$ROOT/xdg-cache" \
  GOCACHE="$GOCACHE_SHARED" \
  AI_CONFIG_ROOT="$REPO_ROOT" \
  HERDR_ENV=1 HERDR_PANE_ID=p_env \
  HERDER_GUID=guid-par-0000 \
  HERDER_STATE_DIR="$CASE/state" \
  MOCK_COMPACT_SCENARIO=midturn MOCK_COMPACT_STATE="$CASE/mock" \
  MOCK_PROBE_DIR="$CASE/probe" MOCK_COMPACT_CWD="$CASE/cwd" \
  MOCK_PROCESS_INFO_NO_SHELL_PID=1 \
  MOCK_HCOM_ROWS='[{"name":"parent-bus","joined":true,"session_id":"sess-parent","launch_context":{"pane_id":"w1-3"}}]' \
  HERDER_PROBE_PROC_ROOT="$CASE/proc" \
  HERDER_PROBE_SELF_PID=4243 \
  "${HERDER[@]}" compact --stop "$STEER" 2>"$RUN_ERR_F")"
RUN_RC=$?

ok=1
if [[ "$RUN_RC" -ne 0 ]] && [[ ! -s "$CASE/probe/calls" ]]; then
  ok=0
fi
green_verdict "keep-fence: compact refuses a foreign-row claim (positive mismatch; nothing typed)" "$ok" \
  "rc=$RUN_RC calls=$(cat "$CASE/probe/calls" 2>/dev/null)"

# ---------------------------------------------------------------------------
# GREEN — compact keep-fence: NO OCCUPANT still refuses. The caller's env pane
# cannot be resolved live at all (pane gone) — today the pane-get refusal,
# post-deletion NO-OCCUPANT(pane_gone). Nothing typed.
# ---------------------------------------------------------------------------
CASE="$ROOT/green-compact-pane-gone"
mkdir -p "$CASE/home" "$CASE/state" "$CASE/mock" "$CASE/probe"
printf '%s\n' "$ROW_041" >"$CASE/state/registry.jsonl"
seed_probe_substrate "$CASE" "sess-me" "$CASE/cwd" 4242 p_env
RUN_ERR_F="$CASE/stderr"
RUN_OUT="$(cd "$CASE/cwd" && env -i \
  PATH="$PATH_B" \
  HOME="$CASE/home" \
  XDG_CACHE_HOME="$ROOT/xdg-cache" \
  GOCACHE="$GOCACHE_SHARED" \
  AI_CONFIG_ROOT="$REPO_ROOT" \
  HERDR_ENV=1 HERDR_PANE_ID=p_env \
  HERDER_GUID=guid-me-0000 HCOM_SESSION_ID=sess-me \
  HERDER_STATE_DIR="$CASE/state" \
  MOCK_RED_PANE_GONE=1 \
  MOCK_COMPACT_SCENARIO=midturn MOCK_COMPACT_STATE="$CASE/mock" \
  MOCK_PROBE_DIR="$CASE/probe" MOCK_COMPACT_CWD="$CASE/cwd" \
  MOCK_HCOM_ROWS='[{"name":"me-bus","joined":true,"session_id":"sess-me","launch_context":{}}]' \
  HERDER_PROBE_PROC_ROOT="$CASE/proc" \
  HERDER_PROBE_SELF_PID=4243 \
  "${HERDER[@]}" compact --stop "$STEER" 2>"$RUN_ERR_F")"
RUN_RC=$?

ok=1
if [[ "$RUN_RC" -ne 0 ]] && [[ ! -s "$CASE/probe/calls" ]]; then
  ok=0
fi
green_verdict "keep-fence: compact refuses when the own pane is gone (no occupant; nothing typed)" "$ok" \
  "rc=$RUN_RC calls=$(cat "$CASE/probe/calls" 2>/dev/null)"

# ===========================================================================
# RED 10 — incident-262-uncorroborated-bus (TASK-262, live outage 2026-07-16)
#
# Recorded refusal (verbatim, peer orchestrator spawn-dead in the field):
#   "herder spawn: refused — initial prompt sender identity is not verified:
#    no joined bus row matches the calling session, process, or pane. Nothing
#    was launched."
# An adopt-recovered orchestrator's healthy registry row rode an hcom row with
# EMPTY launch_context {}, so sender verification had no launch coordinates to
# match, and the follow-up field case left the seat bit hcom_verified=false —
# which also defeats the TASK-262 empty-launch-context fallback (its narrow
# precondition class). Field workaround: env-prefix spawn
# (HERDR_PANE_ID=<pane> HERDER_GUID=<guid> ... promptless, then herder send).
#
# State: seated row guid-adopted-0000 @ term_ORCH/p_orch, hcom_name
# adopted-bus, hcom_verified=false, recorded sid sess-adopted; live roster has
# @adopted-bus joined with launch_context {} (mock-hcom-spawn emptyctx);
# occupant claude in p_orch with transcript artifact proving sess-adopted.
#
# TARGET (charter decision 1, deletion map §9 REPLACE of the sender proof):
# the sender fence runs SelfProbe — own pid -> transcript -> sid -> row — the
# sid matches the row, so spawn proceeds: pane created, prompt delivered from
# @adopted-bus. No env-prefix workaround, no persisted hcom_verified bit
# consulted (verification is per-operation ground truth).
# ===========================================================================
ROW_262='{"kind":"session","guid":"guid-adopted-0000","event":"seated","recorded_at":"2026-07-03T00:00:00Z","state":"seated","label":"dispatcher","role":"orchestrator","tool":"claude","seat":{"kind":"herdr","terminal_id":"term_ORCH","pane_id":"p_orch","hcom_name":"adopted-bus","namespace":"/hcom","hcom_verified":false},"sids":[{"sid":"sess-adopted","observed_at":"2026-07-03T00:00:00Z","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"enroll","spawned_by":"user","tool_session_id":"sess-adopted","tag":"orchestrator","cwd":"/repo","workspace_id":"ws_1","branch":"main","ts":"2026-07-03T00:00:00Z"}}'

CASE="$ROOT/incident-262-uncorroborated-bus"
mkdir -p "$CASE/home" "$CASE/state" "$CASE/mock" "$CASE/probe"
printf '%s\n' "$ROW_262" >"$CASE/state/registry.jsonl"
seed_probe_substrate "$CASE" "sess-adopted" "$CASE/cwd" 4242 p_orch
RUN_ERR_F="$CASE/stderr"
RUN_OUT="$(cd "$CASE/cwd" && env -i \
  PATH="$PATH_C" \
  HOME="$CASE/home" \
  XDG_CACHE_HOME="$ROOT/xdg-cache" \
  GOCACHE="$GOCACHE_SHARED" \
  AI_CONFIG_ROOT="$REPO_ROOT" \
  HERDR_ENV=1 HERDR_PANE_ID=p_orch \
  HERDER_GUID=guid-adopted-0000 \
  HERDER_STATE_DIR="$CASE/state" \
  HERDER_SPAWN_SHELL=/bin/zsh \
  HERDER_SPAWN_BIND_MS=60000 HERDER_SPAWN_VERIFY_MS=1000 \
  MOCK_SPAWN_SCENARIO=ready MOCK_SPAWN_AGENT=claude \
  MOCK_SPAWN_STATE="$CASE/mock" MOCK_PROBE_DIR="$CASE/probe" \
  MOCK_SPAWNER_CWD="$CASE/cwd" MOCK_SPAWNER_BUS=adopted-bus \
  MOCK_HCOM_SPAWN_SCENARIO=emptyctx \
  HERDER_PROBE_PROC_ROOT="$CASE/proc" \
  HERDER_PROBE_SELF_PID=4243 \
  "${HERDER[@]}" spawn --role worker --agent claude --prompt "do the thing" --json 2>"$RUN_ERR_F")"
RUN_RC=$?

target_ok=1
if [[ "$RUN_RC" -eq 0 ]] \
  && [[ -f "$CASE/probe/pane_create_argv" ]] \
  && grep -q -- '--from adopted-bus' "$CASE/probe/send_argv" 2>/dev/null; then
  target_ok=0
fi
red_verdict "incident-262-uncorroborated-bus (spawn sender fence passes via occupant proof; prompt delivered)" "$target_ok" \
  "initial prompt sender identity is not verified.*Nothing was launched"

# ---------------------------------------------------------------------------
# GREEN — spawn keep-fence: MULTI-MATCH fails closed. Two seated rows claim
# the caller's terminal+pane AND both record the same sid (sess-orch) that the
# occupant's transcript proves — genuinely ambiguous in every era. Refuse
# before any pane exists.
# ---------------------------------------------------------------------------
ROW_AMB_A='{"kind":"session","guid":"guid-first-0000","event":"seated","recorded_at":"2026-07-03T00:00:00Z","state":"seated","label":"first","role":"orchestrator","tool":"claude","seat":{"kind":"herdr","terminal_id":"term_ORCH","pane_id":"p_orch","hcom_name":"first-bus","namespace":"/hcom","hcom_verified":true},"sids":[{"sid":"sess-orch","observed_at":"2026-07-03T00:00:00Z","source":"harvest"}],"provenance":{"mechanism":"enroll","spawned_by":"user","tool_session_id":"sess-orch","tag":"orchestrator","cwd":"/repo","workspace_id":"ws_1","branch":"main","ts":"2026-07-03T00:00:00Z"}}'
ROW_AMB_B='{"kind":"session","guid":"guid-second-0000","event":"seated","recorded_at":"2026-07-03T00:00:01Z","state":"seated","label":"second","role":"orchestrator","tool":"claude","seat":{"kind":"herdr","terminal_id":"term_ORCH","pane_id":"p_orch","hcom_name":"second-bus","namespace":"/hcom","hcom_verified":true},"sids":[{"sid":"sess-orch","observed_at":"2026-07-03T00:00:01Z","source":"harvest"}],"provenance":{"mechanism":"enroll","spawned_by":"user","tool_session_id":"sess-orch","tag":"orchestrator","cwd":"/repo","workspace_id":"ws_1","branch":"main","ts":"2026-07-03T00:00:00Z"}}'

CASE="$ROOT/green-spawn-sender-multimatch"
mkdir -p "$CASE/home" "$CASE/state" "$CASE/mock" "$CASE/probe"
printf '%s\n%s\n' "$ROW_AMB_A" "$ROW_AMB_B" >"$CASE/state/registry.jsonl"
seed_probe_substrate "$CASE" "sess-orch" "$CASE/cwd" 4242 p_orch
RUN_ERR_F="$CASE/stderr"
RUN_OUT="$(cd "$CASE/cwd" && env -i \
  PATH="$PATH_C" \
  HOME="$CASE/home" \
  XDG_CACHE_HOME="$ROOT/xdg-cache" \
  GOCACHE="$GOCACHE_SHARED" \
  AI_CONFIG_ROOT="$REPO_ROOT" \
  HERDR_ENV=1 HERDR_PANE_ID=p_orch \
  HERDER_STATE_DIR="$CASE/state" \
  HERDER_SPAWN_SHELL=/bin/zsh \
  HERDER_SPAWN_BIND_MS=60000 HERDER_SPAWN_VERIFY_MS=1000 \
  MOCK_SPAWN_SCENARIO=ready MOCK_SPAWN_AGENT=claude \
  MOCK_SPAWN_STATE="$CASE/mock" MOCK_PROBE_DIR="$CASE/probe" \
  MOCK_SPAWNER_CWD="$CASE/cwd" MOCK_SPAWNER_BUS=first-bus \
  MOCK_HCOM_SPAWN_SCENARIO=emptyctx \
  HERDER_PROBE_PROC_ROOT="$CASE/proc" \
  HERDER_PROBE_SELF_PID=4243 \
  "${HERDER[@]}" spawn --role worker --agent claude --prompt "do the thing" --json 2>"$RUN_ERR_F")"
RUN_RC=$?

ok=1
if [[ "$RUN_RC" -ne 0 ]] && [[ ! -f "$CASE/probe/pane_create_argv" ]]; then
  ok=0
fi
green_verdict "keep-fence: spawn sender fails closed on multi-match (two rows record the occupant sid; no pane created)" "$ok" \
  "rc=$RUN_RC pane_create=$([[ -f "$CASE/probe/pane_create_argv" ]] && echo yes || echo no)"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
printf '\nreds still refusing: %d   reds self-healing: %d   greens holding: %d   greens broken: %d\n' \
  "$red_open" "$red_green" "$green_ok" "$green_bad"
if [[ "$green_bad" -gt 0 ]]; then
  printf 'FENCE BROKEN — a keep-list negative stopped refusing. Fix before shipping any slim-down deletion.\n'
  exit 1
fi
if [[ "$red_open" -gt 0 ]]; then
  printf 'RED AS DESIGNED — %d incident fixture(s) still reproduce their recorded refusals; the slim-down deletion passes must flip them green.\n' "$red_open"
  exit 1
fi
printf 'ALL GREEN — slim-down fence satisfied: incident fixtures self-heal AND keep-list negatives still refuse. Rename this suite to check-*.sh to wire it into the battery.\n'
exit 0
