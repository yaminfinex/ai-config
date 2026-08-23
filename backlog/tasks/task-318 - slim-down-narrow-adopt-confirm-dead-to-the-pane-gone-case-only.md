---
id: TASK-318
title: 'slim-down: narrow adopt --confirm-dead to the pane-gone case only'
status: In Progress
assignee: []
created_date: '2026-08-23 13:05'
labels:
  - herder
  - slimdown
dependencies: []
ordinal: 317500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The held slice of the adopt/enroll deletion unit, released by owner ruling
2026-08-23: keep the adopt --confirm-dead flag, but honor it ONLY when the
old seat's recorded pane no longer exists (the one fact the probe cannot
observe, so the human vouches for it). Everywhere the probe answers, the
flag is refused: unnecessary when the pane is provably vacant (positive
death — adopt proceeds flag-free), contradicted when the old session is
provably live in its pane, not applicable (fail closed) on a foreign
occupant or an unprobeable pane. Today the flag is an unconditional bypass
of all live checks. Brief: missions repo,
missions/fleet-refit/artifacts/conductor/briefs/adopt-confirm-dead-flag-brief.md;
playbook herder-slimdown-run-playbook.md governs.
<!-- SECTION:DESCRIPTION:END -->
