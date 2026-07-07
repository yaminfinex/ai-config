---
id: TASK-023
title: 'herder spawn --notify-to: accept a literal bus name (post-TASK-003 regression)'
status: Done
assignee:
  - unit-h-risa
created_date: '2026-07-07 08:31'
updated_date: '2026-07-07 09:02'
labels:
  - run-herder-dx
dependencies: []
priority: high
ordinal: 23000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Found live by orchestrator dispatching wave 3: herder spawn --notify-to hera now hard-errors ('spawner does not resolve to a bus-bound agent... tried --notify-to "hera"') — the post-TASK-003 resolution treats --notify-to purely as a registry hint (guid/label/terminal/pane -> row -> hcom_name) and never considers that the value may BE a bus name. Also visible: spawner detection from a Claude Code Bash-tool env yields guid 'user', raw pane p_744, empty terminal — so self-resolution cannot rescue it. Fix: resolve --notify-to against registry hcom_name (active rows) and/or fall back to literal bus name validated against hcom list, consistent with send's HERDER_BUS=hcom literal affordance. Keep the bus-less hard error for genuinely unresolvable targets.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 spawn --notify-to <bus-name> works from a non-bus-env shell (live smoke: notify lands as hcom message)
- [x] #2 unresolvable --notify-to still hard-errors before pane creation; suite golden updated
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit cdb7652. resolveSpawnerBus gained two steps after guid/label/pane resolution of --notify-to: (3) an ACTIVE registry row whose hcom_name equals the value (closed rows never vouch — names recycle), (4) literal bus name validated against `hcom list` on the bus the CHILD will join (hcomDirEff resolved early; team-scoped, so cross-bus names still hard-error — the child could not reach them). Registry-unreadable no longer aborts resolution (literal path still works from non-bus-env shells). Deliberate tightening (hera-approved sliding door): an EXPLICIT --notify-to resolving nowhere now returns hard error instead of silently falling through to the spawner own name — typo protection. AC#1 live smoke: env -u HERDER_GUID (exact incident env) spawn --worktree --notify-to unit-h-risa -> resolved, child notify landed as hcom message #2119; hera reproduced independently with --notify-to hera. AC#2: notify.txt golden regenerated (message now names bus-name attempts + remedy) + notify_to_unresolvable assertion pins rc=1, no agent start on the wire, no registry row. New goldens notify_to_hcomname / notify_to_busname; unit test TestResolveSpawnerBusAcceptsBusNames (stubbed hcom). Help/README document the affordance.
<!-- SECTION:NOTES:END -->
