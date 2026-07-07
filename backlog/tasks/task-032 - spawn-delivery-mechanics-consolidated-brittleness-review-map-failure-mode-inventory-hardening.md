---
id: TASK-032
title: >-
  spawn delivery mechanics: consolidated brittleness review (map, failure-mode
  inventory, hardening)
status: To Do
assignee: []
created_date: '2026-07-07 20:31'
labels:
  - run-herder-dx
dependencies: []
priority: high
ordinal: 32000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
USER CONCERN (2026-07-07): TASK-024 then TASK-031 in quick succession suggest boot-time prompt delivery is brittle — "might be worth a consolidated review across spawning to make sure we do not have brittle mechanics". Diagnosis is part of the deliverable: WHY does this subsystem keep producing incidents?

Working hypothesis to test, not assume: bootpaste drives a TUI whose readiness is only indirectly observable, and each patch (TASK-023 notify resolution, TASK-024 verify evidence gating, TASK-031 boot-swallowed Enter) hardened one observed race while the underlying state machine (ready-wait → paste → land-verify → Enter retries → submit-verify, per agent family) has never been reviewed as a whole. Known facts: claude boots fast and has been reliable; codex boots slow, ready-wait can time out (status=done) before the TUI is interactive, Enters fired blind get swallowed, and a queued bus message cannot flush into an idle codex (no turn boundary) — so a stranded composer is a dead end without manual send-keys.

PHASE A (no code): map the full delivery state machine end-to-end for spawn --prompt and compact, per agent family (claude/codex/bash) — every timeout, retry, evidence gate, and what each failure reports. Place every known incident on the map with root cause. Enumerate residual race windows. Report the map + a ranked hardening proposal on your thread for ratification BEFORE any code. Consider whether ready-wait should be agent-family-aware (codex needs interactive-TUI readiness, not process-alive). PHASE B: implement the ratified subset.

SEQUENCING: TASK-031 (late-submit remedy) is GATED on this review — its fix should fall out of the map, not precede it. Evidence sources: run-log incidents (wave-3 dispatch failures, reviewer-kimi stranding), spawncmd/bootpaste.go + compact.go, launchcmd ready-wait, check-spawn/compact suites + goldens.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 PHASE A map artifact exists (napkin or docs): full delivery state machine per agent family with every timeout/retry/evidence gate named
- [ ] #2 Failure-mode inventory: TASK-023/024/031 + wave-3 NOT-confirmed incidents each located on the map with root cause; residual race windows enumerated explicitly
- [ ] #3 Ranked hardening proposal reported for ratification BEFORE code, incl. agent-family-aware readiness assessment and the TASK-031 late-submit question
- [ ] #4 Ratified subset implemented with suite/golden coverage; TASK-024 evidence gating not weakened; battery green
- [ ] #5 TASK-031 resolved or formally superseded; spawn NOT-confirmed hint + README delivery section match post-hardening reality
<!-- AC:END -->
