---
id: TASK-297
title: >-
  herder spawner broken by herdr 0.7.5 agent-start re-architecture — adapt to
  tab-create/pane-split + pane-run
status: To Do
assignee: []
created_date: '2026-07-28 00:15'
labels:
  - herder-herdr-drift
dependencies: []
references:
  - 'tools/herder/internal/lifecyclecmd/lifecycle.go:849'
modified_files:
  - tools/herder/internal/lifecyclecmd/lifecycle.go
  - tools/herder/internal/herdrcli/herdrcli.go
ordinal: 296500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
herdr auto-updated to 0.7.5 on the stable channel (single forward-only ~/.local/bin/herdr binary, rebuilt 2026-07-27 20:17; no cached 0.7.4, so rollback is impractical). 0.7.5 RE-ARCHITECTED `herdr agent start`: it no longer creates a pane. It is now `herdr agent start <name> --kind <kind> --pane <ID>` — it starts a supported agent KIND in an EXISTING pane, and no longer accepts --split/--cwd/--workspace or a trailing `-- bash -lic <cmd>`.

herder's spawner issues the OLD form at lifecyclecmd/lifecycle.go:849 (the single spawn/fork/resume launch call; nothing else in herder calls agent start):
  herdr agent start <label> --split <dir> [--workspace <ws>] --cwd <p> -- bash -lic <innerCmd>
so every spawn/fork/resume fails.

0.7.5 splits pane-creation from agent-start. New primitives (all tested live this session):
- NEW TAB (the common path, per owner): `herdr tab create --workspace <ws> --cwd <p> --label <t> [--env K=V] --no-focus` -> returns result.root_pane.{pane_id,terminal_id,workspace_id,cwd,tab_id} + result.tab. One call replaces old `agent start --split --workspace --cwd` PLUS the follow-up `pane move --new-tab`.
- SPLIT (less common): `herdr pane split <base> --direction <right|down> --cwd <p> [--env K=V]` -> returns result.pane.{...}. NOTE: key is 'pane', not 'root_pane'. pane split has NO --workspace.
- RUN the wrapper: `herdr pane run <pane> bash -lic '<innerCmd>'` (exit 0; preserves the login-shell env/PATH/mise strategy unchanged).
- REGISTER the agent: `herdr pane report-agent <pane> --source <id> --agent <label> --state working` and `herdr pane report-agent-session <pane> --source <id> --agent <label> --agent-session-id <sid>`.

VALIDATED end-to-end: tab create -> pane run 'exec sleep' -> report-agent(-session) -> `herdr agent get <pane>` returns agent_status=working, fully tracked (pane/terminal/workspace/tab all present).

WHY CONTAINED: herder ALREADY owns agent registration — sidecarcmd/sidecar.go:1146 already calls report-agent-session and pane.report_agent. So 0.7.5 broke only the pane-CREATION half; the run half and the reporting half are intact. Blast radius: lifecyclecmd/lifecycle.go (startArgs + newTabMoveArgs region; new-tab no longer needs a separate pane move) and herdrcli parsing (AgentStart -> parse root_pane/pane pane_info; ParsePaneGet already handles that shape), plus the test battery. Env/identity/reporting model untouched.

GOTCHAS: (1) JSON shape split — tab create=result.root_pane, pane split=result.pane, both downstream into one pane_info. (2) Workspace: tab create takes --workspace directly; pane split does not (needs a follow-up pane move --new-workspace). Favor tab create for the common path. (3) Spawner should emit one report-agent --state working right after pane run so the seat shows live immediately instead of waiting for the sidecar's first tick. (4) AUTO-UPDATE will re-break this: 0.7.5 came via stable at 20:17, forward-only, no pin — track/pin herdr or expect churn (prior art: TASK-293 herdr JSON skew).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 spawn/fork/resume create panes via 'herdr tab create' (new-tab, default) or 'herdr pane split' (split direction), never 'agent start --split'
- [ ] #2 new-tab path is a single 'tab create --workspace --cwd --label' call with NO separate 'pane move --new-tab'
- [ ] #3 login-shell wrapper runs via 'herdr pane run <pane> bash -lic <innerCmd>' with env/PATH strategy unchanged
- [ ] #4 spawner emits an initial 'pane report-agent --state working' after pane run so the seat shows live immediately
- [ ] #5 herdrcli parses tab-create result.root_pane and pane-split result.pane into pane_info; parseAgentStart callsite updated
- [ ] #6 workspace placement preserved (tab create --workspace; split path falls back to pane move --new-workspace)
- [ ] #7 full herder test battery green and updated for the new call shapes
- [ ] #8 one real live spawn validated end-to-end: herdr agent get shows the spawned agent as working
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Confirm spawn.go routes through lifecyclecmd RunStart (single launch path) vs a second call site. 2. herdrcli: add/adjust a parser so tab-create(result.root_pane) and pane-split(result.pane) both yield pane_info; retire/repoint parseAgentStart. 3. lifecycle.go: replace startArgs — new-tab => 'tab create [--workspace] --cwd --label [--no-focus]'; split => 'pane split <base> --direction <dir> --cwd'; then 'pane run <pane> bash -lic <innerCmd>'; drop the now-redundant newTabMoveArgs pane move on the new-tab path. 4. Emit initial report-agent --state working. 5. Preserve workspace: tab create --workspace; split -> pane move --new-workspace fallback. 6. Update the 63-test battery + herdrcli_test fixtures to the new JSON/argv. 7. Live-validate one real spawn (agent get => working); clean up. 8. Consider pinning/disabling herdr auto-update.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Characterized live 2026-07-27/28 against herdr 0.7.5: agent start re-arch confirmed; tab create / pane split / pane run / report-agent all exercised on throwaway panes (cleaned up). Recipe proven: tab create -> pane run -> report-agent(-session) yields agent_status=working via 'herdr agent get'. Reporting infra already exists (sidecar.go:1146). Fix is contained to pane-creation half. Owner note: new-tab is the dominant path, not split.
<!-- SECTION:NOTES:END -->
