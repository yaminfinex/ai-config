---
id: TASK-164
title: >-
  herder operator language: align living docs/help with the four-state session
  model
status: Done
assignee: []
created_date: '2026-07-12 12:19'
updated_date: '2026-07-12 13:53'
labels: []
dependencies: []
priority: low
ordinal: 163000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Living READMEs, command help, diagnostics, injected lifecycle doctrine, comments, and test labels still teach the retired two-state registry vocabulary (active/closed), which now misdescribes behavior: list default output is non-retired (seated AND unseated) but help says active records; cull writes an unseat but help/output say closed (an unseated session remains resumable and keeps its label lease — cull is not retire); enroll unseats stale seat claims but says retired/closed; send/notify help calls seated coordinate candidates ACTIVE. Replace with seated/unseated/retired/lost language matching actual behavior. Do NOT touch hcom agent-status vocabulary (active/listening) — that is a different state machine.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 List help states default output includes non-retired sessions and explains what --all adds
- [x] #2 Cull documentation and output say cull unseats a session; no implication of retirement or label release
- [x] #3 Enroll documentation and output say stale seat claims are unseated, not retired
- [x] #4 Send/notify coordinate-resolution wording says seated candidates; bus statuses keep their current terms
- [x] #5 Living README examples and injected doctrine agree with command behavior; help/output goldens updated; full contract battery passes
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged 058cd41 (worker commits 75ab144 + 97eb9cd). Wording-only four-state migration: 38 files, 137/136 lines — list help (non-retired default, --all adds retired+lost), cull unseat + label-lease/resume-continuity sentence, enroll unseat, send/notify/spawn seated candidates, README, injected doctrine (+33 bytes, golden counts verified arithmetically), test labels (RACE_LEGACY_VIEW -> RACE_UNSEATED_VIEW, one test rename with fragment-swept -run safety). Adversarial review (opus) verified every new claim against actual predicates/writes empirically; fix round closed the one P2 (reconcile duplicate-claim diagnostic said seated where predicate is non-retired — new wording that lied differently). Delta APPROVE with hermetic reproduction. hcom vocabulary untouched. Pre-existing legacy-v1 mapping disagreement surfaced by review -> appended to TASK-165. Gates 53/53 x2 + post-merge 53/53. Sweep: only pre-existing identifiers carried through reworded lines.
<!-- SECTION:NOTES:END -->
