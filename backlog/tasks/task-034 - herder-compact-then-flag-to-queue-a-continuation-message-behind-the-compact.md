---
id: TASK-034
title: >-
  herder compact: --then flag to queue a continuation message behind the
  /compact
status: To Do
assignee: []
created_date: '2026-07-08 00:59'
updated_date: '2026-07-08 01:19'
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

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 01:00
---
LIVE RESULT #1 (hera, naive two-line variant): FAILED — ordering inversion. The plain continuation line did NOT queue behind /compact: claude injects plain queued messages into the RUNNING turn at the next tool boundary (arrived mid-turn, consumed pre-compact), while slash commands hold until turn end. DESIGN IMPLICATION: --then cannot be a second paste line. Correct mechanism (experiment #2 in flight): detached post-compact BUS send — compact --then should fork a detached child that waits for compaction to finish (poll session state, not sleep) then herder-sends the continuation to the caller own bus name; hcom queue-until-deliverable (TASK-032 finding) makes the timing forgiving. ACs should be reread under this design.
---

author: hera
created: 2026-07-08 01:19
---
RESULT #2 (delayed bus send, 2026-07-08): FAILED — but at herder's resolution layer, NOT the design. The helper's `herder send w6554208c1918a12-1 '<continuation>'` errored pre-send: 'target w6554208c1918a12-1 (@zero) not found on bus'. Root cause: the orchestrator pane has THREE manual-enroll registry rows (@hera live, @vore and @zero stale from earlier sessions in the same reused pane, all still LIVE=working), and pane-id resolution picked a stale row. The hcom queue-until-deliverable leg was never exercised. Filed as its own bug (stale manual rows + ambiguous pane-id resolution). Design implication stands from result #1+#2 combined: --then must be a detached post-compact BUS send resolved to the live bus name (in-binary, using the compacting session's own verified identity — not pane-id re-resolution).
---
<!-- COMMENTS:END -->
