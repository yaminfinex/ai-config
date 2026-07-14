---
id: TASK-209
title: >-
  Gold-plating audit — grok (shipped) + pi (designed) delivery/authority
  machinery vs the flagship bar and self-healing orchestration
status: Done
assignee: []
created_date: '2026-07-14 21:03'
updated_date: '2026-07-14 21:25'
labels: []
dependencies: []
ordinal: 208000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OWNER-DIRECTED (2026-07-14): full complexity review of BOTH the grok family's SHIPPED machinery (MCP bridge, spool journal, receipt/fetch-ack state machine, wake line, session preservation remnants) and the pi design's machinery (DR-2 spool/settlement receipts/epochs/driver lease/capability lanes, DR-3 extension gating) to ensure we are not building gold-plated infrastructure. Owner premise: agent-driven systems are somewhat SELF-HEALING — the orchestration layer notices silent/stalled workers and re-prompts; the flagship harnesses (claude/codex) run on native hcom with none of this machinery and the fleet runs happily. RESEARCH/DESIGN-REVIEW unit, no production code changes; deliverable is an audit memo. GROUND TRUTH INPUTS: the flagship crash/parity characterization memo (in progress — this unit dispatches AFTER it merges; it establishes the empirical bar: e.g. claude verified to share native-pi's crash window, mid-turn busy injection, no replay) and the hcom-native-pi characterization (docs/design/2026-07-14-hcom-native-pi-characterization.md). METHOD — categorize every mechanism by FAILURE CLASS, because self-healing only covers some classes: (a) LIVENESS failures (message stalls, crash-without-replay, hung driver) — self-healing at the orchestration layer covers these; machinery guarding only these is the prime gold-plating suspect; (b) INTEGRITY failures (stale seat writing as the live one, duplicate/replayed injection corrupting a turn, cross-seat delivery) — NOT self-healing, silent corruption; machinery here needs a real verdict, not a blanket delete; (c) AUTHORITY/SECURITY (capability lanes, credential scoping — scoping is RETAINED per owner ruling, settled); (d) OBSERVABILITY honesty (refuse-to-claim vs report-wrong). For each mechanism in grok-shipped and pi-designed: what failure class, does the flagship bar have it, does self-healing cover it, measured/estimated cost (code size, maintenance, launch latency, review burden), recommendation KEEP/SIMPLIFY/DELETE with migration cost (grok = shipped code changes; pi = design amendment before any build). ALSO CHECK: does hcom natively integrate grok now (the same §0 onboarding question pi got — if yes, the custom grok bridge deserves the same native-vs-custom scrutiny). Deliverable: dated identifier-free audit memo in docs/design/ with a per-mechanism verdict table + filed-ready simplification candidates for the owner ruling. Settled, do not relitigate: default-homes ruling, credential scoping retained, delivery-decision authority is the owner's.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every grok-shipped and pi-designed delivery/authority mechanism enumerated with failure class, flagship-bar comparison, self-healing coverage, and cost
- [x] #2 Per-mechanism KEEP/SIMPLIFY/DELETE verdict table, evidence-cited, with migration cost for shipped grok code and amendment scope for the pi design
- [x] #3 hcom-native grok integration status checked and the native-vs-custom question answered for the grok bridge
- [x] #4 Dated identifier-free memo merged-ready in docs/design/ with filed-ready simplification candidates for owner ruling
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
MERGED (docs-only, head 1003404). Verdict split: GROK = parity plumbing, not gold-plating (hcom 0.7.23 has NO grok integration — verified at gate; drain/spool/fencing/identity guard reproduced class-b failures, no native substitute); genuine fat = idle-nudge machinery (PROVEN INERT on launched path — launch never passes --session-events; verified at gate binder.go:178 + grok.go:489) and ~200-line manual-guest path. PI = DR-2/DR-3 almost entirely above the flagship bar; DELETE via flagship-parity design amendment (zero migration, nothing built); settlement receipt honestly noted as closing a real report-wrong window with the native-fork as cheap hedge. Addresses the keep-custom characterization decision squarely (its decisive leg cites properties no flagship has). 4 filed-ready candidates for owner ruling. Gate: orchestrator full line-level read + independent verification of both decisive claims; identifier sweep all-FP (rato-in-orchestrator/operator class).
<!-- SECTION:NOTES:END -->
