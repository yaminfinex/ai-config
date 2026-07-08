---
id: TASK-036
title: >-
  herder spawn: codex boot latency routinely exceeds the 60s bind window
  (bind_timeout twice in wave 7)
status: Done
assignee:
  - unit-y-vivo
created_date: '2026-07-08 02:30'
updated_date: '2026-07-08 04:13'
labels: []
dependencies: []
priority: low
ordinal: 36000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Both codex reviewer spawns in wave 7 (x-review, w-review) hit bind_timeout: codex took >60s (HERDER_SPAWN_BIND_MS default) to join the bus, spawn returned 'NOT sent — resend SAFE', the agent joined shortly after, and a manual hcom resend delivered. The documented recovery works verbatim but is manual friction on every slow boot. Candidates: (a) agent-family-specific bind default (codex boots slower than claude — bump its window); (b) spawn --json emitting the exact resend command for the operator; (c) a herder 'redeliver <guid>' that waits for join then sends the stored prompt (spawn already persists it?). Decide after checking whether the latency is environmental (tonight's load) or structural.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Structural-vs-environmental established via live measurement
- [x] #2 Direction chosen with rationale; disproven premises abandoned
- [x] #3 bind_timeout/ready_match_timeout emit the exact verbatim resend command (stderr + --json resend_command); vocabulary meanings unchanged
- [x] #4 Resend command round-trips metachar labels AND prompts through bash verbatim
- [x] #5 Pinned gate green + goldens updated
- [x] #6 Docs: README Delivery resend_command + codex pane_id asymmetry; spawn --help
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Established via live measurement that codex bind_timeouts are STRUCTURAL, not slow boots: codex joins the bus ~1s after launch but its roster entry omits launch_context.pane_id (claude publishes it), defeating both pane-id correlation paths; correlation arrives only via async sidecar enrichment, which lags minutes under load (a clean probe exceeded a 240s window). hera cross-checked the harsher reading (post-TASK-033 = never enriches) against the registry: disproven — rows do enrich eventually; async-lag confirmed. Shipped direction (b): bind_timeout/ready_match_timeout print the exact verbatim resend command (herder send <quoted-label> <quoted-prompt>, notify appendix folded in) on stderr and as --json resend_command (omitempty; the two not-sent results only). Both label AND prompt shell-quoted (review P2: metachar labels are accepted, raw label could split/expand on paste; bind_timeout_metachar golden + TestResendCommandQuotesMetacharLabel pin the argv-level round-trip). Direction (a) family bind window TRIED AND REVERTED (measurement disproved its premise); (c) herder redeliver DEFERRED (same async dependency, heavier). Root cause routed upstream: TASK-029 candidate 12 (hcom should publish launch_context.pane_id for codex — collapses the class). Merged a475af8; hera gates green three times (worktree R1/R2 + post-merge main 19/19). Review: zana REQUEST-CHANGES (1 P2) => APPROVE (argv-level verification). Live-smoked on a real codex spawn.
<!-- SECTION:NOTES:END -->
