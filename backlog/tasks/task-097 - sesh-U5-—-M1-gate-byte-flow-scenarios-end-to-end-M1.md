---
id: TASK-097
title: 'sesh U5 — M1 gate: byte-flow scenarios end-to-end (M1)'
status: In Progress
assignee:
  - sesh-scaffold-buro
created_date: '2026-07-09 05:28'
updated_date: '2026-07-09 06:32'
labels:
  - sesh
dependencies:
  - TASK-095
  - TASK-096
priority: high
ordinal: 97000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement (test harnesses). Orchestrator-owned shared ground. Deliverable: hermetic scenario harnesses proving the walking skeleton on a real machine — tests/check-s1-backfill.sh, check-s3-truncation.sh, check-s4-move.sh, check-s5-deletion.sh, check-s9-replay.sh + shared tests/lib.sh. House style: mktemp state dirs, fixture session trees, real sesh serve on ephemeral loopback port, real sesh ship run to quiescence, assertions by byte-compare + store-DB queries. Permanent regression gate, not one-off demos. Include both kill-and-restart checks (shipper mid-file; store mid-PUT).

Read first: /home/grace/Coding/ai-config/napkins/sesh-build/playbook.md, plan U5 section (git show 05dfc47:docs/plans/2026-07-09-001-feat-sesh-session-service-plan.md). Thread: sesh-u5. M1 is declared by the orchestrator only after these run green on this machine.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Five scenario scripts + both kill-and-restart checks each print ALL GREEN
- [ ] #2 Harness is idempotent: full suite green twice back-to-back
<!-- AC:END -->
