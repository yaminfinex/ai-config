---
id: TASK-301
title: >-
  slim-down pass 1: deletion map — inventory every resolution/proof call-site in
  tools/herder
status: Done
assignee: []
created_date: '2026-08-21 04:40'
updated_date: '2026-08-21 04:48'
labels:
  - herder
  - slimdown
dependencies: []
references:
  - napkins/herder-slimdown-charter.md
  - docs/design/2026-07-17-registration-brittleness-memo.md
priority: high
ordinal: 300500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Wayfinding pass 1 of the herder slim-down run (charter: napkins/herder-slimdown-charter.md — deletion-first, owner-ratified 2026-08-20/21). Inventory every hcomidentity consumer (Resolve, CurrentEvidence, ResolveExactSessionPane, ...) and every stored pane/terminal-coordinate read used for resolution/proof/liveness in tools/herder, plus the named repair-ladder surfaces (enroll live.Verified gate, reconcile healing exceptions + re-confirm-gated backfill, adopt enroll-leg label-conflict, re-seat-in-place paths, compact self-location ladder). Each entry gets file:line + disposition: DELETE / REPLACE-with-occupant-probe / KEEP-PROVENANCE / KEEP-FENCE / UNCLEAR. Output: napkins/herder-slimdown-deletion-map.md. This map is what the verb-by-verb deletion tasks (pass 4) are cut from.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Exhaustive: every hcomidentity consumer and stored-coordinate resolution read in tools/herder listed with current-HEAD file:line
- [x] #2 Every entry carries a disposition; keep-list fences (charter decision 6) marked KEEP-FENCE, never DELETE
- [x] #3 UNCLEAR items and charter-contradicting findings collected for owner decision
<!-- AC:END -->



## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Deletion map delivered: napkins/herder-slimdown-deletion-map.md (HEAD ada6bf7). Wayfinder spot-verified three load-bearing citations (enroll.go:543 live.Verified gate; wait.go:180-182 raw stored-pane fallback; completion.go:433-456 hcom pane-copy policing) — all exact. Headlines: reconcilecmd and repaircmd delete ~100% (~1230 lines + tests); hcomidentity/launch_context.go ~85%; enroll's repair half, compact's self-location ladder, seatcompletion's fallback rungs all DELETE. Re-seat-in-place already absent from code (C5) — spec-only amendment. Observer already runs the occupant probe (C6) — new resolution is a generalization of shipped code. Charter-unanticipated findings C1-C8, notably: herder WRITES pane-id copies into hcom's DB (RepairLaunchContext is the sole hcom-DB write; completion polices agreement) so the copy problem spans two stores (C2); Resolve's 'live' pane signal is hcom's stored copy (C3); seatcred verification is woven into the deleted evidence plane at 7 call-sites (C1/U7). U1-U7 owner questions collected in the map §(b) — routed to owner via bus.
<!-- SECTION:NOTES:END -->
