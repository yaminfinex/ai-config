---
id: TASK-061
title: >-
  herder spawn: first-class --model flag (or help example) for model-pinned
  spawns
status: To Do
assignee:
  - hera
created_date: '2026-07-08 07:09'
labels: []
dependencies: []
ordinal: 61000
---

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
herder spawn can pin a claude model today only via the generic passthrough: --extra-arg --model --extra-arg claude-opus-4-8. Two orchestrators (hera during this run, lale in market-sim #7660) needed this for the owner model policy (opus for reviews) and neither found it from --help; lale nearly fell back to raw hcom spawn, which bypasses the registry. Proposal: first-class --model flag on herder spawn for claude/codex agents (maps to the underlying CLI flag), or minimally a --help example showing the --extra-arg pattern. Low risk, pure DX; keeps policy-compliant spawns on the canonical path.
<!-- SECTION:NOTES:END -->
