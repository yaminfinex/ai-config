---
id: TASK-170
title: 'grok: first-class herder/hcom family (design first)'
status: To Do
assignee: []
created_date: '2026-07-12 21:03'
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
