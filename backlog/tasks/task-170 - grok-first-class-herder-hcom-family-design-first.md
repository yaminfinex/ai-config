---
id: TASK-170
title: 'grok: first-class herder/hcom family (design first)'
status: To Do
assignee: []
created_date: '2026-07-12 21:03'
updated_date: '2026-07-12 22:46'
labels: []
dependencies: []
priority: medium
ordinal: 169000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Design, then separately implement, Grok as an explicit herder/hcom family. The hard boundary is honest inbound delivery: Grok 0.2.93 discards passive Claude-hook context, so the evidence-backed bridge is a silent persistent Grok monitor for wake plus MCP fetch/ack/pending/send — never treat Claude-hook registration as proof of delivery. Launch contract: pinned GROK_HOME, preassigned session id, --rules doctrine, --model, explicit permission mapping (--always-approve vs bypassPermissions is an owner ruling). Carry the family through registry/sidecar/observer/lifecycle/resume/fork/transcript (chat_history.jsonl, not hook transcriptPath)/shim/tests; no cwd-only identity (subagent SessionEnd stopped a parent in probes). hcom 0.7.23 has no grok launcher — decide hcom-vs-herder ownership in design. Ground truth: docs/design/grok-onboarding-memo.md + docs/grok-integration-characterization.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A reviewed design fixes the receipt state machine, persistence/recovery boundary, bridge ownership, parent/subagent identity rules, and hcom-vs-herder responsibility before code
- [ ] #2 herder spawn --agent grok launches first-class: tool:grok, pinned GROK_HOME, session id captured, doctrine via --rules; autonomy modes and --model explicitly mapped and tested
- [ ] #3 Receipt/recovery tests cover initial, idle, busy, duplicate, out-of-order, bridge-restart, auth/rate-failure, compaction, resume, fork, subagent lifecycle; delivered only on correlated monitor wake + message-id ack
- [ ] #4 Observer/transcript honest (unknown stays unknown); shim/setup/doctor land only with working launch; isolated live smoke proves bidirectional messaging; cross-family adversarial review + full gates
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
SETTLED DELIVERY DECISIONS recovered by archaeology (do not relitigate in design): (1) PTY paste is technically functional but OWNER-REJECTED as the delivery mechanism (characterization doc B4). (2) MCP polling + ack with resume fallback was chosen (901b49a), then SUPERSEDED on wake only: later monitor probes proved idle wake and busy-turn buffering, giving the final architecture = monitor-wake + MCP fetch/ack/pending/send. (3) Passive hook stdout, stop-hook exit-2 stderr, and hcom term inject are proven DEAD surfaces (mechanism matrix, docs/grok-integration-characterization.md:306). CAVEAT the design must close: monitor and MCP were proved SEPARATELY, never as one end-to-end receipt state machine — correlation/persistence/recovery is the genuinely open design work. Full lineage: napkins/run-herder-dx/grok-delivery-archaeology-memo.md; canonical record: docs/grok-integration-characterization.md (960 lines, recovery commits 1c3adbc + 140944d).

DEMO FINDINGS (falsify the claude-hook shortcut entirely — registration AND delivery must both be first-class): grok 0.2.93 hooks all exit 0 against real hcom 0.7.23 yet NO roster row/bus name is created (memo erratum in docs/design/grok-onboarding-memo.md). Environment truths for the design: raw-agent login shells reset HOME and drop spawn-time env (child-side injection required); hcom in herder panes resolves to tools/herder/shims/hcom routing through herder hook — design must decide which hcom hooks resolve to; grok settings env.HCOM override ineffective in 0.2.93, only direct child env export works; update suppression documented and proven (--no-auto-update, [cli] auto_update=false); never equate hook exit 0 with registration/delivery. Evidence: docs/design/grok-demo-report-2026-07-12.md.
<!-- SECTION:NOTES:END -->
