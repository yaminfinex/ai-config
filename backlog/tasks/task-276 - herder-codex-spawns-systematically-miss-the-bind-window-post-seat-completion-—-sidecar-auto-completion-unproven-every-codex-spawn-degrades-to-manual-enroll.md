---
id: TASK-276
title: >-
  herder: codex spawns systematically miss the bind window post seat-completion
  — sidecar auto-completion unproven, every codex spawn degrades to manual
  enroll
status: In Progress
assignee: []
created_date: '2026-07-17 07:33'
updated_date: '2026-07-17 07:35'
labels:
  - herder
  - identity-migration
dependencies: []
priority: high
ordinal: 275500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field class, same night the canonical seat-completion unit merged: 4/4 codex spawns on this host hit bind-timeout(60000ms) -> 'seat completion refused [joined_bus_row_missing]' with pane correctly preserved and NO registry row (1 in this fleet, 3 in a peer fleet via compound+single spawns), while codex spawns succeeded earlier the same day pre-merge. Evidence from the in-fleet case: the child JOINED the bus ~27-57s after launch (ready event on record), yet bind still refused — codex roster rows omit the pane coordinate and the session-id enrichment lags (known structural class), so the completion-step Evidence correlates (SessionID/ProcessID/PaneIDs) cannot match a codex row inside the window even when the row exists. The refusal's promised automatic recovery ('its sidecar will complete the seat') did NOT fire within ~4 minutes despite the sidecar process alive and actively polling; the documented manual recovery (herder enroll from the live seat, deliverable over the bus when the child joined) worked first try and is currently the ONLY practical path. Under pre-merge behavior the row was minted creator-side and lag only delayed prompt delivery; refuse-not-mint is the ratified design, but the operational outcome is that codex spawns now routinely require manual recovery. Scope: (a) establish why the sidecar's correlated-recognition cannot (or how slowly it can) complete a codex seat — if its predicate needs the same absent correlates, the automatic path is structurally dead for codex and the refusal text overpromises; (b) make codex spawns complete at birth again: candidates include a codex-aware bind window, matching on additional correlates herder itself knows (it launched the pane and knows the child pid + the hcom name it minted — the launcher env carries the name), or having spawn consult the roster row by its OWN minted name once joined; (c) keep the ratified fences: no partial rows, refuse-not-mint, multi-match fail-closed, no ambient re-selection. Design checkpoint required before code (touches the completion evidence path).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Root cause established with evidence: exactly which correlate(s) fail for codex rows at bind time and in the sidecar predicate, with timings
- [ ] #2 A codex spawn under enrichment lag completes its seat automatically (at birth or via sidecar within a bounded, stated time) without manual enroll, on this host under load
- [ ] #3 Refusal text matches reality: the automatic-recovery promise is either made true or reworded to the actual remedy
- [ ] #4 All ratified completion fences preserved (refuse-not-mint, multi-match fail-closed, no partial rows); existing suites + spawn goldens green
<!-- AC:END -->
