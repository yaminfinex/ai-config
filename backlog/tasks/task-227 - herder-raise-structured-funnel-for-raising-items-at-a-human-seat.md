---
id: TASK-227
title: 'herder raise: structured funnel for raising items at a human seat'
status: In Progress
assignee: []
created_date: '2026-07-15 05:02'
updated_date: '2026-07-15 05:03'
labels:
  - herder
dependencies: []
priority: high
ordinal: 226500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner directive 2026-07-15 (SUPER HIGH): agents need a deliberate, well-formed way to open an item on the owner's desk. Add a raise verb to herder (comms is herder's domain; mission-control holds the seat and renders; mish resolves missions — the raise funnel is transport).

Verb shape (design fully adjudicated by owner — do not relitigate; decision record: ~/Coding/missions/missions/2026-07-15-mission-control/artifacts/raise-doctrine-design.md):
  herder raise --context '<cold-open sentence(s)>' --expects decide|act|reply|read [--thread <slug>] [--mission <slug>] -- '<body>'

Settled decisions:
- REFUSES without --context and --expects; refusal names the missing field and the remedy (cause+remedy style).
- Emits an ORDINARY bus send to the CONFIGURED seat (no new message shape): context as first line, intent derived from expects (reply/decide -> request, else inform).
- Mission association: explicit --mission flag wins, else mish resolve at cwd.
- Seat name is configuration, not hardcoded (owner seat may rename).
- Day-one scope: raises at seats only; no agent-to-agent generalization.
- Future (NOT this task): MCP tool wrapping the same funnel.

Independent of the join/spawn-mission lane — parallelizable.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A raise lands as a managed thread in mission control's inbox with correct expects
- [ ] #2 A bare raise attempt (missing --context and/or --expects) refuses with actionable text naming the missing field
- [ ] #3 Unit tests cover the refusal matrix (each missing/invalid field combination)
- [ ] #4 Intent derivation: reply/decide -> request; act/read -> inform; covered by tests
- [ ] #5 No new bus message shape; the send is an ordinary hcom send to the configured seat
<!-- AC:END -->
