---
id: TASK-241
title: >-
  herder spawn/compact prompt sends carry a synthetic 'orchestrator' sender —
  worker replies reroute to the human seat
status: Done
assignee: []
created_date: '2026-07-15 09:00'
updated_date: '2026-07-15 12:10'
labels: []
dependencies: []
ordinal: 240500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field incident (twice in one day, evidence on the wire): herder spawn and compact prompt delivery posts bus messages from a synthetic sender name with no live bus row. When the spawned worker acks the prompt with --reply-to, hcom resolves the reply recipient of the dead/synthetic sender to the OWNER seat, so polite worker acks land on the human's desk as owner mentions. Workers who try to address the sender directly get 'not an active address' refusals.

FIX SHAPE: spawn/compact prompt sends must carry the SPAWNING session's real bus identity (the dispatcher, e.g. the orchestrator's verified own name — same identity-honesty class as the compact --then in-binary send doctrine), so replies route to the dispatcher, never the owner desk. Fail closed if the spawner has no verified bus row (state the refusal cause+remedy) rather than falling back to a synthetic name.

AC sketch: (1) spawn prompt message sender == spawner's live bus name, receipt-verified; (2) worker --reply-to on that message routes to the spawner, proven by test through the real reply-resolution path; (3) no synthetic sender literal remains in the send path; (4) spawner-has-no-bus-row refuses typed with remedy, no silent synthetic fallback; (5) red-first regression for the owner-desk reroute. Peer tool note: mission-control is intent-gating promotion desk-side (acks never promote) as defense in depth — that does not replace this fix.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged 6ec0417 (--no-ff, clean three-way union with the re-proof and launch-isolation merges), post-merge battery 61/61 green on main, pushed. The synthetic 'orchestrator' sender and its helper are DELETED tree-wide; every production bus send requires an explicit verified sender (spawn proves the spawner BEFORE any side effect; compact --then carries the armed sender without re-resolution; explicit send verifies the caller). Refusals typed cause+remedy, no fallback of any kind; degraded-but-legitimate dispatcher seats accepted via pane/terminal evidence (do-not-brick pinned at two layers, and live-verified post-merge: a herder send through the new gate from the field-repaired orchestrator seat delivered). Wire-honest evidence: tagged-dispatcher case proves --from stamps FULL verbatim + deliver:<FULL> receipt + @full reply routes only to the dispatcher; mock receipt now echoes the real sender (synthetic token residue removed); spawn contract passes from repo root (fixture cwd honesty). SCOPE-HONEST RESIDUAL (incumbent-proven, filed separately): bare --reply-to acks are broadcasts and can still reach the owner seat — this unit's guarantee is an ADDRESSABLE stamp + correct explicit-reply routing, not full desk protection. Reviews: incumbent opus fix-list(3)→delta APPROVE with full round-1 mutation battery re-run all-red; grok calibration fix-list→delta APPROVE (found the mock receipt residue + tagged fixture need independently).
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
author: hera
created: 2026-07-15 11:46
---
Incumbent review adjudication (binding on the DONE claim): the fix makes the prompt sender an ADDRESSABLE live bus name — a real narrowing — but does NOT close the incident fully: bare --reply-to acks (no @target) are broadcasts and still reach the owner seat. Residual filed as its own task (bare-reply-to broadcast); upstream candidate ledgered. This task's scope remains the synthetic-sender deletion + verified stamping.
---
<!-- COMMENTS:END -->
