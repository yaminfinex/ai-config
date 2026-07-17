#!/usr/bin/env bash
# check-cull-busdrop.sh — hermetic guard for herder cull's advisory hcom
# cleanup. It proves culling hcom-bound registry rows runs `hcom kill <name>`
# with the recorded HCOM_DIR, bus-less rows never call hcom, and hcom failure
# never makes cull fail or skip pane closure.
#
# HERDER_CULL_BIN may point at ANY executable honouring the herder cull CLI
# (the bash script or the Go `bin/herder cull` shim); it is exec'd directly,
# not via `bash`, so the same suite gates either implementation.

set -uo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$TESTS_DIR/../../.." && pwd -P)"
# Env hygiene (TASK-019): herder-spawned agents export HERDER_BIN/AI_CONFIG_ROOT
# pointing at the spawner's checkout — honoring them silently drives another
# tree's wrapper/sources. Ignore the binary override; pin the root to THIS tree.
unset HERDER_BIN
export AI_CONFIG_ROOT="$REPO_ROOT"
CULL=("$REPO_ROOT/bin/herder" cull)
[[ -n "${HERDER_CULL_BIN:-}" ]] && CULL=("$HERDER_CULL_BIN")

ROOT="$(mktemp -d)"
MOCKBIN="$ROOT/bin"
mkdir -p "$MOCKBIN"
cleanup() { rm -rf "$ROOT"; }
trap cleanup EXIT

cat > "$MOCKBIN/hcom" <<'MOCK_HCOM'
#!/usr/bin/env bash
set -euo pipefail

: "${MOCK_PROBE_DIR:?}"

case "${1:-}" in
  kill)
    shift
    printf '%s\n' "${HCOM_DIR:-}" >>"$MOCK_PROBE_DIR/hcom_dirs"
    printf '%s\n' "$*" >>"$MOCK_PROBE_DIR/hcom_kill_argv"
    case "${MOCK_HCOM_KILL_FAIL:-0}" in
      1)        printf 'mock kill failed\n' >&2; exit 23;;
      notfound) printf 'instance %s not found\n' "$*" >&2; exit 1;;
    esac
    printf '{"result":{"ok":true}}\n'
    ;;
  *)
    printf 'mock-hcom: unhandled: %s\n' "$*" >&2
    exit 64
    ;;
esac
MOCK_HCOM

cat > "$MOCKBIN/herdr" <<MOCK_HERDR
#!/usr/bin/env bash
set -euo pipefail

MOCK_HERDR="$TESTS_DIR/mock-herdr"
: "\${MOCK_PROBE_DIR:?}"

case "\${1:-} \${2:-}" in
  "agent list")
    if [[ "\${MOCK_CULL_LIVE:-all}" = "none" ]]; then
      jq -n '{result:{agents:[]}}'
    else
      jq -n '{result:{agents:[
        {pane_id:"p_bus", terminal_id:"term_BUS"},
        {pane_id:"p_plain", terminal_id:"term_PLAIN"},
        {pane_id:"p_fail", terminal_id:"term_FAIL"}
      ]}}'
    fi
    ;;
  "pane get")
    case "\${3:-}" in
      p_bus)   jq -n '{result:{pane:{pane_id:"p_bus", terminal_id:"term_BUS"}}}' ;;
      p_plain) jq -n '{result:{pane:{pane_id:"p_plain", terminal_id:"term_PLAIN"}}}' ;;
      p_fail)  jq -n '{result:{pane:{pane_id:"p_fail", terminal_id:"term_FAIL"}}}' ;;
      *)       jq -n '{result:{}}' ;;
    esac
    ;;
  "pane close")
    printf '%s\n' "\${3:-}" >>"\$MOCK_PROBE_DIR/closed_panes"
    if [[ "\${MOCK_CULL_APPEND_ENRICHED:-0}" = "1" ]]; then
      jq -nc --arg dir "\${MOCK_CULL_APPEND_HCOM_DIR:-}" \
        '{kind:"session", guid:"guid-race", event:"registered", recorded_at:"2026-07-08T00:00:01Z",
          state:"seated", label:"race", role:"worker", tool:"claude",
          seat:{kind:"herdr", terminal_id:"term_BUS", pane_id:"p_bus", hcom_name:"bus-race", namespace:\$dir}}' \
        >>"\${HERDER_STATE_DIR:?}/registry.jsonl"
    fi
    jq -n --arg pane "\${3:-}" '{result:{type:"closed", pane_id:\$pane}}'
    ;;
  *)
    exec "\$MOCK_HERDR" "\$@"
    ;;
