---
id: TASK-015
title: 'bin/bottle: same wipe + toolchain fixes as bin/herder (TASK-008/012 twin)'
status: To Do
assignee: []
created_date: '2026-07-07 07:29'
updated_date: '2026-07-07 07:41'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 15000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Unit A finding (run-herder-dx): bin/bottle:33 has the identical 'rm -f bottle-*' cache wipe and unversioned toolchain pick that TASK-008/TASK-012 fixed in bin/herder. Apply the same two fixes verbatim: version-check go against go.mod with mise fall-through + GOTOOLCHAIN=local, and per-hash binaries pruned by age instead of pre-build wipe.
<!-- SECTION:DESCRIPTION:END -->
