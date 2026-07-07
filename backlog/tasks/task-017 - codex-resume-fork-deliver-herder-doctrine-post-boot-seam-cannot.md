---
id: TASK-017
title: 'codex resume/fork: deliver herder doctrine post-boot (seam cannot)'
status: To Do
assignee: []
created_date: '2026-07-07 07:29'
updated_date: '2026-07-07 07:59'
labels: []
dependencies: []
priority: medium
ordinal: 17000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Unit C finding (TASK-014): hcom strips ALL user developer_instructions on codex resume/fork and re-adds only its own bootstrap — structurally unfixable at the launch-args seam, so resumed/forked codex sessions see only hcom stock guidance. Wave-2 candidate: resume/fork cmds deliver the herder addendum post-boot via herder send / hcom message once the session is up. Needs design (timing, dedup, idempotence).

SCOPE ADDITION (wave-2 findings): also close the launch --help documentation gap — it says nothing about the codex bootstrap addendum (TASK-014) or its resume/fork strip behavior (Unit F finding #2, Unit G nice-to-have).
<!-- SECTION:DESCRIPTION:END -->
