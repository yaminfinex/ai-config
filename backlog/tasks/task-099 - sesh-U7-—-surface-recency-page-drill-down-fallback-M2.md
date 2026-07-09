---
id: TASK-099
title: 'sesh U7 — surface: recency page + drill-down + fallback (M2)'
status: In Progress
assignee: []
created_date: '2026-07-09 05:28'
updated_date: '2026-07-09 09:41'
labels:
  - sesh
dependencies:
  - TASK-098
priority: medium
ordinal: 99000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Lane: surface (claude worker). Can start at M0 against fixtures + the frozen index schema; integrates at M2. Deliverable: internal/surface — server-rendered html/template + one embedded htmx asset; recency page (person -> nodes -> sessions, recency = max parsed message timestamp, first-ingest time for fully quarantined sessions, mirrored-at secondary); transcript drill-down from index rows ordered by (parsed timestamp, file, in-file ordinal) — NEVER parentUuid chains — tool calls collapsed; raw-JSONL fallback from the mirror whenever the index cannot render. The page must never 500 on a mirrored session. No search, no write actions, no form/POST surface (R17). Requirements R14,R16,R17.

Read first: /home/grace/Coding/ai-config/napkins/sesh-build/playbook.md, plan U7 section (git show 05dfc47:docs/plans/2026-07-09-001-feat-sesh-session-service-plan.md), docs/specs/sesh-wire.md index schema, captures Lane 3 (git show 6843649:docs/design/2026-07-09-sesh-task-captures.md). Thread: sesh-u7.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Backfilled old session sorts below a live one despite later ingest (parsed-timestamp recency)
- [x] #2 Fully-quarantined session renders raw with first-ingest ordering; resume-pair renders one transcript, no duplicated history (S2)
- [x] #3 Multi-MB single line truncates in render with raw fallback available
- [x] #4 Zero form/POST surface; every fixture session renders valid HTML (golden snapshots)
- [ ] #5 Owner eyeball sign-off at M2 (the exposure gate)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Status intent (2026-07-09, code-complete): deliberately held In Progress — ACs 1-4 done and merged (fixture leg b69d8c0, live integration 0f3b325 + harness fix 3fb59db, all through review; on sesh-build @ 5105225). AC5 is the owner eyeball at the M2 exposure gate, requested from @bigboss on thread sesh-m2gate (with the tailscale-serve exposure sign-off). This is the only task open on an owner action; check AC5 and close the moment the eyeball is recorded.
<!-- SECTION:NOTES:END -->
