---
id: TASK-018
title: 'bin/herder + bin/bottle: pin LC_ALL=C on the source-hash sort'
status: To Do
assignee: []
created_date: '2026-07-07 07:29'
labels: []
dependencies: []
priority: low
ordinal: 18000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Unit B finding (run-herder-dx): the cache key's 'sort -z' is locale-sensitive — interactive vs env -i environments can compute different hashes for the SAME tree, doubling builds across regimes (was a thrash amplifier pre-TASK-012; still causes duplicate cache entries after). One-liner: LC_ALL=C sort -z.
<!-- SECTION:DESCRIPTION:END -->
