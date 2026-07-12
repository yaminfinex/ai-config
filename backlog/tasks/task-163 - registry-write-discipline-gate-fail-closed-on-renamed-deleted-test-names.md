---
id: TASK-163
title: 'registry write-discipline gate: fail closed on renamed/deleted test names'
status: To Do
assignee: []
created_date: '2026-07-12 12:19'
labels: []
dependencies: []
priority: medium
ordinal: 162000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The registry write-discipline check builds one go test -run alternation of explicitly named tests. One named test was deleted when the legacy two-state view was retired (its replacements are the four-state load/view test and the seated-vs-non-retired predicate test), and a regex alternative that matches no test is silently harmless — the gate stays green while coverage shrinks. Repair the selector to run the current four-state read-contract tests, and make the gate prove every name it declares actually exists and executes, so future renames cannot silently reduce coverage. Remove the run-identifier wording from the script's header comment while in there.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The gate runs the four-state load/view test and the seated-versus-non-retired predicate test
- [ ] #2 Every test name declared by the gate is proven to exist and execute; a deliberately nonexistent name makes the gate fail with a useful message
- [ ] #3 No deleted/legacy test name remains in the gate; registry gate and full herder go test suite pass
<!-- AC:END -->
