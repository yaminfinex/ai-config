---
id: decision-001
title: Every task keeps all relevant docs current as part of its acceptance
date: '2026-07-12 20:54'
status: Accepted
---
## Context

Standing ask from the owner (2026-07-12, during the TASK-155 → TASK-168 sesh
distribution work): documentation drift keeps happening because doc updates are
treated as follow-up work rather than part of the change itself. READMEs,
runbooks, spec notes, and cross-references in backlog docs go stale the moment
a task ships without them.

## Decision

Every task — design or implementation — enumerates the documents its change
touches and updates them within the same unit of work. Doc updates are
acceptance criteria, not follow-ups. Design passes must include an explicit
docs plan (doc → change → owning task) that hands each follow-up task its doc
rows as ACs (first instance: the docs plan in
docs/design/2026-07-12-sesh-store-served-distribution.md §10).

"Relevant docs" includes at minimum: the component README and runbooks, spec
documents (including informational notes where contracts are unchanged),
deprecation pointers in superseded scripts, justfile/recipe comments, and
cross-references in backlog docs that state something the change makes untrue.

## Consequences

- Task authors (including agents filing tasks) write doc ACs at filing time;
  reviewers treat missing doc updates as incomplete work, same as missing
  tests.
- Stale docs found during any task are fixed or filed in that task, not
  silently skipped.
- Slightly larger tasks, in exchange for docs that can be trusted as current.
