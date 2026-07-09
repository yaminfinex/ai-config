---
id: TASK-105
title: >-
  sesh — post-freeze follow-ups: sync plan R7/state-diagram to frozen conflict
  handshake + first wire amendment batch
status: To Do
assignee: []
created_date: '2026-07-09 05:47'
updated_date: '2026-07-09 08:24'
labels:
  - sesh
dependencies:
  - TASK-093
priority: low
ordinal: 105000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: design/doc follow-up, from the M0 sign-off verdict (thread sesh-u1, #25130) — carry, not gate. (1) Plan sync: plan R7 and the file-identity state diagram (docs/plans/2026-07-09-001 @ 05dfc47 on branch missions-and-daemon) carry the superseded immediate-open conflict wording; the frozen wire doc (docs/specs/sesh-wire.md, confirm-then-open handshake) binds above the plan — propose the plan edit through whoever owns the plan branch; the orchestrator routes, does not edit. (2) First wire amendment batch, when a pen next opens for cause: relabel "Rescan interval: 60 seconds" under Constants as a shipper default (tunable) rather than a frozen wire constant — fsnotify-coverage calibration may adjust it without amendment ambiguity. Neither item blocks any milestone.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Plan R7 + state diagram synced to the frozen handshake wording (or the plan owner explicitly declines)
- [x] #2 Rescan interval relabeled as default in the wire doc via a recorded amendment
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Disposition at code-complete (2026-07-09): AC2 done earlier (wire-doc parenthetical). AC1 (plan R7/state-diagram sync to frozen conflict handshake + amendment wording) remains OPEN and routes to the plan owner on the missions-and-daemon worktree — the plan is pinned at 05dfc47 for this run's purposes and was executed against as-is; sync is documentation debt, not build debt. The cosmetic byte_conflict parenthetical touch-up queued for 'next amendment' is moot for this run (no third amendment occurred) — rides any future amendment. Nothing here blocks ship.
<!-- SECTION:NOTES:END -->
