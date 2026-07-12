---
id: TASK-157
title: >-
  compact-then: turn-end proof can never establish when target status reads
  unknown — investigate proof path, not timeout length
status: To Do
assignee: []
created_date: '2026-07-12 01:57'
labels: []
dependencies: []
priority: medium
ordinal: 156000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OWNER INCIDENT (relayed 2026-07-12, from another run): a compact --then was dropped fail-closed; that session self-diagnosed 'the sender could not prove my turn ended within its timeout, ~4 minutes between arming and compaction' and the owner asked to bump the window to 10 minutes. VERIFIED FACTS (2026-07-12): defaultThenTimeout has been 15 MINUTES (900000ms) since the feature was born (commit history: single introduction, never 4m) — a 4-minute arm-to-compact gap is well inside it, so timeout length was NOT the mechanism. The one real fail-closed on this machine (compact-then-a9fcee3d-655548.log, @viru, 2026-07-08) shows the actual failure class: TIMEOUT after the full 900000ms with last status=unknown, saw_active=false, event_proof=true — the sender could never READ the target's status, so no working-to-listening transition was observable and it correctly refused to inject blind. INVESTIGATE: (1) why a compacting session's status reads unknown to the detached sender (enrollment/tracker gap? status source race post-compaction restart?); (2) whether the event-history proof path should have caught it (event_proof=true yet never fired); (3) whether the failure should surface louder than a log file (it does print a manual herder send recovery line — good — but the compacting session's owner only learns by noticing dormancy). NOT the fix: blindly lengthening the default — a session whose status never resolves will time out at any length. If investigation shows a legitimate slow-path, owner is open to raising the bound (they suggested 10m believing it was 4m; it is already 15m). Session-practice lesson from the incident session, worth folding into the compact docs fix: arm compact --then as the LAST tool call of the turn so the arm-to-fire window stays short.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Root cause of unknown-status turn-end proof failures named with evidence
- [ ] #2 Fix or explicit wont-fix for the proof path; timeout default re-examined against findings (not blindly raised)
- [ ] #3 Failure surfacing assessed: dropped continuation should be discoverable without reading sender logs
<!-- AC:END -->
