---
id: TASK-256
title: >-
  Update herdr api-schema golden + any parsing for 0.7.4 shapes (tokens replace
  custom_status, popup/graphics params)
status: Done
assignee: []
created_date: '2026-07-16 01:20'
updated_date: '2026-07-16 03:27'
labels:
  - herder
dependencies: []
priority: high
ordinal: 255500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The 0.7.4 handoff changed the api schema without a protocol bump (still 16): PaneInfo/WorkspaceInfo gained metadata tokens and lost custom_status, popup-pane and pane-graphics request params added, PaneReportMetadataParams ttl bounds changed. check-live-contract's schema-drift check is RED against the committed golden (the only live-contract failure; 10/11 pass). Grep confirms no herder code touches removed fields. Per runbook: update tools/herder/tests/goldens/live-contract/herdr-api-schema.json and any herdrcli parsing + goldens in the SAME change, from a machine running 0.7.4.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Golden regenerated from the live 0.7.4 server and inspected (not blind --write)
- [x] #2 check-live-contract 11/11 green; any parsing that consumed changed shapes updated in the same diff
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Dispatched 2026-07-16 as a priority lane (worktree task-256-schema-golden): the drift golden now blocks every full battery on this box — it voided the compact-fix lane's gate at suite 16. Deliberate-review regen (field-delta enumeration + removed-field consumer evidence), not a blind re-capture.

Regenerated the live-contract api-schema golden from installed herdr 0.7.4. Protocol remains 16. Deliberate delta review: 5 methods added (workspace metadata reporting, pane graphics set/clear/info, popup close), metadata tokens maps added across pane/agent/workspace shapes, terminal_title fields added, ttl_ms bounds tightened; custom_status/clear_custom_status removed from all six surfaces; no definitions/methods/enums removed. Consumer audit: zero matches for removed fields across all four modules — no parsing change needed; diff is exactly the regenerated golden. Orchestrator verification: independent live-contract re-run 11/11, protocol pin confirmed, identifier sweep clean, single-file commit confirmed. Adversarial review seat waived under the stakes gate (machine-regenerated test fixture deterministically validated against the installed binary; independently re-verified). Builder battery 61/61; post-merge main battery 61/61 (r2 — r1 voided by an orchestrator gate-harness PATH error, not by main).
<!-- SECTION:NOTES:END -->
