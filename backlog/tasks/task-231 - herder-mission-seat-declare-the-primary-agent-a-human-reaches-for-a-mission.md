---
id: TASK-231
title: 'herder mission seat: declare the primary agent a human reaches for a mission'
status: To Do
assignee: []
created_date: '2026-07-15 06:00'
labels:
  - herder
dependencies:
  - TASK-228
priority: medium
ordinal: 230500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner-motivated (a human seat was used as a dead-letter target when an orchestrator dropped off the roster; a declared mission seat is the right target for that traffic). Extend mission membership with a SEAT declaration: the one agent per mission slug that human-facing traffic routes to by default.

Settled representation (committed to the mc lane, do not fork it): seat is a FIELD ON THE MEMBERSHIP OBJECT carried by the session row (mission:{slug, source, seat:...} on list --json), never a parallel record or second registry surface. Consumers ignore unknown keys inside mission:{} (forward-compatible by contract).

Design points for the unit (design-first per standing practice): join --seat flag vs separate verb; ONE-SEAT-PER-SLUG is a cross-row invariant — enforcement belongs in the existing locked write path at declaration time (check sibling sessions under the same lock); seat handoff semantics (displacing an existing seat: refuse vs --take-seat); lifecycle interaction with the adjudicated membership matrix (fork/adoption do not inherit membership, so they cannot inherit seat; same-guid carry should carry seat; observer-proven turnover should transfer it with the membership); what leave does to a held seat (must clear it — a mission must never have a phantom seat).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Seat rides the membership object on the session row; no parallel representation
- [ ] #2 One seat per mission slug enforced under the existing locked write path (cross-row check)
- [ ] #3 Lifecycle matrix extended: seat follows membership carry/transfer rules; leave clears a held seat
- [ ] #4 Typed refusals for seat conflicts; refusal names the current seat holder's label
<!-- AC:END -->
