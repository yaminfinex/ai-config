---
id: TASK-159
title: >-
  compact-then: failed detached continuations are invisible — persist lifecycle
  and surface unresolved failures
status: In Progress
assignee: []
created_date: '2026-07-12 06:52'
updated_date: '2026-07-12 08:33'
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
- [ ] #1 A timeout or exhausted send budget creates a durable failed-continuation record: target, timestamp, reason, log path, manual recovery command
- [ ] #2 herder list or an equally routine command shows unresolved failures without scanning logs
- [ ] #3 Observer emits a finding for an unresolved failure; cleared on recovery, never on mere restart
- [ ] #4 Success and queued-delivery paths close the record without false warnings
<!-- AC:END -->
