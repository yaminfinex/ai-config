---
id: TASK-206
title: >-
  Tighten orchestrator battery-slot announcement so workers never run a battery
  in the orchestrator's gate worktree
status: To Do
assignee: []
created_date: '2026-07-14 02:57'
labels: []
dependencies: []
ordinal: 205000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
INCIDENT (2026-07-14, TASK-201 gate): the orchestrator announced a two-slot gate plan; worker-muho misread it as license to run its OWN gate battery in the task-201 worktree while the orchestrator's authoritative gate-201.sh was already running there — the two batteries collided and both had to be voided (orchestrator killed its gate via pkill, had the worker finish+freeze, then re-ran solo). Root cause: the battery-slot announcement wording does not make it unambiguous WHICH battery runs in WHICH worktree and that the orchestrator's gate worktree is off-limits to worker-run batteries. FIX SPACE (doctrine, standing-orders battery section): (1) a fixed announcement grammar naming, per slot, the exact worktree path + who runs it (orchestrator vs worker) + 'do not run any battery here' for orchestrator-gated worktrees; (2) the review-brief house-rules line already says 'an independent gate battery is running in it; do NOT build/test/modify' — make the orchestrator's live announcement echo that same sentence with the concrete path at slot-open time; (3) consider a cheap lock/marker file in a worktree while the orchestrator gate runs, so a worker's battery script self-aborts on contention. Low effort, doctrine-first; no code strictly required but the marker-file option is a small guardrail. Capture only — schedule behind the live grok/pi lanes.
<!-- SECTION:DESCRIPTION:END -->
