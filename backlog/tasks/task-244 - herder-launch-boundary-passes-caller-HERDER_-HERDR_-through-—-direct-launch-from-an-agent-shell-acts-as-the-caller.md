---
id: TASK-244
title: >-
  herder launch boundary passes caller HERDER_*/HERDR_* through — direct launch
  from an agent shell acts as the caller
status: In Progress
assignee: []
created_date: '2026-07-15 11:28'
updated_date: '2026-07-17 02:19'
labels: []
dependencies: []
ordinal: 243500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Follow-up from the launch-env isolation unit (both reviewers converged; explicitly out of that unit's settled scope, which covered HCOM_* only). The launch boundary drops all ambient HCOM_* but deliberately passes HERDER_*/HERDR_* through, relying on the managed spawn path pre-exporting child-minted HERDER_GUID/ROLE/LABEL into the pane. The exposed path is a DIRECT 'herder launch <tool>' from an identity-bearing agent shell: the caller's HERDER_GUID/HERDER_LABEL/HERDR_PANE_ID inherit into the child, which then acts AS the caller for guid-keyed surfaces (mission verb caller identification, lifecycle provenance, enroll). The codebase already treats inherited HERDER_GUID as a hazard elsewhere (grok launcher refuses it; compact refuses on stale/inherited guid shapes). Fix shape (design checkpoint first): the boundary scrubs HERDER_*/HERDR_* unless the launch path explicitly provides child-minted values (spawn does); direct launch either mints fresh identity or refuses with cause+remedy when it detects caller-inherited identity it cannot re-own. Must not break: managed spawn pre-export, sidecar, print bypass, grok identity minting. Note: the isolation unit's tests assert the passthrough with child-guid naming — they are being re-framed in that unit's fix round so this fix will not read as a regression.

WIRE-PROVEN SECOND VECTOR (2026-07-15, live incident): running herder SPAWN with HCOM_SESSION_ID=<caller sid> in the CLI env causes the spawned agent to HARVEST THE CALLER'S SID ONTO ITS OWN ROW (sids[] source=harvest) — the caller's sid then resolves to the child's row, and the caller's own identity-correlated verbs (compact) refuse with a registry-vs-live identity mismatch naming the CHILD's bus name. This is spawn-verb-side contamination (the harvest reads the spawning process env), distinct from child-env inheritance, same class: caller identity env writes into child registry state. Fix must cover it: the spawn/fork path must never attribute the caller's ambient session correlate to the spawned row. Operational guidance until fixed: never prefix HCOM_SESSION_ID on spawn/fork; enroll once, bare verbs after; prefix only when a bare verb refuses.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Direct launch from an identity-bearing shell cannot act as the caller (scrub, fresh mint, or cause+remedy refusal)
- [ ] #2 spawn/fork never attribute the caller's ambient session correlate (HCOM_SESSION_ID) to the spawned row — red-first fixture reproducing the harvest contamination
- [ ] #3 Managed spawn pre-export, sidecar, print bypass, grok identity minting unchanged
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
TASK-266 investigation code-verified the second vector OPEN at HEAD (d50acfa): registry BuildProvenance unconditionally stamps ToolSessionID from ambient HCOM_SESSION_ID on creator-flow child rows, and the v2 projection records it as SIDs[{source: harvest}] and upgrades Continuity to confirmed. The adjacent spawnedBy field already takes explicit values with a comment documenting exactly this hazard class — the reasoning was never extended to the sid one field below. Recommended contained fix (from the memo, endorsed by hera after verification): pass explicit values like spawnedBy does; do not wait for the full per-seat credential design (memo direction R3). Note the hazard doc's 'managed launches discard every ambient HCOM_*' covers the child ENV only — the child ROW still receives the caller sid via this harvest.

Contained fix dispatched 2026-07-17 as its own implement unit (second vector only: creator-flow ambient-SID harvest). Brief: napkins/run-herder-dx/builder-244-brief.md — design checkpoint required before code (call-site creator/self classification). First vector (HERDER_*/HERDR_* passthrough on direct launch) remains open, not in this unit's scope.
<!-- SECTION:NOTES:END -->
