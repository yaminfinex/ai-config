---
id: TASK-289
title: >-
  credential-path notice send_failed on every spawn from a long-lived forked
  seat
status: To Do
assignee: []
created_date: '2026-07-19 00:09'
labels:
  - herder
  - credentials
dependencies: []
priority: medium
ordinal: 288500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Two consecutive spawns from the standing orchestrator seat (a long-lived forked session) printed 'herder spawn: credential path notice=send_failed (automatic retries suppressed)' while the spawn itself succeeded fully (seat born, prompt delivered with receipt, credential issued and usable). The generation-keyed credential-path notice is the documented primary channel for a new seat to learn its credential path; recovery via 'herder credential path --guid' exists, so this degrades DX rather than correctness — but if it reproduces for other spawner classes, freshly spawned seats routinely boot without the notice. Investigate: why the notice send fails when the spawning seat is a legacy/forked sender (suspected same sender-verification class that requires --credential-file on prompted spawns from such seats), whether the notice should ride a derived sender like the compact continuation does, and whether 'retries suppressed' is the right posture. Reproduce first from a fresh seat to isolate the spawner-class variable.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Root cause of send_failed identified with evidence (not guessed)
- [ ] #2 Notice delivery works from long-lived forked spawner seats, or the refusal names a cause+remedy
- [ ] #3 Regression test covers the failing spawner class
<!-- AC:END -->
