---
id: TASK-014
title: >-
  codex bootstrap: full herder-native rewrite (codex still gets hcom's original
  bootstrap)
status: Done
assignee: []
created_date: '2026-07-07 06:40'
updated_date: '2026-07-07 07:40'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 14000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Follow-up from TASK-002: codex sessions still receive hcom's ORIGINAL bootstrap baked into developer_instructions (advertises hcom spawn/kill, no herder AGENTS doctrine). TASK-002 only appends the SUBAGENTS block at launch. A full codex bootstrap rewrite — mirroring the claude sessionstart rewrite doctrine, delivered via the launch-args seam — is unowned territory. Note: hcom strips user developer_instructions on codex resume/fork, so that path needs its own design.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commits bd2189d+682be11+8784b5d (unit-c-codex-bootstrap, merged f847501). Supersede-by-addendum: hcom's bootstrap cannot be removed at the seam (merged first, unconditionally), so fresh codex launches get [HERDER SESSION ADDENDUM] = supersede preamble + shared AGENTS doctrine + codex SUBAGENTS, threaded as user-level -c developer_instructions. Shared herderAgentsSection const + byte-identity drift guard keeps claude/codex doctrine identical (claude template verified byte-identical pre/post refactor). codexStripsDevInstructions mirrors hcom's resume/fork strip predicate — no dead argv on those paths; resumed/forked codex sessions get hcom stock only → TASK-017. pin_codex golden regenerated on main at integration (commit after f847501). Battery delta zero.
<!-- SECTION:NOTES:END -->
