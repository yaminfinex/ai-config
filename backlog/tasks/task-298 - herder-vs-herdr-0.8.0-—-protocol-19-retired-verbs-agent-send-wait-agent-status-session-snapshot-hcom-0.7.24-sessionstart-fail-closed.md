---
id: TASK-298
title: >-
  herder vs herdr 0.8.0 — protocol 19 + retired verbs (agent send / wait
  agent-status / session snapshot); hcom 0.7.24 sessionstart fail-closed
status: Done
assignee: []
created_date: '2026-08-04 01:52'
updated_date: '2026-08-04 02:00'
labels: []
dependencies: []
ordinal: 297500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
herdr auto-updated 0.7.5 -> 0.8.0 (stable, forward-only; predicted by TASK-297's follow-up note). Battery fell 63-green to 6 red: spawn/fork/enroll/resume (hermetic mocks still spoke the pre-0.7.5 agent-start wire — TASK-297's gate covered the Go battery only), live/observer (protocol 16 -> 19 + schema golden drift). Separately, the hcom 0.7.23 -> 0.7.24 pin bump (same day) broke one real-hcom wire test: 0.7.24 fail-closes bare sessionstart for unknown actors (upstream #99) — identity rows come only from hcom start/launch now.

ADAPTATION (all landed this change):
- observercmd/socket.go: supportedHerdrProtocol 16 -> 19 after auditing the 761-line schema diff: only REMOVED method is agent.send; the observer's whole subscription surface (events.subscribe params/ack, pane.* variants, session.snapshot envelope) is byte-identical. Schema golden regenerated.
- bootpaste.go: agent send -> pane send-text (probed live on a throwaway pane: paste-without-submit semantics identical; Enter leg unchanged).
- waitcmd: wait agent-status --status -> agent wait --until (same match + timeout).
- observer CLI fallback: session snapshot -> api snapshot (same envelope; socket method unchanged).
- Test substrate: mock-herdr-spawn rewritten to the tab create/pane split/pane run/report-agent wire (incl. pane list tab_ids for firstPaneInTab, worktree seed-split, movefail repurposed to the surviving split+workspace move leg; retired newtab_movefail); fork/resume inline mocks likewise (bind hook moved to pane run); wait/compact/observer mocks verb-bumped; enroll help golden refreshed (7c5e692 flags); spawn/fork/resume/wait/compact goldens regenerated and diff-reviewed (wire swap only); spawn block normalizer derives <SHORT> from the --label argv on fail-at-creation (the wrapper no longer rides creation argv); check-observer-contract /tmp outputs moved under $CASE (shared-box Permission denied live-hit).
- compactthen_wire_test primes recipient identity via hcom start before sessionstart (matches the live launch path).

Gates: go vet clean, go test ./... ALL PASS, live-contract 10/10 vs real herdr 0.8.0 socket, observer contract green, spawn 74/74. Observer daemon restart on the new build pending rollout. Runbooks updated: herdr-upgrade.md (0.8.0 section), hcom-upgrade.md (0.7.24 fail-closed gotcha).

STILL OPEN (pre-existing follow-up, unchanged): pin/track herdr — stable-channel auto-update will re-break on the next forward-only bump.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
ROLLED OUT 2026-08-04: full check battery 37/39 green + go vet clean + go test ./... ALL PASS. The 2 residual reds (check-identity-doctor, check-launch-contract) are NOT this task's scope — both fail against the concurrent uncommitted launchers/vendor-pinning refactor in the working tree (bin/ai-doctor +124, bin/ai-setup +29, launchcmd/launch.go modified, vendor.go untracked-new); they were green at this task's baseline and their goldens belong to that in-flight work. Resident observer restarted on the fixed build: protocol_compatible=true, sweeps observe via hcom_roster+herdr_snapshot again (was hcom-roster-only while wedged on protocol 16). Also fixed en route: check-observer-contract hardcoded /tmp outputs (shared-box collision with another user's leftover file) moved under $CASE.
<!-- SECTION:NOTES:END -->
