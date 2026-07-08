---
id: TASK-035
title: >-
  herder registry: reused panes accumulate stale 'working' manual rows; pane-id
  resolution picks the wrong bus name
status: Done
assignee:
  - unit-v-koda
created_date: '2026-07-08 01:20'
updated_date: '2026-07-08 01:53'
labels: []
dependencies: []
priority: medium
ordinal: 35000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Live failure (TASK-034 experiment #2, 2026-07-08): pane w6554208c1918a12-1 has three manual-enroll rows — @hera (live session), @vore, @zero (both from earlier sessions in the same pane, still marked LIVE=working). `herder send <pane-id>` resolved to stale @zero and errored 'not found on bus', when @hera was live and deliverable. Two defects: (1) liveness for manual rows appears pane-based, so a dead session's row stays 'working' forever once the pane is reused; (2) pane-id resolution doesn't disambiguate multiple matching rows — no prefer-bus-live, no newest-first guarantee, no ambiguity error. Fix directions: mark superseded manual rows gone on re-enroll of the same pane; resolution should prefer the row whose bus name is currently joined (hcom list), and error loudly on ambiguity instead of silently picking. Acceptance: reused-pane re-enroll retires prior rows; `herder send <pane-id>` with one live + N stale rows delivers to the live one; ambiguous-with-multiple-live errors with the candidate list; golden/suite coverage for the reused-pane case.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Re-enrolling a reused pane retires (marks gone) prior manual rows for that pane
- [x] #2 herder send <pane-id> with one bus-live + N stale rows delivers to the live one
- [x] #3 Multiple bus-live candidates for one pane-id errors loudly with the candidate list (no silent pick)
- [x] #4 Suite/golden coverage for the reused-pane resolution case
- [x] #5 Docs/help text updated if resolution semantics are user-visible
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Fixed reused-pane stale-row defects across all three positional-coordinate resolvers. (1) herder enroll retires (closes, reason=reenroll_same_pane) prior active rows on the same pane_id, guarded against herdr pane-id compaction: only when terminal_id agrees (or is absent) and the row isn't currently bus-joined — never closes a live relocated session (round-2 fix, review P1-b). (2) herder send and (3) herder spawn --notify share registry helpers (ActiveCandidatesByPaneOrTerminal + PickLiveCandidate): a lone candidate resolves exactly as before (bus-liveness is a tiebreaker, never a gate); >1 candidates => the sole bus-joined row wins. send hard-refuses ambiguity (exit 2, candidate list); notify warns and skips (best-effort, TASK-017 warn-never-block; round-2 fix, review P1-a). Live smoke on the incident pane: delivers to @hera where pre-fix silently picked stale @zero. Coverage: check-send-resolution.sh (new suite, battery now 18), enroll goldens reenroll_reused_pane/reenroll_compacted_pane, Go tests per resolver. Docs: send/enroll --help + README Delivery/notify. Merged 6d97deb; hera-verified gate green three times (worktree R1+R2, post-merge main). Review: naga REQUEST-CHANGES (2 P1) => APPROVE.
<!-- SECTION:NOTES:END -->
