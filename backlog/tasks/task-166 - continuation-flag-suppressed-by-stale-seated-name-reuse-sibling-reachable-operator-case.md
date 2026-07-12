---
id: TASK-166
title: >-
  continuation flag suppressed by stale seated name-reuse sibling (reachable
  operator case)
status: To Do
assignee: []
created_date: '2026-07-12 12:32'
labels: []
dependencies: []
priority: low
ordinal: 165000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
When an agent dies without a cull, its registry row stays SEATED and keeps holding its bus name; a respawn that reuses the name then yields two seated rows with the same hcom_name, continuation target resolution sees ambiguity, and the per-row observer hint for an unresolved continuation failure is suppressed — exactly the situation (dead session, operator needs the surfacing) the hint exists for. This is the REACHABLE form of the name-reuse suppression: the retired/lost variant is unreachable (the write normalizer strips seats on every transition out of seated) and is already guarded as defense-in-depth. Resolving it needs a liveness signal to disambiguate the true holder (the observer computing these flags has liveness data), or a deliberate decision that ambiguous means unsurfaced — likely design-first. Empirical repro exists in the review record: two seated rows sharing a name resolve to no target and no flag.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A decision is recorded: disambiguate via liveness, or ratify no-flag-on-ambiguity with the operator remedy documented
- [ ] #2 If disambiguation: the stale-seated name-reuse scenario attaches the flag to the live row, pinned by a test driving the real write path
<!-- AC:END -->
