---
id: TASK-165
title: 'registry: retire the legacy-shaped production write bridge (design first)'
status: To Do
assignee: []
created_date: '2026-07-12 12:19'
updated_date: '2026-07-12 13:44'
labels: []
dependencies: []
priority: medium
ordinal: 164000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
DESIGN task. The read-side two-state view is retired, but lifecycle, sidecar, and reconcile still construct old-shaped raw JSON registry candidates (including literal status active/closed fields) and cross a compatibility bridge that parses the untyped row and converts it back to a typed v2 record, with a status-derived state fallback inside the converter. Spawn and enroll already demonstrate the target shape: typed v2 candidates under the locked writer. Design deliverable: inventory the raw JSON/CLI compatibility obligations of the three writers, decide the target write shape per caller, and produce filed-ready implementation task(s). The write spine (normalizers, typed outcomes, state machine) is ratified — this moves CALLERS onto it; any spine change is out of scope. Include: the bridge function removed once no production caller needs it; the converter no longer infers v2 state from two-state status unless a documented migration-only caller proves necessary; raw/list/JSON output compatibility preserved by tests or deliberately changed with a documented contract decision; LegacyV1 raw-coordinate migration compatibility stays guarded; the unused broad latest-archive helper deleted as ride-along.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Design memo inventories the compatibility obligations of all three legacy-shaped writers with a per-caller target write shape
- [ ] #2 Filed-ready implementation task(s) with settled-decisions lists produced
- [ ] #3 Explicit ruling per compatibility surface: preserved, or changed with documented contract decision
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Additional evidence from the four-state wording review (verified independently): the legacy-v1 status mappings DISAGREE across paths — legacyV1State (registry.go:236) and the v2 projection ingest (v2/registry.go:~337) map v1 status:"active" -> unseated, while V2FromRecord's fallback (registry.go:~735) maps the same status -> seated. Observable today: cull on a legacy active row refuses with "already unseated (migrated corpse)" while list shows the same row seated. The design inventory must pick ONE mapping and eliminate the other path (removing the V2FromRecord fallback, already in scope, kills one side).
<!-- SECTION:NOTES:END -->
