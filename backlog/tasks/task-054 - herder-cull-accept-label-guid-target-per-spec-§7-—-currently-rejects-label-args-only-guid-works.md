---
id: TASK-054
title: >-
  herder cull: accept label/guid <target> per spec §7 — currently rejects label
  args (only --guid works)
status: To Do
assignee: []
created_date: '2026-07-08 05:29'
updated_date: '2026-07-08 23:42'
labels: []
dependencies: []
priority: low
ordinal: 54000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Reported by spec-ravu (bus #6043, applied by hera): herder cull rejects a bare label argument with 'unknown arg'; only --guid is accepted. Spec §7 (branch herder-spec, under ratification) defines cull <target> where target = guid | short-guid | label, consistent with the one-resolver rule (D10). Align the CLI: accept positional target through the standard resolver; keep --guid as an alias for compatibility. Small; pairs naturally with the TASK-051 cull-message fix (already-closed wording).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 herder cull accepts positional <target> = guid | short-guid | label via the standard resolver (one-resolver rule D10)
- [ ] #2 --guid keeps working as an alias
- [ ] #3 ambiguous label refuses with candidates named, consistent with resolver discipline elsewhere
- [ ] #4 suite covers label cull and the ambiguity refusal
<!-- AC:END -->
