---
id: TASK-126
title: 'registry: UpdateLocked multi-row batch refusal is not atomic'
status: In Progress
assignee: []
created_date: '2026-07-09 12:54'
updated_date: '2026-07-10 10:12'
labels: []
dependencies: []
priority: medium
ordinal: 126000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## Capture (from reviewer-tofu adversarial review of TASK-084, msg #31487, 2026-07-09)

Empirically demonstrated: the legacy-poison gate in write.go:113 runs per-row INSIDE the append loop, so a batch [healthy, legacy] returns LegacyV1AppendError AND leaves the healthy row's 195 bytes appended. Real caller affected: observer applyCandidates submits multi-row batches (unseat/reconfirm do full record copies carrying LegacyV1) — a sweep can report "refused N candidate(s)" while some rows actually landed. Not corruption (rows valid, lock held) but the error contract lies, and "file bytes unchanged on refusal" only holds when the poison row is first.

## Direction (reviewer's)

Run the legacy gate — ideally all row validation — as a pre-pass over the whole batch before writing any row: refuse-all-or-write-all under the held lock.

## Acceptance criteria

1. Failing test first: batch [healthy, legacy] → refusal with file bytes PROVEN unchanged (not just first-row-poison ordering).
2. Observer multi-row candidate path covered (sweep refusal leaves zero rows landed).
3. Full house gate green.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Dispatched 2026-07-10 with TASK-147 as one unit (@worker-gole, 5.6-high, branch task-126-batch-atomicity), brief napkins/run-herder-dx/task-126-147-brief.md.
<!-- SECTION:NOTES:END -->
