---
id: TASK-228
title: 'herder join/leave: declare mission membership for a running agent'
status: To Do
assignee: []
created_date: '2026-07-15 05:02'
labels:
  - herder
dependencies: []
priority: high
ordinal: 227500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner directive 2026-07-15 (SUPER HIGH): agents must be able to declare mission membership after spawn. Add 'herder join <mission-slug>' and 'herder leave' for an ALREADY-RUNNING agent. Owner called this the pre-req for spawn --mission (separate task, depends on this one).

Settled requirements (mission-control side, from the mc lane):
- Membership must surface on 'herder list --json' rows so mc can group agents by mission.
- Explicit membership WINS over marker inference; mish resolve at cwd stays as the fallback when no explicit membership exists.
- Leaving returns the agent to inference (removes explicit membership; does not write an anti-membership).

Mechanics (registry event shape, storage, verb ergonomics) are the herder lane's call — design the row/field shape, then report it so the mc-side grouping task can be filed same day. Registry writes go through the existing locked write path; no new write spine.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 herder join <mission-slug> records explicit membership for the calling/target agent; herder leave removes it
- [ ] #2 Membership surfaces on herder list --json rows (field shape documented in the DONE report for the mc lane)
- [ ] #3 Explicit membership wins over cwd marker inference; absent membership falls back to inference
- [ ] #4 Refusals are typed cause+remedy (unknown slug shape, no live row, double-join semantics defined and tested)
- [ ] #5 Unit tests cover join, leave, precedence over inference, and list --json surfacing
<!-- AC:END -->
