---
id: TASK-118
title: mish U6 — mish status single-mission report
status: To Do
assignee: []
created_date: '2026-07-09 09:46'
labels:
  - mish
dependencies: []
priority: high
ordinal: 118000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Part of the mish mission-CLI build. FIRST read /home/grace/Coding/ai-config/napkins/mish-build/playbook.md fully (binding worker protocol, verification gate, settled decisions, stop-and-report rule), then this capture, then your plan unit section and the spec sections it cites — both files are in your worktree: docs/specs/mission-spec.md (RATIFIED, authority) and docs/plans/2026-07-09-001-feat-mish-mission-cli-plan.md (the plan).

Goal: spec §6.3 single-mission block (board counts in the board's configured status order, artifact count + newest mtime recency) plus the full one-line warning set; strictly read-only (invariant 11). Plan §U6; spec §6.3, R4/R12.

Files: tools/mish/internal/cli/{status.go,status_test.go}. Depends on U2+U3.

Settled decisions: compose from missionfs findings (KTD6 — direct frontmatter parse, no backlog subprocess) + git seam staleness (KTD7 — read-only, scoped to missions/<slug>, silently skipped when repo isn't git or has no upstream; never more than one warning line); recency from mtimes only, node-local; --mission naming a missing dir refuses before any partial render; warnings each one line: pinned-key drift, mission:≠dirname, unknown frontmatter keys, invalid status: value, duplicate task IDs, missing board, missing artifacts, uncommitted/unpushed subtree.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC-12 unit level: happy block matches §6.3 shape; each warning fires from its induced condition
- [ ] #2 staleness line appears with fake git seam reporting dirty/unpushed; absent when seam reports non-git
- [ ] #3 before/after subtree hash identical (read-only proof)
- [ ] #4 board counts render in configured status order for a board with custom statuses
- [ ] #5 --mission naming a missing dir refuses with §5.3 wording before any output
<!-- AC:END -->
