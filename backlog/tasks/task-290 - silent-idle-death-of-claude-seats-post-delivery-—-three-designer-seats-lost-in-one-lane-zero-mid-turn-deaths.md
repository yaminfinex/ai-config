---
id: TASK-290
title: >-
  silent idle-death of claude seats post-delivery — three designer seats lost in
  one lane, zero mid-turn deaths
status: To Do
assignee: []
created_date: '2026-07-19 11:45'
labels: []
dependencies: []
ordinal: 289500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Three claude-family designer seats in one lane (workspace w4D) died SILENTLY while idle after delivering their work; the deaths were only discovered later (cull sweep reporting 'already unseated ... observed_dead', or a herder wait returning pane_not_found). Zero work lost so far because all three died AFTER delivery — but an idle-death before delivery would strand a unit silently.

Evidence (from the lane orchestrator, positively observed):
- mizu 35c894af, claude, w4D:p2Y — delivered memo, idle-held hours; at cull sweep: already unseated, observed_dead 2026-07-19T01:15:31Z.
- funo 422af820, claude, w4D:p2Z — identical pattern, observed_dead 01:15:36Z, same sweep. Caveat both: timestamp is the unseat OBSERVATION (seconds within the sweep); died-hours-earlier vs died-at-observation cannot be distinguished from current telemetry — the release-notice flow ran verify=delivered then 'already unseated', which reads as death predating the sweep.
- niza 49c98e41, claude, w4D:p32, cwd=missions repo — active through multiple turns, last send its fix-confirm report; found off-bus + pane_not_found shortly after. Died AT IDLE post-delivery, same day as spawn.
Controls that survived to clean cull+ack: riko 66d0b741 (codex, w4D:p20, SAME idle-hold duration as mizu/funo) and huno 47391281 (claude, w4D:p31, longest-lived seat in the lane). So neither claude-alone nor lifetime-alone explains it; idle-at-death is the only universal. All three victims are claude; possible correlate: niza (and other recent spawns in that lane) emitted 'credential path notice=send_failed' at spawn — the forked-spawner notice-failure class (separate task on the board covers that signal itself); mizu/funo spawn output unknown (predates the observer's compaction).

Unit shape: INVESTIGATE (research type). (1) Pull whatever host/registry/observer telemetry exists for the three guids (registry rows, observer sweep logs, tmux/pane server logs, process exit traces) and bound the actual times of death. (2) Determine the death mechanism: agent process exit? pane teardown? harness-side idle timeout? OOM? distinguish herder-caused vs vendor-CLI-caused vs terminal-server-caused. (3) Establish whether the class is claude-specific, workspace-w4D-specific, or lane-coincidence — check other workspaces' idle claude seats for survivors as further controls. (4) Recommend detection (a seat death should be OBSERVED and announced near death time, not discovered at the next verb — relates to the observer's sweep cadence) and, if mechanism is herder-side, a fix capture as a follow-on task. Probes read-only against live state; NO probe may kill, restart, or write to live seats or the registry.
<!-- SECTION:DESCRIPTION:END -->
