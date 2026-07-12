---
id: TASK-153
title: 'mish: align help slug and context validation with mission spec'
status: Done
assignee: []
created_date: '2026-07-10 10:15'
updated_date: '2026-07-12 08:04'
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
- [x] #1 mish new help creates the valid help mission slug; extra positional arguments still refuse
- [x] #2 Explicit, marker, and cwd context resolution all use the canonical mission slug validator
- [x] #3 Trailing-hyphen and consecutive-hyphen mission directories refuse consistently
- [x] #4 CLI/help goldens and scenario tests cover both regressions
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
SHIPPED, merged from branch task-153-mish-slug (commit b4ad3c3). mish new help creates the valid help mission (extra positionals still refuse rc2); explicit --mission, .mission marker, and cwd resolution all delegate to missionfs.ValidateSlug — the looser resolve.go slugPattern regexp is REMOVED, and cwd resolution (previously ZERO validation) now validates; trailing/consecutive-hyphen dirs refuse consistently with cause+remedy, no destructive advice. Only-tightens property verified by review: creation always used the canonical validator, so nothing creatable can now fail resolution. Adversarial review (opus, cross-family): APPROVE, all six lenses cleared, all help surfaces live-verified intact (new -h/--help, help new, bare help, -h, --help); two non-blocking notes recorded (help detection scans args before -- splitting — harmless since hyphen-leading slugs are never valid; cwd refusal remedy wording reads as typed-slug context). Gates: independent 4-module + 53-script battery green from the worktree; post-merge battery green on main.
<!-- SECTION:NOTES:END -->
