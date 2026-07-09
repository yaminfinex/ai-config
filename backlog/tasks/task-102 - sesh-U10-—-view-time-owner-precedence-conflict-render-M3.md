---
id: TASK-102
title: sesh U10 — view-time owner precedence + conflict render (M3)
status: Done
assignee: []
created_date: '2026-07-09 05:29'
updated_date: '2026-07-09 07:32'
labels:
  - sesh
dependencies:
  - TASK-099
  - TASK-101
priority: medium
ordinal: 102000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Lane: surface (claude worker). Deliverable: internal/surface/owner.go + template updates — display-owner precedence computed at view time over the facts observation log: SESSION_OWNER > tailnet identity (M4+) > OS user > hostname, winning source labeled; conflicting SESSION_OWNER observations for one session -> honest absence + conflicting-claims label; unclaimed sessions group under node/OS-user. Pure store/surface logic — assert no precedence code exists shipper-side. Requirement R15.

Read first: /home/grace/Coding/ai-config/napkins/sesh-build/playbook.md, plan U10 section (git show 05dfc47:docs/plans/2026-07-09-001-feat-sesh-session-service-plan.md), spec section 3.2, captures Lane 3 (git show 6843649:docs/design/2026-07-09-sesh-task-captures.md). Thread: sesh-u10.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Each precedence tier wins when higher tiers absent; label names the source
- [ ] #2 Conflict renders honest absence with conflicting-claims label
- [ ] #3 macOS facts-only session falls through to tailnet identity (M4) or OS user (pre-M4)
- [ ] #4 No precedence logic shipper-side (asserted)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Done at 1a5411f, merged to sesh-build (ff). Cycle: rizo (codex) adversarial review ACCEPT zero findings — precedence centralized in owner.go, raw facts across the seam (I1), conflict = honest absence + names withheld, claimed-tier grouping per 4.4, empty-string filtered as absence, no wire/shipper change. Orchestrator runs: all eleven harnesses green in worker worktree; merged-state module gates + s6/surface-live/surface-fixtures green. Sliding doors on thread sesh-u10 (grouping split, names withheld page-wide, conflict-as-unclaimed+badge). Tailnet tier wired-but-empty awaiting U11 WhoIs.
<!-- SECTION:NOTES:END -->
