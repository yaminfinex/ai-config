---
id: TASK-105
title: >-
  sesh — post-freeze follow-ups: sync plan R7/state-diagram to frozen conflict
  handshake + first wire amendment batch
status: To Do
assignee: []
created_date: '2026-07-09 05:47'
updated_date: '2026-07-09 06:46'
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
AC#2 satisfied by wire Amendment 1 (dea4ba0, zomi confirm #25965): rescan interval relabeled shipper-local default. AC#1 (plan R7/state-diagram sync — now also carry Amendment 1 clamp+splice and Amendment 2 silent fingerprint routing into the plan wording) still open — routes through the plan owner on missions-and-daemon. Batch item from lovu #26753: cosmetic touch-up when a future amendment next opens the pen — byte_conflict reaction parenthetical "the fingerprint_conflict path selects the right generation" -> "the store's fingerprint routing selects the right generation" (non-normative staleness, adjudicated no-round).
<!-- SECTION:NOTES:END -->
