---
id: TASK-034
title: >-
  herder compact: --then flag to queue a continuation message behind the
  /compact
status: To Do
assignee: []
created_date: '2026-07-08 00:59'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 34000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
USER ASK (2026-07-08): "compact and queue an immediate next message? usually compact hits stop". Mechanics: herder compact already rides claude queued-message system (the /compact line queues and fires at turn end). A second queued line should fire immediately after compaction completes, turning compact-then-stop into compact-then-continue. hera is live-testing the raw two-line variant on its own pane at filing time — result recorded in run-log + a comment here. Design: herder compact <steer> --then <continuation prompt> — same bootpaste engine, second paste+Enter after the compact line, same self-pane identity ladder, TASK-024 evidence gating per line. Open questions: does compaction preserve the TUI-level queued message (expected yes — queue is TUI state, not context state); ordering guarantees; codex parity (codex compact semantics differ — claude-only first); failure mode if line 2 lands but line 1 did not (must not fire the continuation into an uncompacted session — order verification matters).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 compact --then queues both lines with per-line delivery evidence (TASK-024 gating); continuation fires only if the compact line verified first — pinned in the compact suite goldens
- [ ] #2 Live smoke: real steered self-compact with --then, continuation message observed firing post-compaction, transcript evidence
- [ ] #3 compact --help + README + orchestrate skill context-discipline sections document compact-then-continue; claude-only scope stated
- [ ] #4 Pinned gate green (go vet/test + full battery)
<!-- AC:END -->
