---
id: TASK-018
title: 'bin/herder + bin/bottle: pin LC_ALL=C on the source-hash sort'
status: Done
assignee: []
created_date: '2026-07-07 07:29'
updated_date: '2026-07-07 07:57'
labels:
  - run-herder-dx
dependencies: []
priority: low
ordinal: 18000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Unit B finding (run-herder-dx): the cache key's 'sort -z' is locale-sensitive — interactive vs env -i environments can compute different hashes for the SAME tree, doubling builds across regimes (was a thrash amplifier pre-TASK-012; still causes duplicate cache entries after). One-liner: LC_ALL=C sort -z.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Both bin/herder and bin/bottle pin LC_ALL=C on the source-hash sort -z
- [x] #2 Evidence: same tree hashes identically under a UTF-8 locale vs env -i (shown in notes)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit 4f6d128. Root cause: `sort -z` in the cache-key pipeline is locale-sensitive, so interactive (UTF-8 locale) vs env -i (C) regimes could hash the SAME tree differently — duplicate cache entries, pre-TASK-012 a thrash amplifier. Change: LC_ALL=C pinned on the sort in BOTH bin/herder and bin/bottle. Evidence: synthetic proof that C vs en_US.UTF-8 genuinely reorder (x/B1 sorts first under C, last under UTF-8); end-to-end, both wrapper copies reused the env-i-built binary when re-run under LC_ALL=en_US.UTF-8 with no toolchain on PATH (reuse ⇒ identical hash). hera addendum (blessed): wherever the two regimes previously disagreed, merge causes a ONE-TIME cache re-key (rebuild) per checkout; benign — orphaned old-key binaries age out under the 14d post-build prune, no wipe involved.
<!-- SECTION:NOTES:END -->
