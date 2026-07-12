---
id: TASK-170
title: 'grok: first-class herder/hcom family (design first)'
status: In Progress
assignee: []
created_date: '2026-07-12 21:03'
updated_date: '2026-07-12 23:32'
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
- [x] #1 A reviewed design fixes the receipt state machine, persistence/recovery boundary, bridge ownership, parent/subagent identity rules, and hcom-vs-herder responsibility before code
- [ ] #2 herder spawn --agent grok launches first-class: tool:grok, pinned GROK_HOME, session id captured, doctrine via --rules; autonomy modes and --model explicitly mapped and tested
- [ ] #3 Receipt/recovery tests cover initial, idle, busy, duplicate, out-of-order, bridge-restart, auth/rate-failure, compaction, resume, fork, subagent lifecycle; delivered only on correlated monitor wake + message-id ack
- [ ] #4 Observer/transcript honest (unknown stays unknown); shim/setup/doctor land only with working launch; isolated live smoke proves bidirectional messaging; cross-family adversarial review + full gates
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
DESIGN PHASE COMPLETE, merged (docs/design/grok-first-class-design.md, 817 lines, commit 0988318 via --no-ff). Claude designer + codex 5.6 adversarial design review, FOUR fix rounds, final APPROVE — every round grounded in independent scratch-bus reproduction against real hcom 0.7.23: (r1) listen is destructive/envelope-poor -> pickup respecced onto the events surface; (r2) events --wait 10s-lookback loss, named-read post-dispatch drain, self-broadcast predicate bug -> anonymous --full drain as sole durable primitive, identity-free reads, canonical delivered_to; (r3) newest-first LIMIT 20 default silently drops backlog >20 -> oldest-page-above-C subselect; (r4) page membership != emission order -> mandatory ascending-id sort before journal append. Also enforced: --no-subagents always-on, flock+generation fencing, resolved-binary capability gate (refuses the 0.2.99 the PATH points at today), activation-gated staging U1-U5. IMPLEMENTATION NOT STARTED — awaiting owner rulings on design section 10 (bypassPermissions mapping, model pin, per-unit smoke spend, conditional upstream hcom niceties a/b/c, conditional boot-arming fallback). U1 (transport core) is dispatchable on owner go.
<!-- SECTION:NOTES:END -->
