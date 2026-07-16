---
id: TASK-248
title: >-
  herder raise conversations become dyadic — replies route asker+owner only,
  backed by a first-class ask entity
status: To Do
assignee: []
created_date: '2026-07-15 20:11'
updated_date: '2026-07-16 00:51'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 247500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Ratified via the desk-mechanics design ruling (owner via design-seat; filed by the mc doctrine lane). EVIDENCE: an owner reply to a raise fanned out to the working thread membership (thread herder-mission-verbs, reply #89603) and two crew members nearly double-executed the ruling — the raise funnel currently uses the working thread as the conversation container, so every member receives owner rulings addressed to one asker.

TARGET SHAPE (three parts):
1. ASK ENTITY: a raise creates a first-class ask entity — markdown body + references + soft-links to the working thread, git custody, its own status lifecycle. Entity vocabulary is being authored in the mc doctrine pass; consume it, do not invent a parallel one.
2. DYADIC CONVERSATION: the bus conversation on a raise is asker + owner ONLY. Replies route to the asker, never to working-thread membership. The working thread is soft-linked context, never the container.
3. SETTLE WRITE-BACK: on settle, the outcome writes back into the ask entity — that is the durable ruling trail.

UNIT 2 — BLOCKING FACT (co-scheduled, from ratified discussion 2, owner do-not-relitigate constraints): raise gains an optional declared fact "blocking: <what is stopped right now>" alongside expects. Semantics: a FACT, not a grade — a checkable statement of what work/lane/decision is stopped while the raise sits, rendered VERBATIM on the owner desk. NO urgency enum anywhere, ever — urgency is never declared and watchers never infer it. Silence HOLDS regardless: a blocking raise still never timeout-proceeds. Omission is valid (not everything blocks). Wire shape consistent with the merged raise format: an optional "Blocking: <one line>" metadata line after "Mission:", same stops-at-blank-line parsing.

Priority: not urgent-tagged by the owner; prevents the misrouting class above. One reopening of the raise verb takes both units.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A raise creates a durable ask entity (md + references + working-thread soft-links, git custody, own status lifecycle) using the mc doctrine pass entity vocabulary
- [ ] #2 Replies to a raise deliver to the asker and owner seat only — working-thread membership receives nothing; the thread remains soft-linked context
- [ ] #3 Settling a raise writes the outcome back into the ask entity
- [ ] #4 Refusal/degraded paths carry cause+remedy; no run identifiers (agent names, task numbers) in durable fixtures/goldens
- [ ] #5 raise --blocking <one line> renders an optional "Blocking:" metadata line after "Mission:" (same stops-at-blank-line parsing); absent flag = absent line; refusal matrix unchanged
- [ ] #6 No urgency enum or inferred urgency anywhere; blocking raises still never timeout-proceed (silence holds)
- [ ] #7 Owner-only DYADIC WIDEN gesture: the owner may invite a third party into a live raise conversation; nobody else widens
- [ ] #8 Blocking is attested by ruling: the wire carries the claim verbatim; nothing verifies or grades it
<!-- AC:END -->
