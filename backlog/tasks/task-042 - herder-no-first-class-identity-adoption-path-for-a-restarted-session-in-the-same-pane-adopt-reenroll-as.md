---
id: TASK-042
title: >-
  identity adoption for a restarted session: respec as enroll (new guid) +
  rename --take-from + retire — guid reuse violates spec D1
status: In Progress
assignee:
  - hera-run
created_date: '2026-07-08 04:45'
updated_date: '2026-07-12 09:22'
labels: []
dependencies: []
priority: medium
ordinal: 42000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
A restarted session in the same pane needs a first-class identity-adoption path. FROZEN DOCTRINE (spec D1 / §3.1-1, ratified with owner clarification): a restarted process is a NEW transcript and must get a NEW guid — resume keeps the guid (same transcript continuing), only fork, /clear, and restart-replacement mint new guids. The legal composite for the restart case is: herder enroll (new guid) -> transfer the label from the old row -> retire the old row. Never re-key a guid to a new transcript (the original description of this task proposed adopt-same-guid; that design is DROPPED as spec-illegal).

REMAINING SCOPE after the 0.7.3 re-run and TASK-071 (retire/reopen shipped): (1) an adopt convenience wrapper — herder adopt <old-target>, run from inside the new pane, composing the existing verbs (enroll new guid, retire old row, rename to reclaim the label), reclaiming or verifying the hcom bus name (hcom start --as), never re-keying a guid; (2) enroll label-uniqueness UX — enroll refuses a label held by a DEAD row (unseated + live_status=gone) with the same message as an active holder; it must distinguish, and either point at retire or (safer) refuse with the exact retire+rename recovery sequence named. rename --take-from stays wave C (atomicity convenience only — un-entombing already works via retire + plain rename).

Live-hit history and the narrowing trail are in the comments; the two items above are the whole remaining scope.
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 05:04
---
vibe (herdr-0.7.3 audit, bus #5629, applied by hera): Upstream #620/#684/#943/#712/#765 (see TASK-050) land directly on this task's territory. Re-run the restart repro on 0.7.3 before designing adopt/reenroll-as; the herdr-side identity half may now work, leaving only the herder-registry adoption affordance to build.
---

created: 2026-07-08 05:29
---
RESPEC per spec D1 / §3.1-1 (spec-ravu #6043, hera concurs — flagged in the spec review as flag 2): drop the adopt-same-guid design; a restarted process is a NEW transcript and must get a new guid. The composite affordance is: herder enroll (new guid) -> rename <new> <label> --take-from <old> (explicit lease transfer) -> retire <old>. Today's live runbook (guid 404a13df reused for hera's new transcript) was expedient but spec-illegal; do not repeat post-ratification. Wrapping the composite as a single 'herder adopt' convenience remains open — but it composes the three verbs, never re-keys a guid.
---

created: 2026-07-08 05:55
---
Ratification note (#6423): identity rules confirmed with owner clarification — resume KEEPS the guid (same transcript continuing, AC-11); only fork, /clear, and restart-replacement mint new guids. The enroll + rename --take-from + retire composite on this ticket is now frozen doctrine for the restart case.
---

created: 2026-07-08 10:19
---
0.7.3 re-run complete (TASK-050 controlled restart, replacement session bbbc84c2). Scope confirms the audit prediction: the herdr-side identity half now works — fresh launch identity from the shim (no stale env inherited), dead bus row dropped by hcom 0.7.23, hcom start --as clean — so this task shrinks to the REGISTRY-SIDE adoption affordance, which is missing in full: (1) rename has no --take-from (help: a taken label requires culling the holder first); (2) retire absent (unknown command, wave C); (3) enroll label-uniqueness treats a DEAD row (unseated + live_status=gone) as "active" and refuses; (4) the cull escape hatch is broken — pane_not_found path writes no closed record even with --force (TASK-069) — so a dead agent's label is permanently unreclaimable today. Live consequence: the standing orchestrator now runs as label hera-restart-050b instead of hera. The frozen composite (enroll new guid -> rename --take-from -> retire old) remains the right shape; TASK-069 is a prerequisite or co-fix.
---

created: 2026-07-08 11:19
---
Spec-ravu ruling #11678 on the label-entombment blocker (surfaced by TASK-069 review): cull --retire variant REFUSED at steward level (seat-verb/session-verb separation; second write path to a terminal state; would need owner-blessed spec amendment). Resolution: retire + reopen pulled forward as unit C0 = TASK-071 (no spec change, pure sequencing). rename --take-from explicitly stays in wave C as an atomicity convenience — un-entombing needs only retire + existing plain rename. This task's remaining scope narrows further: the adopt composite convenience wrapper, and the enroll label-uniqueness UX against unseated holders.
---
<!-- COMMENTS:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 herder adopt (or the agreed composite wrapper) takes a restarted session from fresh-pane state to: new guid seated with live coordinates, old row retired, label reclaimed, bus name reclaimed-or-verified — one command, guid never re-keyed
- [ ] #2 enroll refusal against a label held by an unseated/dead row names the holder state and the concrete recovery steps (distinct from the active-holder refusal)
- [ ] #3 suite covers the composite happy path and both refusal shapes
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
LIVE VALIDATION (2026-07-12, hera on own row): the exact composite this task wraps was run manually — retire <old-labeled-row> then rename <live-guid> <label> — and worked cleanly (label released, reassigned, herdr terminal renamed to match). The orchestrator itself was the specimen (label stranded on a dead TASK-050-era row while the live session ran unlabeled). Confirms the adopt wrapper design is sound and needed: the manual sequence requires knowing both guids and the verb order; the wrapper + dead-label enroll UX is the whole remaining scope.

A1 merge (a1c5acd) live-validates the composite repair path this task designs around: retire+rename executed live on the orchestrator's own row 2026-07-12, and A1 added re-enroll-same-guid (SID-corroborated) as the bus-name rebind affordance. Label verbs remain this task's scope; identity evidence plumbing (hcomidentity package, multi-correlate) now exists to build on.
<!-- SECTION:NOTES:END -->
