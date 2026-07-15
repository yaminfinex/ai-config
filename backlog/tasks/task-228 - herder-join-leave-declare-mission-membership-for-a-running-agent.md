---
id: TASK-228
title: 'herder join/leave: declare mission membership for a running agent'
status: Done
assignee: []
created_date: '2026-07-15 05:02'
updated_date: '2026-07-15 08:06'
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
- [x] #1 herder join <mission-slug> records explicit membership for the calling/target agent; herder leave removes it
- [x] #2 Membership surfaces on herder list --json rows (field shape documented in the DONE report for the mc lane)
- [x] #3 Explicit membership wins over cwd marker inference; absent membership falls back to inference
- [x] #4 Refusals are typed cause+remedy (unknown slug shape, no live row, double-join semantics defined and tested)
- [x] #5 Unit tests cover join, leave, precedence over inference, and list --json surfacing
<!-- AC:END -->











## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged 0b0b3b4 (--no-ff, 2 commits: e644e85 feature + 5db13fb validation tripwire). Design-first checkpoint ran pre-code (design doc merged at docs/design/2026-07-15-herder-mission-membership.md incl. adjudicated lifecycle rider). Shipped: join/leave verbs (self via HERDER_GUID or --target), explicit membership durable (mission:{slug,source:explicit} only), inference view-only at list render (explicit > cwd > marker > null), leave-by-omission resumes inference, lifecycle matrix pinned by mutation (same-guid carry; fork/adoption no-inherit; observer turnover transfer; ordinary events cannot silently change membership), 7 typed refusals all mutation-red, list --json additive-only (pure mission:null additions in goldens). Review: opus incumbent — cross-binary byte-identity rotation proof for scope-leak (gold standard), 1 required fix (validateDurableMission untested; 3 surviving mutants -> all red after fix, incumbent re-ran battery), calibration P1 (V2Resolve last-wins) adjudicated OUT as pre-existing shared-resolver behavior -> TASK-236. Gates: independent 60/60 at e644e85, re-gate 60/60 at 5db13fb, post-merge 60/60 on main. mc-side consumer: group on mission.slug; mission:{} additive-extensible; marker-source rows need MISSIONS_REPO in the listing caller env (design-consistent, flagged to mc).
<!-- SECTION:NOTES:END -->
