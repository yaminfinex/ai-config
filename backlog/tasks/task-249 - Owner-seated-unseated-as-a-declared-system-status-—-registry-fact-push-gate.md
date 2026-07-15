---
id: TASK-249
title: Owner seated/unseated as a declared system status — registry fact + push gate
status: To Do
assignee: []
created_date: '2026-07-15 20:25'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 248500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
From the ratified desk-mechanics design program (owner via design-seat; D5 modes, do-not-relitigate). herder/hcom lane home: registry status + push gate. The mc rendering side is separate (wireframe-era on the mc board).

SEMANTICS (ratified, do-not-relitigate):
- Exactly ONE mode exists: UNSEATED = bed/away/DND. Seated is the default state, not a second mode.
- It is a DECLARED FACT agents can read — same family as the raise blocking fact (TASK-248 unit 2). NEVER inferred from idle time; watchers never infer it.
- It GRANTS NOTHING: silence holds, gates hold, autonomy widening stays an explicit owner instruction. Unseated is not permission.
- Its ONLY effects: (a) push-silence, (b) crew awareness — crews sequence around the absence and batch asks.

PUSH DOCTRINE (context the gate implements against): only a blocking decide/reply ever pushes; unseated silences even that; NO emergency category exists.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Owner can declare unseated/seated; the status is a readable registry fact for agents
- [ ] #2 Status is only ever declared — nothing infers it from idle time or activity
- [ ] #3 Unseated silences push (including blocking decide/reply — the only class that pushes at all); no emergency bypass exists
- [ ] #4 Unseated grants nothing: no gate relaxation, no timeout-proceed, no autonomy widening
- [ ] #5 Refusal/degraded paths carry cause+remedy; no run identifiers in durable fixtures/goldens
<!-- AC:END -->
