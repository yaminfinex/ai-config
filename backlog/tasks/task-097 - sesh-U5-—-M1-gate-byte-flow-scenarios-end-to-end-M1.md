---
id: TASK-097
title: 'sesh U5 — M1 gate: byte-flow scenarios end-to-end (M1)'
status: Done
assignee:
  - sesh-scaffold-buro
created_date: '2026-07-09 05:28'
updated_date: '2026-07-09 06:44'
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
- [x] #1 Five scenario scripts + both kill-and-restart checks each print ALL GREEN
- [x] #2 Harness is idempotent: full suite green twice back-to-back
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged to sesh-build @ 89c0d78 (ff). M1 GATE VERIFIED BY ORCHESTRATOR OWN RUNS: all five scenario harnesses + surface harness ALL GREEN twice back-to-back on this machine, plus module gates fresh. Kill-and-restart both sides in-suite (shipper SIGKILL mid-file resumes to parity; store kill -9 mid-PUT leaves no state or exact prefix, replay converges to byte parity). Doors accepted: quiescence asserted via recovery GET + registry stability over 1s rather than PUT counting; s9 mid-PUT accepts both spec-correct durable outcomes. tests/dbq module-local SQL helper (no sqlite3 CLI on machine). Trail: thread sesh-u5.
<!-- SECTION:NOTES:END -->
