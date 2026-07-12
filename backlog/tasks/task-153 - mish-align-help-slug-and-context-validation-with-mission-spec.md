---
id: TASK-153
title: 'mish: align help slug and context validation with mission spec'
status: In Progress
assignee: []
created_date: '2026-07-10 10:15'
updated_date: '2026-07-12 07:53'
labels: []
dependencies: []
references:
  - docs/specs/mission-spec.md
modified_files:
  - tools/mish/internal/cli/new.go
  - tools/mish/internal/resolve/resolve.go
priority: high
ordinal: 152000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Fix two mission-contract violations found during the documentation accuracy audit. The valid slug help is currently hijacked by positional help parsing, and context resolution uses a looser slug check than mission creation, allowing trailing or consecutive hyphens through explicit/marker/cwd resolution. Reuse one canonical validator and keep normal help available through flags and the root help command.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 mish new help creates the valid help mission slug; extra positional arguments still refuse
- [ ] #2 Explicit, marker, and cwd context resolution all use the canonical mission slug validator
- [ ] #3 Trailing-hyphen and consecutive-hyphen mission directories refuse consistently
- [ ] #4 CLI/help goldens and scenario tests cover both regressions
<!-- AC:END -->
