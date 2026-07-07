---
id: TASK-027
title: >-
  codex fork --self: addendum delivery for the spawn-fallback path (TASK-017
  residual)
status: To Do
assignee: []
created_date: '2026-07-07 09:39'
labels:
  - run-herder-dx
dependencies: []
priority: low
ordinal: 27000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-017 (Unit L, wave 4) delivers the herder doctrine addendum post-boot on codex resume and NATIVE fork paths. Residual gap ruled acceptable: codex fork --self falls back to a 'herder spawn --extra-arg fork ...' handoff where the child guid is only known inside spawncmd — covering it from lifecyclecmd means parsing spawn --json or cross-package surgery. When worth it: teach spawn itself to thread the addendum on that path (it owns the guid), or surface the child guid back to the fork caller. Rare path; documented in README as a known gap by TASK-017.
<!-- SECTION:DESCRIPTION:END -->
