---
id: TASK-029
title: 'upstream tickets: file issues where an upstream fix collapses local complexity'
status: To Do
assignee: []
created_date: '2026-07-07 12:31'
updated_date: '2026-07-08 05:04'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 29000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
USER DIRECTIVE (2026-07-07): as we close pieces of work, capture anything worth an upstream ticket when a fix there collapses complexity here. This task is the ledger + the eventual filing pass. Candidates accumulated so far (append at every unit closeout):

(1) hcom — user developer_instructions STRIPPED on codex resume/fork (flagship example). hcom re-adds only its own bootstrap; the launch-args seam cannot deliver there. Cost to us: TASK-014 merge-into-last launch hack, TASK-017 entire post-boot bus-delivery mechanism, TASK-027 residual, and the mirrored strip predicate that TASK-028 must re-audit every hcom upgrade. Upstream ask: preserve/merge user developer_instructions across resume/fork, or expose a supported per-agent bootstrap-extension seam (overriding/extending hcom system prompts).

(2) hcom — codex sessionstart is a no-op (no SessionStart-equivalent seam for codex). Forces the -c developer_instructions= ride-along for fresh launches. Possibly the same fix as (1): one sanctioned injection point.

(3) hcom — print-mode (claude -p) one-shots become persistent background agents. TASK-010 recorded option (d) "upstream patch" as skipped (3 coordinated changes fighting deliberate design); we carry the HCOM_LAUNCH_INFLIGHT bypass instead. Upstream ask: native print-mode passthrough; would let us delete the bypass + its goldens.

(4) Claude Code (not hcom) — alarming ".claude.json not found / backup exists / restored" triple when CLAUDE_CONFIG_DIR is re-rooted; reads like data loss, is cosmetic. Draft UX feedback already written in TASK-011 DONE report, unfiled.

(5) hcom minor — replying to an inform with --intent ack is rejected; forces intent=inform for acknowledgements (ergonomics only, may be by-design).

Doctrine: NOTHING is filed externally by agents — drafts are prepared here, the user reviews and files.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Each candidate above (plus any appended later) gets an explicit verdict in notes: file / do-not-file / superseded, with one-paragraph rationale tied to what local complexity it would collapse
- [ ] #2 For every FILE verdict: ready-to-paste issue draft (title, repro, current local workaround, concrete ask) stored in the task or a linked napkin — nothing submitted externally; user files
- [ ] #3 Candidates cross-checked against the hcom version current at execution time (coordinate with TASK-028 — an upgrade may moot or reshape asks (1)/(2)/(5))
- [ ] #4 Standing practice recorded in the orchestrate skill or run playbook template: unit closeout includes an upstream-candidate sweep
<!-- AC:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-07 12:36
---
CANDIDATE (6) — hcom events sub UX (hera field report, 2026-07-07): (a) `--once` reads like "block until one event" (tail -f expectation) but means "auto-remove subscription after first match" — the command always returns immediately and notifies later via a bus message from [hcom-events]; an agent that wraps it in background execution misreads process exit as the event firing (happened live). (b) "historical matches: N" on create is ambiguous — unclear whether a historical match consumes a --once subscription or only fresh events do. (c) subscriptions stack silently — re-arming without unsub yields duplicate notifications per event (3 live pings from one idle event observed). Asks: sub-specific --help lead line "returns immediately; notification arrives as a bus message", rename/alias --once or document it as auto-unsub, state historical-match semantics on create, dedupe-or-warn on identical filter subscription.
---

created: 2026-07-07 20:55
---
CANDIDATES (7)+(8) — from Unit R phase A (TASK-032 map, live-probe evidence): (7) hcom — dirty-composer starvation is SILENT: a bus message to an agent whose composer holds unsubmitted text queues indefinitely with no receipt, no error, no timeout event, BOTH families (probes vila/keto; reviewer-kimi starved 8h). Ask: an hcom-side "delivery blocked: composer holds a draft" event/receipt — would have named the state in seconds. (8) codex TUI — boot-window input is lossy (Enter-swallow, head-clipping of early pastes); moot for herder post-B1 (bus-first spawn delivery) but still the physics under any remaining TUI-paste user.
---

created: 2026-07-07 21:30
---
CANDIDATE (9) — from Unit R phase B (TASK-032): hcom lacks an "await receipt of message X" primitive — herder reconstructs delivery receipts by polling the event stream, and ALL THREE reconstruction layers were live bugs (receipt query keyed to the wrong side: receipts live on the RECEIVER instance as deliver:<SENDER>; --after boundary excluded same-second receipts; live events emit JSONL while the parser expected a JSON array — masked by mock-shape drift). A first-class receipt-await (send returns a receipt handle, or events exposes await --msg-id) would delete the whole heuristic class.
---

author: hera
created: 2026-07-08 03:28
---
Candidate 10 (wave 7, 2026-07-08): hcom list <name> --json returns a SINGLE object keyed by the BASE name (not an array, not the full scoped name). This surprised two independent implementations in one night (compact --then pickStatus live bug, fixed 2a434fd; mock-shape divergence). Upstream ask: document the single-object/base-name contract in --json help, or emit an array consistently. Candidate 11 (wave 7): codex boot-to-bus-join latency exceeded 60s twice; if hcom's launch path contributes measurable startup cost for codex, a changelog note or a faster join would collapse herder's TASK-036 workaround. (TASK-036 unit is measuring; fold its finding in before filing.)
---

author: hera
created: 2026-07-08 04:05
---
Candidate 12 (Unit Y measurement, 2026-07-08): codex roster entries omit launch_context.pane_id (claude publishes it; codex carries only process_id — verified on fully-booted sessions). This defeats herder's fast child-correlation for codex entirely: initial-prompt bind, sidecar pane-correlation, and recovery all degrade to async tag+cwd-independent enrichment that lags minutes under load. Upstream fix (publish pane_id for codex like claude) collapses the class: TASK-036's recovery affordance, the deferred redeliver verb, and the structural codex bind_timeouts all stop being needed. Strengthens/absorbs candidate 8.
---

created: 2026-07-08 05:04
---
vibe (herdr-0.7.3 audit, bus #5629, applied by hera): Reverse-direction entry: herdr 0.6.10->0.7.3 shipped fixes that collapse local complexity (stable ids #569, pane move #299, send-keys combos #613, session.snapshot, api schema, worktree #729, identity fixes #620/#684/#943). The four audit tasks TASK-047..050 enumerate the collapse work; when closing them, check whether any of our previously-planned upstream tickets are now moot.
---
<!-- COMMENTS:END -->
