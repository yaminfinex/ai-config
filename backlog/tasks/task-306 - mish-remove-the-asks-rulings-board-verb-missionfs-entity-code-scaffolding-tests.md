---
id: TASK-306
title: >-
  mish: remove the asks/rulings board (verb, missionfs entity code, scaffolding,
  tests)
status: To Do
assignee: []
created_date: '2026-08-21 05:42'
labels:
  - mish
  - cleanup
dependencies: []
priority: medium
ordinal: 305500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner ruling 2026-08-21 (yamen, in-session): mish asks is not needed — remove it as part of the fleet-refit cleanup. Scope at HEAD: tools/mish/internal/cli/asks.go (588 lines) + internal/missionfs/asks.go (705 lines) + asks_test.go in both packages + tests/check-asks.sh; references to sweep: cli/help.go + help_golden_test.go (verb listing), cli/new.go (scaffolds asks/config.yml + entities/), cli/status.go (asks health section), missionfs/scan.go + board.go (asks awareness), cli/allowlist.go + root_test.go/new_test.go/backlog_test.go/status_test.go/resolve_test.go mentions. Mission-dir sweep: remove asks/ scaffolding from existing missions ($MISSIONS_REPO/missions/*/asks/ — only fleet-refit has entities; the two wayfinder-filed ones are being deleted ahead of this task, so expect config.yml + empty entities/ only). mish help text and the mish skill need no asks mention post-removal (skill already has none). The one consumer flow that used it (herder slim-down pass-4 owner gate) re-routes to the decision-sheet artifact + program-brief decision register — no replacement mechanism needed.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 mish binary has no asks verb; help/goldens updated; all asks Go code and tests deleted; battery green
- [ ] #2 mish new no longer scaffolds asks/; mish status has no asks section; existing mission asks/ dirs removed with a custody commit
<!-- AC:END -->
