---
id: TASK-207
title: >-
  Characterize flagship hcom crash/delivery semantics (claude+codex) — parity
  bar for pi delivery design
status: Done
assignee: []
created_date: '2026-07-14 07:51'
updated_date: '2026-07-14 21:13'
labels: []
dependencies: []
ordinal: 206000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OWNER-DIRECTED PREMISE RE-CHECK (2026-07-14) before pi design sign-off: the pi design's DR-2 custom delivery machinery (durable spool journal, settlement-correlated receipts, ownership epochs, driver lease, capability lanes) reaches a bar never verified for the flagship harnesses. claude/codex launch THROUGH hcom's native integration (hooks) with none of that machinery, and the fleet has run happily on it. Owner concern: 'a ton of custom machinery to reach a bar we don't have for our flagship harnesses'. RESEARCH unit, no production code changes. QUESTIONS: (1) Run the SAME crash probe the hcom-native-pi characterization ran (docs/design/2026-07-14-hcom-native-pi-characterization.md — mirror its method for comparability) against disposable claude and codex sessions under an ISOLATED HCOM_DIR: when does hcom advance the read cursor relative to turn settlement? Kill the agent mid-turn after delivery — is the message replayed on resume/restart, stranded-in-transcript, or lost? Idle wake, busy delivery, ordering, duplicate behavior — fill the same evaluation table. (2) Parity analysis: four-column table (native-claude, native-codex, native-pi as probed, pi DR-2 as designed) over delivery/crash/recovery/authority properties — which DR-2 properties exceed the bar the flagships actually meet? (3) Costing: what would 'herder wraps hcom pi exactly like it wraps hcom claude/codex' require (spawn.go launch-through-hcom path), what DR-2/DR-3 machinery does flagship-parity delete, what is genuinely kept (credential scoping in env construction is RETAINED per owner ruling — not up for debate)? DELIVERABLE: dated characterization memo in docs/design/ (same shape/honesty rules as the pi probe record: report only what evidence shows, version-pinned claims), with a recommendation section for the owner ruling on pi design scope. HARD CONSTRAINTS: isolated HCOM_DIR + scratch project dir mandatory — NEVER touch the live hcom database, live registry, or any live fleet seat; probe sessions are disposable ones you spawn inside the isolated bus. Live vendor homes acceptable for auth per the default-homes ruling (hygiene not quarantine). No agent names or task numbers in the durable memo.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Crash probe executed against real claude and real codex under isolated HCOM_DIR, mirroring the pi probe method; cursor-advance timing and post-crash replay behavior established with evidence for both
- [x] #2 Four-column parity table (native-claude, native-codex, native-pi, DR-2 design) over delivery/crash/recovery/authority properties, each cell evidence-backed or marked unverified
- [x] #3 Flagship-parity costing for pi: what herder-wraps-hcom-pi requires, what DR-2/DR-3 machinery it deletes, what is retained (credential scoping stays)
- [x] #4 Dated identifier-free memo merged-ready in docs/design/ with an explicit recommendation for the owner ruling
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
MERGED 4dd8848 (docs-only, memo ba89866). DECISIVE: flagships share native-pi crash window via shared commit_delivery_ack (claude.rs:135/codex.rs:427, ack at hook injection pre-settlement); reproduced SIGKILL strand both harnesses; codex battery full (115ms idle wake, mid-turn busy injection, batch ordering, burst dedupe). Parity table: flagships match native-pi on all five delivery/recovery rows; epochs/lease/lanes absent from ALL native integrations. Costing: flagship-parity pi = few launch-contract lines, deletes DR-2 delivery machinery. Recommendation: flagship parity unless non-delivery properties independently required. Seat continuity: first prober (codex) stalled on usage limits after decisive claude column; second seat (opus) verified from persisted artifacts + ran codex battery. One gate fix round (memo aligned to default-homes ruling). Owner ruling next; gold-plating audit dispatches on this evidence.
<!-- SECTION:NOTES:END -->
