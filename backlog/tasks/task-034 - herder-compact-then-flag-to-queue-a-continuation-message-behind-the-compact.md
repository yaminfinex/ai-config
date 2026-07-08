---
id: TASK-034
title: >-
  herder compact: --then flag to queue a continuation message behind the
  /compact
status: Done
assignee:
  - unit-w-kava
created_date: '2026-07-08 00:59'
updated_date: '2026-07-08 03:01'
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
- [x] #1 compact --then forks a detached sender that fires the continuation over the BUS to the caller's own verified bus name (never pane-id re-resolution); it waits for the current turn to END (session-state detection, not sleep) before sending, so the continuation cannot inject into the running pre-compact turn
- [x] #2 Ordering safety: the compact line's paste evidence (TASK-024 floor) is verified before the sender is armed; unverifiable compact line => --then aborts loudly, nothing sent
- [x] #3 Suite/golden coverage: compact suite extended for --then (armed/aborted/sent shapes), mocks emit live shapes
- [x] #4 Live smoke: real steered self-compact with --then on a live session; continuation observed arriving post-compaction with transcript evidence
- [x] #5 Docs: compact --help + README + orchestrate skill context-discipline sections document compact-then-continue; claude-only scope stated
- [x] #6 Pinned gate green (go vet/test herder+bottle + full 18-suite battery)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
compact --then is a detached, setsid-isolated post-compaction BUS send (herder compact-then, internal subcommand), NOT a second paste (experiment #1: a plain queued line jumps the /compact queue). It arms ONLY after the /compact paste verifies (TASK-024 floor), targets the caller's OWN bus name from the proven self row at compact time (never pane-id re-resolution — experiment #2). Turn end is PROVEN, never assumed from a delay: (a) an observed active->listening transition, else (b) an hcom event-history listening record strictly newer than a TRUSTED arm-time event-id watermark — maxEventID distinguishes empty history (trusted 0) from an unestablished snapshot (hcom error/garbage; bounded retries), and an unestablished snapshot DISABLES proof (b) rather than failing open; unproven turn-end FAILS CLOSED (drop > mid-turn inject). Delivery treats queued as success (never resent), retries not_joined/send_failed with settling backoff over the remaining --then-timeout budget, and caps the receipt window to the deadline. Default timeout 15m; diagnostics at <state>/compact-then/*.log. Claude-only. Live-verified TWICE (throwaway agents; POSTCOMPACT markers + isCompactSummary:true); live runs caught a real bug (hcom list --json single-object shape). Merged 735a18d; hera gates green four times (worktree R1-R3 + post-merge main, 18/18). Review: viro REQUEST-CHANGES x2 (grace fail-open, watermark fail-open, retry burn) => APPROVE.
<!-- SECTION:NOTES:END -->

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
