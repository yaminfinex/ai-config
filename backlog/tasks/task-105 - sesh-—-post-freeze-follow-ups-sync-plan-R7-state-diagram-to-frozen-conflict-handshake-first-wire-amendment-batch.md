---
id: TASK-105
title: >-
  sesh — post-freeze follow-ups: sync plan R7/state-diagram to frozen conflict
  handshake + first wire amendment batch
status: Done
assignee: []
created_date: '2026-07-09 05:47'
updated_date: '2026-07-10 10:13'
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
- [x] #1 Plan R7 + state diagram synced to the frozen handshake wording (or the plan owner explicitly declines)
- [x] #2 Rescan interval relabeled as default in the wire doc via a recorded amendment
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Supersession audit 2026-07-10: AC2 was already done (wire-doc parenthetical amendment). AC1 completed in this audit: the routing blocker dissolved when the missions-and-daemon branch merged — the plan is an ordinary main doc now, in the sesh lane. Synced three stale spots in docs/plans/2026-07-09-001-feat-sesh-session-service-plan.md to the frozen confirm-then-open handshake in docs/specs/sesh-wire.md: R7 prose (conflict no longer opens a generation immediately — shipper re-checks identity and retries once, confirmed second divergence opens the generation), the state-diagram Poisoned transition (poisoned = recurrence after a conflict-driven generation, not second conflict), and the sequence diagram's divergent branch (409 byte_conflict + confirm-then-open, replacing the stale generation-hint/recreate-path reaction). The cosmetic byte_conflict parenthetical still rides any future wire amendment (no pen open).
<!-- SECTION:NOTES:END -->
