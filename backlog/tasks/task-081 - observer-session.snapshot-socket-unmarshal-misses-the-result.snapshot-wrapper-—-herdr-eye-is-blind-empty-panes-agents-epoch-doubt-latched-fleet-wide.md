---
id: TASK-081
title: >-
  observer: session.snapshot socket unmarshal misses the result.snapshot wrapper
  — herdr eye is blind (empty panes/agents), epoch-doubt latched fleet-wide
status: In Progress
assignee: []
created_date: '2026-07-08 23:40'
updated_date: '2026-07-08 23:41'
labels: []
dependencies: []
priority: high
ordinal: 81000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
FIRST LIVE SWEEP FINDING (2026-07-08, first production run of the TASK-080 observer; found during bake kickoff). herder observer sweep completed, appended one CORRECT recognised row (bus-sourced sid enrichment on the orchestrator seat), but flagged epoch-doubt: "no recorded seated terminal ids appear in the current snapshot; absence verdicts paused" — while at least three recorded seated terminals were demonstrably live in the snapshot.

ROOT CAUSE (verified against the live server, herdr 0.7.3 protocol 16): tools/herder/internal/observercmd/socket.go snapshot() unmarshals the raw JSON-RPC result directly into herdrcli.Snapshot, but the live server wraps the payload one level down:

  result = {"type": "session_snapshot", "snapshot": {agents, panes, protocol, version, tabs, workspaces, layouts, focused_*}}

So snap.Panes and snap.Agents decode empty, herdrState.byTerm is empty, herdrOverlap is trivially 0/N, and epochFlags latches epoch-doubt on every connection-gap sweep. The same wrapper WAS handled for pane.process_info (socket.go processInfo tries {"process_info": ...} then direct) — session.snapshot needs the same wrapped-then-direct treatment. The contract suite passed because mock-herdr serves the FLAT shape the parser expects: mock-shape drift (same defect class as upstream-ledger candidate 9 on TASK-029).

LIVE SHAPE EVIDENCE (raw socket probes, 2026-07-08): result keys = [type, snapshot]; snapshot keys = [agents, focused_pane_id, focused_tab_id, focused_workspace_id, layouts, panes, protocol, tabs, version, workspaces]; snapshot.protocol = 16, snapshot.version = "0.7.3"; pane object keys = [agent, agent_status, cwd, focused, foreground_cwd, label, pane_id, revision, scroll, tab_id, terminal_id, workspace_id]; snapshot agent object keys = [agent, agent_status, cwd, focused, foreground_cwd, pane_id, revision, tab_id, terminal_id, workspace_id] — note NO name/session field on snapshot agents; verify the herdrcli.Agent field mapping (status vs agent_status, Name) against this shape rather than assuming agent.list parity.

SECOND QUESTION THE FIX MUST ANSWER: the sweep reported protocol_compatible=true despite snap.Protocol decoding as 0 — find where the protocol-16 pin actually reads from; if it reads the broken snapshot field and treats 0 as compatible, that is a second bug (the pin must trip on mismatch, and must read a field that really exists post-fix).

CONSEQUENCES until fixed: the observer herdr eye is blind — no pane/agent evidence, no absence verdicts, only bus-sourced observations land; epoch-doubt advice annotates every herder list row. Positive-evidence discipline means no WRONG unseats were appended (verified: sweep summary applied=1 noop=0 refused=0, the one applied row is correct). DO NOT start the observer daemon (herder observer run) until this lands.

Fix surface: socket.go snapshot() unwrap; mock-herdr socket mode must serve the REAL nested shape (mock fidelity is the regression this task exists to prevent); check-observer-contract.sh extended; loadHerdrStateCLI (observer.go) must be checked against real `herdr session snapshot` CLI output for the same nesting.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 herder observer sweep against the live herdr server populates byTerm (recorded live seated terminals overlap the snapshot; no epoch-doubt flag when recorded terminals are present in the snapshot)
- [ ] #2 socket snapshot() handles the wrapped result.snapshot shape (wrapped-then-direct, mirroring processInfo); snapshot agent/pane field mappings verified against the live shape quoted in this task
- [ ] #3 protocol pin verified: identify where protocol 16 is read; a snapshot/discovery protocol mismatch trips the documented fallback path, and a decoded zero is never treated as compatible
- [ ] #4 mock-herdr socket mode serves the real nested session.snapshot shape; the suite fails if the parser regresses to flat-only
- [ ] #5 CLI fallback path (loadHerdrStateCLI) checked against real herdr CLI output for the same nesting, fixed or explicitly confirmed correct
- [ ] #6 gate green: go vet+test both modules, all 30 check suites
<!-- AC:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 23:41
---
Dispatched: worker @task081-zoni (codex), worktree task-081-observer-snapshot, brief napkins/run-herder-dx/brief-081.md (mechanics + stop-and-report quoted prominently per the TASK-078 amendment). Adversarial review to follow DONE per engine-diff doctrine.
---
<!-- COMMENTS:END -->
