---
id: TASK-062
title: spawn orphans the pane when the registry write refuses (cull-on-write-refusal)
status: To Do
assignee:
  - hera
created_date: '2026-07-08 07:13'
updated_date: '2026-07-08 23:42'
labels: []
dependencies: []
ordinal: 62000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
spawncmd launches the pane BEFORE the registry write; if UpdateLocked refuses (flock refusal, label collision) spawn dies leaving an orphan pane with no registry row (launch at spawn.go:791, death at :953 at time of filing — re-locate at fix time). Pre-existing pattern, but the A2 lock-refusal path widened when it fires. Fix directions: on registry-write refusal after pane launch, cull the just-launched pane before dying (best-effort, report both errors), or claim the label/row BEFORE launching and enrich after. Related discipline: A2 made cull return command failure on write refusal instead of silent success — spawn gets the same.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
From A2 adversarial review LOW-3 (#7705): spawncmd launches the pane (spawn.go:791) BEFORE the registry write; if UpdateLocked refuses (flock refusal, label collision) spawn dies at :953 leaving an orphan pane with no registry row. Pre-existing pattern (old appendLine also died post-launch) but A2's lock-refusal path widens when it fires. Proposal: on registry-write refusal after pane launch, cull the just-launched pane before dying (best-effort, report both errors), or claim the label/row BEFORE launching and enrich after. Related: A2 also made cull return command failure on write refusal instead of silent success — spawn should get the same discipline.
<!-- SECTION:NOTES:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 registry-write refusal after pane launch no longer orphans the pane: either best-effort cull with both errors reported, or claim-before-launch — one chosen, rationale in the DONE report
- [ ] #2 spawn exits non-zero on registry-write refusal
- [ ] #3 suite covers the refusal path (no orphan pane, no phantom row)
<!-- AC:END -->
