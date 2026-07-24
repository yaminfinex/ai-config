---
id: TASK-292
title: claude shim <-> mise shim infinite exec loop freezes pane at startup banner
status: Done
assignee: []
created_date: '2026-07-20 05:19'
updated_date: '2026-07-24 21:53'
labels: []
dependencies: []
ordinal: 291500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field-report (second deployment, root-caused there): tools/herder/shims/claude find_real_tool only skips candidates carrying the herder-path-shim marker. A mise shim (symlink to the mise ELF) is not detectable, gets chosen as the real claude, and when that mise tool is stale/inactive it bounces execution back to the herder shim — infinite exec ping-pong, pane frozen at the wrapper's 'Starting Claude Code...' banner, PID cycling bash<->mise at pipe_read. Trigger observed: Claude self-update recreated ~/.local/bin/claude while an inactive mise claude still had a shim on PATH. Fix per report: (a) skip candidates that resolve (through symlinks) to the mise binary or any shims dir; (b) recursion-depth guard env var so re-entry fails LOUDLY instead of looping silently. Related hazard class: long-lived-session PATH drift (see hcom/mise upgrade gotcha in run doctrine).
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Closed by owner-pushed main 234bc24 (implemented on the second deployment where it was root-caused). Both asks delivered: (a) find_real_tool skips candidates resolving through symlinks to the mise multiplexer or a mise shims dir — hcom shim deliberately exempt (mise IS hcom's provider); (b) re-dispatch depth guard fails loud instead of silent ping-pong, cleared before exec so launched agents start clean. codex shim hardened identically. Regression cases for both detection branches in check-shims.sh. Rollout here: pulled 2026-07-24, house battery 63/63 BATTERY_PASS on main at 234bc24, tail counted. Resident-writer note: shims are read at every launch (no long-lived resident executes stale shim code), so the fix governs all future spawns immediately.
<!-- SECTION:NOTES:END -->
