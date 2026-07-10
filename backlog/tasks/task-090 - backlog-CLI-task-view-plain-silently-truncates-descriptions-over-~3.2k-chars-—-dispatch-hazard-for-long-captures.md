---
id: TASK-090
title: >-
  backlog CLI: task view --plain silently truncates descriptions over ~3.2k
  chars — dispatch hazard for long captures
status: Done
assignee: []
created_date: '2026-07-09 04:22'
updated_date: '2026-07-10 01:42'
labels: []
dependencies: []
priority: high
ordinal: 90000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
FOUND LIVE (2026-07-09): `backlog task 86 --plain` rendered 3256 chars of a 4210-char description and then moved on to Acceptance Criteria with NO truncation marker. The lost tail contained a settled design decision (one-binary CLI) — exactly the content the capture contract exists to deliver, in exactly the command dispatch briefs tell workers to run. Four live tasks affected at discovery (sesh lanes 085-088); mitigated with warning comments pointing at the raw file.

SCOPE: (1) LOCAL DOCTRINE NOW: dispatch briefs and the orchestrate-skill backlog reference should tell workers to read the raw task file under backlog/tasks/ for any capture-critical read, with --plain as the index view — check skills/orchestrate/references/backlog-integration.md and adjust. (2) INVESTIGATE: find the actual cap and whether a flag (--full?) or config raises it; whether comments/AC sections truncate too (comments appeared intact at ~1.5k). (3) UPSTREAM: Backlog.md is an external tool — if no flag exists, draft an upstream issue (truncation without a marker is the bug even if capping is intended) for the TASK-029 ledger; drafts only, owner files.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The exact truncation behavior is pinned (cap value, which sections, marker or none) with a repro
- [x] #2 Local mitigation landed: orchestrate backlog reference + any brief templates direct capture-critical reads at the raw file (or a discovered full-render flag)
- [x] #3 Upstream issue draft in the TASK-029 ledger if no upstream remedy exists
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Dispatched 2026-07-10 to gpt-5.6-sol worker (@worker-maso, branch task-090-plain-truncation), brief napkins/run-herder-dx/task-090-brief.md.

Done 2026-07-10, merged to main (d284300 --no-ff). ROOT CAUSE REVISED: no character cap exists in --plain (20k payloads round-trip byte-perfect on Backlog.md 1.47.1; formatter source has no slice/limit). Actual mechanism: duplicated/nested SECTION markers — the CLI wraps marker-containing input in a second pair, the parser returns the FIRST begin/end pair only, and --plain silently omits the remainder with no warning (the historical failure had two DESCRIPTION marker pairs). Mitigation merged: orchestrate backlog reference requires raw backlog/tasks/ reads for capture-critical decisions, --plain as index only. Upstream draft appended to the TASK-029 ledger. Docs-only diff, gate skipped per stakes rule; verified 1 file +8 lines.
<!-- SECTION:NOTES:END -->