esac
MOCK_HERDR

chmod +x "$MOCKBIN/hcom" "$MOCKBIN/herdr"
PATH_HERMETIC="$MOCKBIN:/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin"

fail=0
ok()  { printf 'PASS  %s\n' "$1"; }
bad() { printf 'FAIL  %s — %s\n' "$1" "$2"; fail=1; }

line_count() {
  local f="$1"
  if [[ -f "$f" ]]; then
    wc -l <"$f" | tr -d '[:space:]'
  else
    printf '0'
  fi
}

make_case() {
  local name="$1" kind="$2" case_dir reg bus
  case_dir="$ROOT/$name"
  REG_DIR="$case_dir/state"
  PROBE="$case_dir/probe"
  BUS_DIR="$case_dir/bus"
  mkdir -p "$REG_DIR" "$PROBE" "$BUS_DIR"
  reg="$REG_DIR/registry.jsonl"
  bus="$BUS_DIR"

  case "$kind" in
    bus)
      jq -nc --arg dir "$bus" \
        '{kind:"session", guid:"guid-bus", event:"registered", recorded_at:"2026-07-08T00:00:00Z",
          state:"seated", label:"bus", role:"worker", tool:"claude",
          seat:{kind:"herdr", terminal_id:"term_BUS", pane_id:"p_bus", hcom_name:"bus-alpha", namespace:$dir}}' \
        >"$reg"
      ;;
    plain)
      jq -nc \
        '{kind:"session", guid:"guid-plain", event:"registered", recorded_at:"2026-07-08T00:00:00Z",
          state:"seated", label:"plain", role:"worker", tool:"bash",
          seat:{kind:"herdr", terminal_id:"term_PLAIN", pane_id:"p_plain"}}' \
        >"$reg"
      ;;
    failbus)
      jq -nc --arg dir "$bus" \
        '{kind:"session", guid:"guid-fail", event:"registered", recorded_at:"2026-07-08T00:00:00Z",
          state:"seated", label:"fail", role:"worker", tool:"claude",
          seat:{kind:"herdr", terminal_id:"term_FAIL", pane_id:"p_fail", hcom_name:"bus-fail", namespace:$dir}}' \
        >"$reg"
      ;;
    gone)
      {
        jq -nc --arg dir "$bus" \
          '{kind:"session", guid:"guid-gone-bus", event:"registered", recorded_at:"2026-07-08T00:00:00Z",
            state:"seated", label:"gonebus", role:"worker", tool:"claude",
            seat:{kind:"herdr", terminal_id:"term_GONE_BUS", pane_id:"p_gone_bus", hcom_name:"bus-gone", namespace:$dir}}'
        jq -nc \
          '{kind:"session", guid:"guid-gone-plain", event:"registered", recorded_at:"2026-07-08T00:00:00Z",
            state:"seated", label:"goneplain", role:"worker", tool:"bash",
            seat:{kind:"herdr", terminal_id:"term_GONE_PLAIN", pane_id:"p_gone_plain"}}'
      } >"$reg"
      ;;
    *)
      bad "setup: unknown case" "$kind"
      return 1
      ;;
  esac
}

run_cull() {
  local kill_fail="$1" live="$2"; shift 2
  RUN_OUT="$(env -i \
    PATH="$PATH_HERMETIC" HOME="$HOME" \
    HERDR_ENV=1 HERDER_STATE_DIR="$REG_DIR" HERDR_PANE_ID="p_test" \
    MOCK_PROBE_DIR="$PROBE" MOCK_HCOM_KILL_FAIL="$kill_fail" MOCK_CULL_LIVE="$live" \
    MOCK_CULL_APPEND_ENRICHED="${MOCK_CULL_APPEND_ENRICHED:-0}" MOCK_CULL_APPEND_HCOM_DIR="${MOCK_CULL_APPEND_HCOM_DIR:-}" \
    "${CULL[@]}" "$@" 2>&1)"
  RUN_RC=$?
}

