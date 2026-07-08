---
id: TASK-036
title: >-
  herder spawn: codex boot latency routinely exceeds the 60s bind window
  (bind_timeout twice in wave 7)
status: In Progress
assignee:
  - unit-y-vivo
created_date: '2026-07-08 02:30'
updated_date: '2026-07-08 03:28'
labels: []
dependencies: []
priority: low
ordinal: 36000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Both codex reviewer spawns in wave 7 (x-review, w-review) hit bind_timeout: codex took >60s (HERDER_SPAWN_BIND_MS default) to join the bus, spawn returned 'NOT sent — resend SAFE', the agent joined shortly after, and a manual hcom resend delivered. The documented recovery works verbatim but is manual friction on every slow boot. Candidates: (a) agent-family-specific bind default (codex boots slower than claude — bump its window); (b) spawn --json emitting the exact resend command for the operator; (c) a herder 'redeliver <guid>' that waits for join then sends the stored prompt (spawn already persists it?). Decide after checking whether the latency is environmental (tonight's load) or structural.
<!-- SECTION:DESCRIPTION:END -->
