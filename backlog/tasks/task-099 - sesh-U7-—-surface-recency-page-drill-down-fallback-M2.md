---
id: TASK-099
title: 'sesh U7 — surface: recency page + drill-down + fallback (M2)'
status: In Progress
assignee: []
created_date: '2026-07-09 05:28'
updated_date: '2026-07-09 06:27'
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
Merged to sesh-build @ b69d8c0 (merge of f36da04). Provenance: b94dc15+e782579 impl (fixture-backed at M0 per ship plan) -> cross-family codex review (MERGE-WITH-FIXES: display-byte-budget DoS hole on raw path, harness toolchain preflight; all 8 sliding doors SOUND) -> f36da04 fixes (8MiB budgets on BOTH render paths incl. the parallel transcript hole ravi self-found, honest notices, budget tests; actionable preflight) -> tutu re-check PASS both + MERGE rec. Orchestrator re-ran gates fresh at each step; harness ALL GREEN twice. Status stays In Progress: AC#5 (owner eyeball) is the M2 exposure gate; live-index integration is the M2 leg. XSS/write-surface review clean; htmx sha256 verified.
<!-- SECTION:NOTES:END -->
