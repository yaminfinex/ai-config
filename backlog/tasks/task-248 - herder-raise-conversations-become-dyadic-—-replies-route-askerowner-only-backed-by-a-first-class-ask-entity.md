---
id: TASK-248
title: >-
  herder raise conversations become dyadic — replies route asker+owner only,
  backed by a first-class ask entity
status: To Do
assignee: []
created_date: '2026-07-15 20:11'
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

CO-SCHEDULING: the mc lane has a separate blocking-fact candidate that also touches the raise verb (logged in their doctrine pass, not yet on this board — pointer needed from @vile at dispatch time). One reopening of the raise verb should take both.

Priority: not urgent-tagged by the owner; prevents the misrouting class above.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A raise creates a durable ask entity (md + references + working-thread soft-links, git custody, own status lifecycle) using the mc doctrine pass entity vocabulary
- [ ] #2 Replies to a raise deliver to the asker and owner seat only — working-thread membership receives nothing; the thread remains soft-linked context
- [ ] #3 Settling a raise writes the outcome back into the ask entity
- [ ] #4 Refusal/degraded paths carry cause+remedy; no run identifiers (agent names, task numbers) in durable fixtures/goldens
<!-- AC:END -->
