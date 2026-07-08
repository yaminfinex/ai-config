---
id: TASK-045
title: >-
  codex spawns never bind to hcom 0.7.23 bus (pty-only, session_id none, flagged
  stale, name-capture timeout, prompt undelivered) — split AND new-tab; claude
  fine
status: To Do
assignee: []
created_date: '2026-07-08 04:49'
updated_date: '2026-07-08 04:59'
labels: []
dependencies: []
priority: high
ordinal: 45000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Reported by lale (market-sim run, bus msg #5477, 2026-07-08), blocking their codex-primary agent policy — they are falling back to claude workers and want a ping on that thread when fixed. Repro: herder spawn --agent codex --new-tab → pane renders empty text, no bus bind (bind-timeout 60000ms, name NOT captured), registry record flaps done/idle. Reproduced twice back-to-back (guids 7b4ad19f, ef4b6441, both culled). codex resolves to the herder shim (~/Coding/ai-config/tools/herder/shims/codex), codex-cli 0.142.5, runs fine standalone. claude spawns in the SAME workspace bind fine. Hypotheses: (a) --new-tab shells may not inherit the spawning env/PATH that splits do — shim or mise resolution fails silently in the fresh tab → codex never execs → empty pane + no bind (check what the tab's login shell sees vs a split); (b) TASK-036's codex boot latency exceeding the 60s bind window — but empty pane rendering suggests the process never drew at all, so (a) more likely; registry done/idle flap during a never-bound spawn may be its own sidecar bug. Cross-ref TASK-036 (codex bind_timeout, wave 7). Verify with a --split spawn of codex in lale's workspace as the control.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
UPDATE from lale control test (bus #5542, 2026-07-08): --split right fails IDENTICALLY to --new-tab, killing the new-tab env/PATH hypothesis. Refined symptoms: codex TUI boots fine and sits idle at its prompt (not a no-boot); hcom binding stays PARTIAL — pty-only, session_id none, flagged stale on hcom list (matches venue-iface-bobo/tina, lale's probe, and hera's earlier smoke36-kure/probe2-mako — signature predates the herdr handoff and postdates the hcom 0.7.22->0.7.23 upgrade); name capture times out at 60s; initial prompt never delivered. Root-cause suspicion moves to the codex bind path in hcom 0.7.23 (hooks/session registration for codex never completes; pty capture alone succeeds). Investigate: hcom 0.7.23 changelog/codex integration, herder hookcmd shim for codex (x-ref TASK-040 reTag fix — did codex tag-line capture regress differently?), and compare a raw 'hcom 1 codex' spawn outside herder to isolate herder vs hcom. SECOND symptom (herder registry, pre-handoff herdr 0.6.10): lale's three codex spawns all registered the SAME pane id w655fb01faa5682c-3 (wrong/duplicate), so cull targeted wrong records — pane-id capture at spawn time races or falls back when bind fails; x-ref TASK-046. lale's run continues codex-primary-violating on claude workers; ping bus thread #5477/#5542 when fixed.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 04:59
---
Isolation result from lale (bus #5598, 2026-07-08) — root cause narrowed to the HERDER SHIM PATH, not env inheritance and not upstream hcom: (1) raw 'hcom 1 codex' binds fine (<1 min, agent mazo); (2) herder-spawned codex post-hcom-upgrade binds ~6 MINUTES after spawn (probe re-registered as probe-codex-dove well past the 60s name-capture window) — slow-boot, TASK-036 flavor, upgraded from no-boot; (3) pre-upgrade codex spawns (venue-iface-bobo/tina) never bind, permanently stale. Fix question: why does the shim/launch path delay codex hcom registration by minutes? Suspects: sidecar/hookcmd startup ordering for codex, HERDER_HOOK_HCOM shim indirection, or codex notify/hook config injection racing the TUI. Bonus TASK-046 confirmation in same test: cull mistargeted (pane-reassigned warning) and the culled pane survived to bind later.
---
<!-- COMMENTS:END -->
