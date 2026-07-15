---
id: TASK-236
title: 'registry.V2Resolve: refuse ambiguous targets instead of last-wins'
status: To Do
assignee: []
created_date: '2026-07-15 07:44'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 235500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Shared-resolver hardening surfaced during the mission-membership review (adjudicated OUT of that unit as pre-existing behavior; the fact is verified real). registry.V2Resolve walks all projection sessions and keeps the LAST match for guid/short-guid/label/pane with no multi-match refusal — an ambiguous target (short-guid collision ~4.3e9 space, shared pane_id across rows, stale labels) silently resolves to whichever row iterates last. Blast radius: every consumer — rename, adopt, retire, and now mission join/leave — can mutate the WRONG row on collision. Reachability is low (v4 short-guids, label uniqueness enforced among non-retired rows) but the failure is silent wrong-row mutation, the worst class for a write-path resolver.

Fix shape: collect ALL matches; count==1 proceeds; count>1 refuses with a typed cause listing the colliding rows (guid + label + state) and a remedy (use the full guid); count==0 unchanged. One resolver change, goldens for each consumer verb's ambiguous-target refusal, plus a test pinning that full-guid always disambiguates. Watch for callers that deliberately rely on last-wins (none known; verify).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Ambiguous short-guid, label, and pane targets refuse with typed cause naming the colliding rows
- [ ] #2 Full-guid targets always resolve uniquely (pinned)
- [ ] #3 Each consumer verb (rename/adopt/retire/join/leave) has an ambiguity refusal golden
- [ ] #4 No caller relied on last-wins (audited and stated in DONE record)
<!-- AC:END -->
