---
id: TASK-159
title: >-
  compact-then: failed detached continuations are invisible — persist lifecycle
  and surface unresolved failures
status: Done
assignee: []
created_date: '2026-07-12 06:52'
updated_date: '2026-07-12 09:14'
labels: []
dependencies: []
priority: medium
ordinal: 158000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
From the same investigation: a dropped continuation is discoverable ONLY by reading the detached sender's log file. The arming process exits 0 printing a future log path; the timeout and the (good) manual recovery command land only in that file; nothing is projected into herder list, no status command inventories pending/failed continuations, no observer finding is emitted; the compacted session just sits dormant. FIX: persist the detached sender lifecycle (armed/delivered/queued/failed+recovery) under herder state; expose unresolved failures through a routine surface (herder list and/or observer finding); success and queued delivery close the record silently.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A timeout or exhausted send budget creates a durable failed-continuation record: target, timestamp, reason, log path, manual recovery command
- [x] #2 herder list or an equally routine command shows unresolved failures without scanning logs
- [x] #3 Observer emits a finding for an unresolved failure; cleared on recovery, never on mere restart
- [x] #4 Success and queued-delivery paths close the record without false warnings
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
SHIPPED, merged 1079b9b (branch task-159-continuation-surfacing, commits 1dbc9bf + 1031277). New internal/continuationstate package: detached sender lifecycle (armed → delivered/queued/failed with shell-safe recovery command) persisted under herder state — never registry rows; state-write failures degrade to log-only and provably never block delivery (failure injected at the real call site in tests). Bare herder list renders an UNRESOLVED DETACHED CONTINUATIONS block; herder list --ack-continuation ID is the documented explicit clear (marks, never purges; idempotent re-ack); --json carries additive unresolved_continuations; observer emits GUID-scoped findings from durable state. Delivered/queued/acked records archive out of the hot scan (crash-safe between mark and move). Adversarial review (opus) round 1 REQUEST-CHANGES caught two Mediums: agent-specific findings broadcast onto EVERY list row (empty-GUID → GlobalFlags), and one poison/foreign .json suppressing ALL failure surfacing. Both fixed and verified under attack in the delta (unique-seated-match resolution, no-broadcast-on-ambiguous; per-record skip-and-warn on stderr with goldens); new list-contract scenarios pin table/json/ack surfaces. Delta APPROVE with three Low residuals filed as follow-up (retired-sibling name-reuse suppresses the per-row observer hint; --json empty-registry gap; row-attachment golden) — the primary top block surfaces failures in every attacked scenario. Gates: independent battery 53/53, re-gate after fix round 53/53, post-merge battery on main.
<!-- SECTION:NOTES:END -->