# 1. Bus-bound explicit cull drops exactly one bus entry with recorded HCOM_DIR.
make_case bus bus
run_cull 0 all --label bus
[[ "$RUN_RC" -eq 0 ]] && ok "bus cull: exit 0" || bad "bus cull: exit 0" "rc=$RUN_RC out=$RUN_OUT"
[[ "$(line_count "$PROBE/hcom_kill_argv")" = "1" ]] && ok "bus cull: hcom kill once" || bad "bus cull: hcom kill once" "count=$(line_count "$PROBE/hcom_kill_argv")"
[[ "$(cat "$PROBE/hcom_kill_argv" 2>/dev/null)" = "bus-alpha" ]] && ok "bus cull: kill uses hcom_name" || bad "bus cull: kill uses hcom_name" "argv=$(cat "$PROBE/hcom_kill_argv" 2>/dev/null)"
[[ "$(cat "$PROBE/hcom_dirs" 2>/dev/null)" = "$BUS_DIR" ]] && ok "bus cull: HCOM_DIR is recorded hcom_dir" || bad "bus cull: HCOM_DIR" "got=$(cat "$PROBE/hcom_dirs" 2>/dev/null) want=$BUS_DIR"
[[ "$(cat "$PROBE/closed_panes" 2>/dev/null)" = "p_bus" ]] && ok "bus cull: pane closed" || bad "bus cull: pane closed" "closed=$(cat "$PROBE/closed_panes" 2>/dev/null)"
grep -q 'bus: dropped @bus-alpha' <<<"$RUN_OUT" && ok "bus cull: reports drop" || bad "bus cull: reports drop" "out=$RUN_OUT"

# 2. Bus-less explicit cull never invokes hcom.
make_case plain plain
run_cull 0 all --label plain
[[ "$RUN_RC" -eq 0 ]] && ok "plain cull: exit 0" || bad "plain cull: exit 0" "rc=$RUN_RC out=$RUN_OUT"
[[ "$(line_count "$PROBE/hcom_kill_argv")" = "0" ]] && ok "plain cull: no hcom invocation" || bad "plain cull: no hcom invocation" "count=$(line_count "$PROBE/hcom_kill_argv")"
[[ "$(cat "$PROBE/closed_panes" 2>/dev/null)" = "p_plain" ]] && ok "plain cull: pane closed" || bad "plain cull: pane closed" "closed=$(cat "$PROBE/closed_panes" 2>/dev/null)"

# 2b. Sidecar enrichment between cull's initial load and close result is preserved for close + bus drop.
make_case race plain
jq -nc '{kind:"session", guid:"guid-race", event:"registered", recorded_at:"2026-07-08T00:00:00Z",
  state:"seated", label:"race", role:"worker", tool:"claude",
  seat:{kind:"herdr", terminal_id:"term_BUS", pane_id:"p_bus"}}' >"$REG_DIR/registry.jsonl"
MOCK_CULL_APPEND_ENRICHED=1 MOCK_CULL_APPEND_HCOM_DIR="$BUS_DIR" run_cull 0 all --label race
unset MOCK_CULL_APPEND_ENRICHED MOCK_CULL_APPEND_HCOM_DIR
[[ "$RUN_RC" -eq 0 ]] && ok "race cull: exit 0" || bad "race cull: exit 0" "rc=$RUN_RC out=$RUN_OUT"
[[ "$(cat "$PROBE/hcom_kill_argv" 2>/dev/null)" = "bus-race" ]] && ok "race cull: refreshed hcom_name dropped" || bad "race cull: refreshed hcom_name dropped" "argv=$(cat "$PROBE/hcom_kill_argv" 2>/dev/null)"
[[ "$(cat "$PROBE/hcom_dirs" 2>/dev/null)" = "$BUS_DIR" ]] && ok "race cull: refreshed hcom_dir used" || bad "race cull: refreshed hcom_dir used" "got=$(cat "$PROBE/hcom_dirs" 2>/dev/null) want=$BUS_DIR"
tail -n1 "$REG_DIR/registry.jsonl" | jq -e '.kind=="session" and .guid=="guid-race" and .event=="unseated" and .state=="unseated" and .label=="race" and .close_result=="closed" and (.status|not) and (.seat|not)' >/dev/null \
  && ok "race cull: v2 unseated session present" || bad "race cull: v2 unseated session present" "latest=$(tail -n1 "$REG_DIR/registry.jsonl")"
