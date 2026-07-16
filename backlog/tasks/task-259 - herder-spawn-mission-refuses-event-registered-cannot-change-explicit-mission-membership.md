---
id: TASK-259
title: >-
  herder spawn --mission refuses: 'event registered cannot change explicit
  mission membership'
status: To Do
assignee: []
created_date: '2026-07-16 05:09'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 258500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Peer-relayed defect (mission-control walkthrough seat, post-herdr-0.7.4 window): herder spawn --mission <slug> refuses with "event registered cannot change explicit mission membership". The spawn-time membership write appears to collide with membership state the registered event itself already carries — i.e., the registration path now claims explicit membership before (or while) the --mission flag tries to write it, and the guard that forbids a registered event from changing explicit membership fires against spawn's own flag.

Known workaround: spawn missionless, then join the mission after bind.

Investigation starters: reproduce with a scratch mission; trace where spawn's --mission membership lands relative to the registered event append (one write or two?); check whether the guard message is aimed at a different caller class (post-hoc membership edits) and is over-matching spawn's atomic intent; decide whether the fix is ordering (membership rides the registration event as one write) or guard scoping. Regression pin: spawn --mission seats a member in one verb; the refusal remains for genuinely conflicting post-registration membership rewrites.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Repro pinned in an isolated fixture (red on current code)
- [ ] #2 spawn --mission works as one atomic verb; the membership-change guard still refuses genuine post-registration conflicts (both pinned)
- [ ] #3 Workaround note removed from docs/help if any grew around it
<!-- AC:END -->
