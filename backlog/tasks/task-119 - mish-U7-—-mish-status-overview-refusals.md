---
id: TASK-119
title: mish U7 — mish status overview + refusals
status: In Progress
assignee: []
created_date: '2026-07-09 09:46'
updated_date: '2026-07-09 10:24'
labels:
  - mish
dependencies: []
priority: high
ordinal: 119000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Part of the mish mission-CLI build. FIRST read /home/grace/Coding/ai-config/napkins/mish-build/playbook.md fully (binding worker protocol, verification gate, settled decisions, stop-and-report rule), then this capture, then your plan unit section and the spec sections it cites — both files are in your worktree: docs/specs/mission-spec.md (RATIFIED, authority) and docs/plans/2026-07-09-001-feat-mish-mission-cli-plan.md (the plan).

Goal: spec §6.3 overview mode: one line per mission dir (SLUG/STATUS/AUTHORITY/OWNER/TASKS/UPDATED), closed missions included; triggered by --all or contextless-inside-missions-repo; contextless OUTSIDE the repo refuses with §5.3 guidance. Plan §U7; spec §6.3, R4/R5.

Files: tools/mish/internal/cli/status.go + status_test.go extended. Depends on U6 (same file — sequential after U6, branch from mish-build after U6 merges).

Settled decisions: cheap filesystem scan of missions/*/ only; TASKS column in each board's own status order; UPDATED = node-local mtime recency; a mission dir with a broken manifest gets a row with a warning marker rather than aborting the table; never guess overview when contextless outside the repo.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC-13 unit level: overview from repo root with no marker lists active + closed missions; unrelated cwd with no context refuses
- [ ] #2 --all works from anywhere with MISSIONS_REPO set
- [ ] #3 broken-manifest mission renders a warning row, table survives
<!-- AC:END -->