RACE_UNSEATED_VIEW="$(env -i PATH="$PATH_HERMETIC" HOME="$HOME" HERDER_STATE_DIR="$REG_DIR" "$REPO_ROOT/bin/herder" list --all 2>&1)"
grep -q 'race' <<<"$RACE_UNSEATED_VIEW" && ok "race cull: unseated view keeps label" || bad "race cull: unseated view keeps label" "view=$RACE_UNSEATED_VIEW"
grep -q 'p_bus' <<<"$RACE_UNSEATED_VIEW" && bad "race cull: unseated view drops pane" "view=$RACE_UNSEATED_VIEW" || ok "race cull: unseated view drops pane"
grep -q '@bus-race' <<<"$RACE_UNSEATED_VIEW" && bad "race cull: unseated view drops bus" "view=$RACE_UNSEATED_VIEW" || ok "race cull: unseated view drops bus"

# 3. Failed hcom kill is advisory; cull still succeeds and closes the pane.
make_case fail failbus
run_cull 1 all --label fail
[[ "$RUN_RC" -eq 0 ]] && ok "kill failure: cull still exits 0" || bad "kill failure: cull still exits 0" "rc=$RUN_RC out=$RUN_OUT"
[[ "$(cat "$PROBE/closed_panes" 2>/dev/null)" = "p_fail" ]] && ok "kill failure: pane still closed" || bad "kill failure: pane still closed" "closed=$(cat "$PROBE/closed_panes" 2>/dev/null)"
[[ "$(line_count "$PROBE/hcom_kill_argv")" = "1" ]] && ok "kill failure: hcom attempted once" || bad "kill failure: hcom attempted once" "count=$(line_count "$PROBE/hcom_kill_argv")"
grep -q 'bus: drop failed (mock kill failed) — pane closed anyway' <<<"$RUN_OUT" && ok "kill failure: reports advisory failure" || bad "kill failure: reports advisory failure" "out=$RUN_OUT"

# 3b. An already-absent bus row (hcom kill: not found) is the expected
# post-timeout state — softened to a plain note, not an alarming "drop failed".
make_case gonebus failbus
run_cull notfound all --label fail
[[ "$RUN_RC" -eq 0 ]] && ok "already-gone bus: cull exits 0" || bad "already-gone bus: cull exits 0" "rc=$RUN_RC out=$RUN_OUT"
grep -q 'bus: @bus-fail already gone (nothing to drop)' <<<"$RUN_OUT" && ok "already-gone bus: softened note" || bad "already-gone bus: softened note" "out=$RUN_OUT"
grep -q 'drop failed' <<<"$RUN_OUT" && bad "already-gone bus: no drop-failed line" "out=$RUN_OUT" || ok "already-gone bus: no drop-failed line"

# 4. --gone sweep does not turn a one-shot tracker/pane absence into death.
# Without positive evidence, it leaves both registry and bus state untouched.
make_case gone gone
gone_registry_before="$(cat "$REG_DIR/registry.jsonl")"
run_cull 0 none --gone
[[ "$RUN_RC" -eq 0 ]] && ok "gone sweep: exit 0" || bad "gone sweep: exit 0" "rc=$RUN_RC out=$RUN_OUT"
[[ "$(line_count "$PROBE/hcom_kill_argv")" = "0" ]] && ok "gone sweep: observation gap preserves bus row" || bad "gone sweep: observation gap preserves bus row" "count=$(line_count "$PROBE/hcom_kill_argv")"
[[ "$(cat "$REG_DIR/registry.jsonl")" = "$gone_registry_before" ]] && ok "gone sweep: observation gap leaves registry byte-identical" || bad "gone sweep: observation gap leaves registry byte-identical" "out=$RUN_OUT"
[[ "$(grep -c 'cause_class=epoch_uncertain; no automated unseat' <<<"$RUN_OUT")" = "2" ]] && ok "gone sweep: reports both observation gaps" || bad "gone sweep: reports both observation gaps" "out=$RUN_OUT"
[[ "$(line_count "$PROBE/closed_panes")" = "0" ]] && ok "gone sweep: no pane close call" || bad "gone sweep: no pane close call" "closed=$(cat "$PROBE/closed_panes" 2>/dev/null)"

echo
if [[ "$fail" -eq 0 ]]; then
  printf 'ALL GREEN — cull bus-drop contract holds.\n'
  exit 0
else
  printf 'CONTRACT DRIFT — see failures above.\n'
  exit 1
fi
