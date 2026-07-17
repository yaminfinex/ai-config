---
id: TASK-279
title: >-
  herder caller identity verification fails for long-lived forked sessions
  (sender unverified despite functional bus)
status: To Do
assignee: []
created_date: '2026-07-17 16:42'
updated_date: '2026-07-17 22:25'
labels:
  - herder
  - identity
dependencies: []
priority: medium
ordinal: 278500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field class (third distinct identity-spine field report post seat-completion merge): a 1-day-uptime claude orchestrator session with a FULLY FUNCTIONAL bus (hooks+pty bound, sends deliver, messages arrive) is refused by herder verbs at CALLER identity verification:

- herder send → "sender identity is not verified: no joined bus row matches the provided name, session, process, or pane"
- herder enroll from the same session → "recording hcom_name as unknown ... seat completion refused [joined_bus_row_missing]"
- herder cull (earlier, same caller) → "release notice: skipped (caller bus identity unverified)" (cull proceeded; graceful-release path silently skipped)

Observed coordinates at failure time: hcom db row EXISTS and is live (base name + orchestrator tag composite display name; session id matches what the operator reports; pane matches the registry row; cwd is a worktree, not repo root). herder reconcile dry-run on the same registry row returns D11 conflict: "stored terminal is live as name=<label>-fork-<guid>" — terminal detection names the live pane under a fork-derived identity while the registry label is the base label.

Hypotheses to investigate (not conclusions):
(a) caller env drift over long uptime — stale HCOM_SESSION_ID/pane/process env in the caller shell after compaction/fork, so every env-provided correlate misses the current db row (fail-closed then correct but unrecoverable: enroll ALSO fails, leaving no self-service path);
(b) composite-vs-base name mismatch — caller env carries the tag+name display form while the db row stores the base name; the stored-side matcher accepts Name-or-BaseName but the CALLER verification arm may not normalize the same way;
(c) fork lineage — terminal detection reports a fork-derived agent name; whatever minted that identity may have rotated the coordinates the caller's env still carries.

Impact: long-lived orchestrator sessions progressively lose herder send/cull-notice/enroll. Workaround in the field: raw bus delivery instead of herder send. Related-not-same: the send TARGET-key gap (hcom_name as resolution key) is separately boarded; the shared-liveness unit of the identity migration plan covers the epoch/coordinate-drift design dimension — this task is the tactical repro+fix.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Reproduce the refusal with a caller whose env coordinates are stale/rotated while its bus row is live (fixture world); characterize which correlate misses and why
- [ ] #2 Caller verification accepts the same Name-or-BaseName normalization as stored-side matching, or documents why not
- [ ] #3 A live-bus caller refused at verification has a working self-service recovery path (enroll must not fail the same way for a session with a live joined row)
- [ ] #4 Reconcile D11 fork-name conflict on the same row explained: why terminal detection reports a fork-derived name for an unforked-looking seat, and whether that is this defect or a separate capture
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Scope-check evidence from the bind-recovery unit: caller/sender verification resolves ONLY session/process/pane evidence (name is compare-after, never a lookup key) — hypothesis (b) composite-vs-base name mismatch is ELIMINATED for the caller arm. Remaining hypotheses: (a) all ambient correlates stale via long-uptime env drift, (c) fork-lineage coordinate rotation (matches the D11 fork-name terminal detection). Recovery-path AC unchanged and now sharper: enroll must succeed for a live-bus caller even when every env correlate is stale.
<!-- SECTION:NOTES:END -->
