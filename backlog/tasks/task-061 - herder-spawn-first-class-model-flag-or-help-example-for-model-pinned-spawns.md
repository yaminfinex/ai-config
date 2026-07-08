---
id: TASK-061
title: >-
  herder spawn: first-class --model flag (or help example) for model-pinned
  spawns
status: To Do
assignee:
  - hera
created_date: '2026-07-08 07:09'
updated_date: '2026-07-08 23:42'
labels: []
dependencies: []
ordinal: 61000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
herder spawn can pin a claude model today only via the generic passthrough (--extra-arg --model --extra-arg claude-opus-4-8). Two orchestrators in separate runs needed model-pinned spawns for the owner model policy and neither discovered the passthrough from --help; one nearly fell back to raw hcom spawn, which bypasses the registry. Fix: a first-class --model flag on herder spawn for claude/codex agents (maps to the underlying CLI model flag), or minimally a worked --extra-arg example in spawn --help. Pure DX; keeps policy-compliant spawns on the canonical path.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
herder spawn can pin a claude model today only via the generic passthrough: --extra-arg --model --extra-arg claude-opus-4-8. Two orchestrators (hera during this run, lale in market-sim #7660) needed this for the owner model policy (opus for reviews) and neither found it from --help; lale nearly fell back to raw hcom spawn, which bypasses the registry. Proposal: first-class --model flag on herder spawn for claude/codex agents (maps to the underlying CLI flag), or minimally a --help example showing the --extra-arg pattern. Low risk, pure DX; keeps policy-compliant spawns on the canonical path.
<!-- SECTION:NOTES:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 herder spawn --model <id> works for claude and codex agents (passes through to the tool CLI), OR spawn --help gains a worked model-pinning example — one of the two, implemented
- [ ] #2 suite or golden covers the chosen mechanism
<!-- AC:END -->
