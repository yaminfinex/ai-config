---
id: TASK-052
title: >-
  herder spawn: --label-prefix concatenates with role without separator
  (spec-hera + spec -> spec-heraspec-<short>)
status: Done
assignee: []
created_date: '2026-07-08 05:08'
updated_date: '2026-07-08 09:14'
labels: []
dependencies: []
priority: low
ordinal: 52000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Observed (hera, 2026-07-08): herder spawn --role spec --label-prefix spec-hera produced label 'spec-heraspec-ff71e7f3' — the prefix override appears to prepend to the role rather than replace it (or a separator is missing). Expected 'spec-hera-ff71e7f3'. Cosmetic but labels are addressing surfaces; worth a one-line fix + test. Worked around via herder rename.
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 09:14
---
[hera 2026-07-08] Delivered as the TASK-047 fold (merge 78edf09): spawnLabel --label-prefix now REPLACES the role prefix; test + contract case reject the old concatenation.
---
<!-- COMMENTS:END -->
