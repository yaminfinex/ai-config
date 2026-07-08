---
id: TASK-062
title: spawn orphans the pane when the registry write refuses (cull-on-write-refusal)
status: To Do
assignee:
  - hera
created_date: '2026-07-08 07:13'
labels: []
dependencies: []
ordinal: 62000
---

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
From A2 adversarial review LOW-3 (#7705): spawncmd launches the pane (spawn.go:791) BEFORE the registry write; if UpdateLocked refuses (flock refusal, label collision) spawn dies at :953 leaving an orphan pane with no registry row. Pre-existing pattern (old appendLine also died post-launch) but A2's lock-refusal path widens when it fires. Proposal: on registry-write refusal after pane launch, cull the just-launched pane before dying (best-effort, report both errors), or claim the label/row BEFORE launching and enrich after. Related: A2 also made cull return command failure on write refusal instead of silent success — spawn should get the same discipline.
<!-- SECTION:NOTES:END -->
