---
id: TASK-059
title: 'wave A5: registry rotation + list --all archive consultation (spec 5.1 growth)'
status: In Progress
assignee: []
created_date: '2026-07-08 05:55'
updated_date: '2026-07-08 09:09'
labels: []
dependencies: []
priority: low
ordinal: 59000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Plan unit A5 (spec-plan-wave-a.md). Size-threshold rotation reusing A4 rotate-reseed mechanics; archives read-only beside the log; list --all and lineage resolution consult archives. Smallest unit; may fold into A4 if the worker is ahead. Depends: A4 (TASK-058).
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 08:57
---
[hera 2026-07-08] +A5 obligation from A4 review (mori #9974 LOW): post-migration, retired guids live archive-only — list --all must consult archives and lineage resolution must resolve forked_from pointing at archived guids, else they vanish/dangle silently. Carry the reviewer's evidence into the A5 brief.
---

created: 2026-07-08 09:09
---
[hera 2026-07-08] A4 merged; dispatching A5 (rotation + archive consultation) — final wave-A unit.
---
<!-- COMMENTS:END -->
