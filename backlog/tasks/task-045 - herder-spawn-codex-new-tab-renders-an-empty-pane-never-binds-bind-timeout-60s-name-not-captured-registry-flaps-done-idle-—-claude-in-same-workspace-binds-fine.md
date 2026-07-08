---
id: TASK-045
title: >-
  herder spawn: codex --new-tab renders an empty pane, never binds (bind-timeout
  60s, name not captured), registry flaps done/idle — claude in same workspace
  binds fine
status: To Do
assignee: []
created_date: '2026-07-08 04:49'
labels: []
dependencies: []
priority: high
ordinal: 45000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Reported by lale (market-sim run, bus msg #5477, 2026-07-08), blocking their codex-primary agent policy — they are falling back to claude workers and want a ping on that thread when fixed. Repro: herder spawn --agent codex --new-tab → pane renders empty text, no bus bind (bind-timeout 60000ms, name NOT captured), registry record flaps done/idle. Reproduced twice back-to-back (guids 7b4ad19f, ef4b6441, both culled). codex resolves to the herder shim (~/Coding/ai-config/tools/herder/shims/codex), codex-cli 0.142.5, runs fine standalone. claude spawns in the SAME workspace bind fine. Hypotheses: (a) --new-tab shells may not inherit the spawning env/PATH that splits do — shim or mise resolution fails silently in the fresh tab → codex never execs → empty pane + no bind (check what the tab's login shell sees vs a split); (b) TASK-036's codex boot latency exceeding the 60s bind window — but empty pane rendering suggests the process never drew at all, so (a) more likely; registry done/idle flap during a never-bound spawn may be its own sidecar bug. Cross-ref TASK-036 (codex bind_timeout, wave 7). Verify with a --split spawn of codex in lale's workspace as the control.
<!-- SECTION:DESCRIPTION:END -->
