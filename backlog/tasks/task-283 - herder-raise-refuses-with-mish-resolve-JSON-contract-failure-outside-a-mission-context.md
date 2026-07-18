---
id: TASK-283
title: >-
  herder raise: refuses with mish-resolve JSON contract failure outside a
  mission context
status: To Do
assignee: []
created_date: '2026-07-18 12:05'
labels:
  - herder
  - dx
dependencies: []
priority: medium
ordinal: 282500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field defect 2026-07-18: herder raise --context ... --expects decide from the orchestrator cwd (/home/grace/Coding/ai-config, no active mission) refused with 'mission resolution failed: mish resolve did not return its JSON contract; verify mish is installed and the mission context is valid'. A raise with no --mission flag from a non-mission cwd should either work missionless or refuse with a cause+remedy naming how to raise without a mission — not a broken-contract error. Determine whether mish resolve is genuinely broken here or raise mishandles the no-mission case; fix the refusal either way.
<!-- SECTION:DESCRIPTION:END -->
