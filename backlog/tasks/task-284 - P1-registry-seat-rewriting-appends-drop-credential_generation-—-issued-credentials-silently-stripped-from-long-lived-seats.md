---
id: TASK-284
title: >-
  P1: registry seat-rewriting appends drop credential_generation — issued
  credentials silently stripped from long-lived seats
status: In Progress
assignee: []
created_date: '2026-07-18 12:45'
updated_date: '2026-07-18 12:45'
labels:
  - herder
  - bug
dependencies: []
priority: high
ordinal: 283500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field incident 2026-07-18, found via fleet report ~40 min after the first cutover flip; cutover ROLLED BACK via the documented marker-deletion lever until this is fixed.

Evidence (registry.jsonl, one orchestrator seat): row rotated normally at 12:20:29Z and 12:20:32Z — both appends carried credential_generation in the seat block. Then an append at 12:39:51Z rewrote the SEATED row with the credential_generation key ABSENT (not empty — absent), and a 12:40:51Z append (adds transcript_path) stayed absent. Audit of all 10 currently seated rows: the 5 older seats (issued via the sweep or early completions, then touched by periodic writers) are ALL stripped; the 5 recently-completed seats still carry generations. Conclusion: at least one seat-REWRITING append path builds the seat block fresh instead of carrying forward fields it does not own — the credentials unit taught the completion path the field but missed at least one enrichment/observation-class writer (suspect: sidecar correlated re-enrichment, which updates hcom_name/hooks_bound/transcript_path — matching the 12:39/12:40 append shapes).

Fix scope:
- Enumerate EVERY writer that appends a version of an existing seated row (sidecar enrichment, observer, reconcile, repair, lifecycle transitions, wait/list-side writes if any) and make seat-field carry-forward the default for fields the writer does not own — a writer must never silently drop registry facts it did not set.
- Consider making this structural (single carry helper all seat-rewriting appends go through) rather than per-writer patches, so the NEXT new seat field cannot repeat this class.
- Pins: per-writer carry test (append via each writer against a row with a generation; field must survive) plus a general source-inventory-style pin that fails when a new seat-rewriting append path appears without a carry test.
- After merge: fresh issuance sweep must show 100% coverage AND a soak (seats touched by periodic writers retain generations) before the cutover marker is re-created (owner action).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every seat-rewriting append path enumerated and carries credential_generation (and other non-owned seat facts) forward, with a per-writer carry pin
- [ ] #2 Structural guard: new seat-rewriting appends cannot ship without carry coverage (inventory-style pin)
- [ ] #3 Post-fix live validation: sweep to 100%, then periodic-writer soak shows no generation loss before re-enable
<!-- AC:END -->
