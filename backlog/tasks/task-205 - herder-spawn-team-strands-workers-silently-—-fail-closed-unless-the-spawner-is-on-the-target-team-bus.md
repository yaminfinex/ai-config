---
id: TASK-205
title: >-
  herder spawn --team strands workers silently — fail closed unless the spawner
  is on the target team bus
status: To Do
assignee: []
created_date: '2026-07-14 02:06'
labels: []
dependencies: []
ordinal: 204000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TWO INDEPENDENT INCIDENTS (2026-07-08 this run; 2026-07-14 a sibling orchestrator's run): spawning with --team puts the child on $HERDER_TEAMS_ROOT/<NAME> while the spawning orchestrator stays on global — child reports to the orchestrator are REJECTED by hcom (not merely unseen), team-bus event subs never notify a global subscriber, and worker reports sit undelivered for hours. The only current validation is path-segment safety; there is no guard, no warning. Sibling-run recovery folklore: reach team-bus agents via herder send <guid>; poll via background watchers. FIX SPACE (recommended: fail-closed): spawn --team refuses with cause+remedy unless the SPAWNER resolves a live bus name on that team bus (registry hcom coordinates make this checkable at plan time); explicit override flag for a deliberately detached team; the refusal text names herder send <guid> as the cross-bus channel. Also surface team-vs-global mismatch in herder list/wait output. Alternative shape (owner may prefer): remove --team entirely — decide at dispatch against then-current usage. Docs: standing 16.2-class hazard graduates from run-doctrine to enforced tool behavior. Full battery + adversarial review.
<!-- SECTION:DESCRIPTION:END -->
