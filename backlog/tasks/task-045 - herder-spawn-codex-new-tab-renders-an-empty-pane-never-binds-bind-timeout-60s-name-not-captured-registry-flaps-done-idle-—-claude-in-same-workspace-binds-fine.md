---
id: TASK-045
title: >-
  codex spawns never bind to hcom 0.7.23 bus (pty-only, session_id none, flagged
  stale, name-capture timeout, prompt undelivered) — split AND new-tab; claude
  fine
status: To Do
assignee: []
created_date: '2026-07-08 04:49'
updated_date: '2026-07-08 06:51'
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

created: 2026-07-08 05:24
---
Live experiment in flight (vibe #5926): the TASK-046 codex re-dispatch runs with HERDER_SPAWN_BIND_MS=480000 (8x the default window). Outcome is a direct datapoint: if the worker binds and gets verified prompt delivery end-to-end, the ~6min shim bind latency is bounded and an extended window is a viable interim mitigation; if not, latency is unbounded/fatal and the shim fix escalates.
---

created: 2026-07-08 05:33
---
Extended-bind experiment result (vibe #6107, applied by hera) — MECHANISM SHARPENED: HERDER_SPAWN_BIND_MS=480000 did NOT fix spawn-native delivery. Spawn exited delivery_result=bind_timeout / hcom_capture=not_found even though the bind demonstrably COMPLETED on the hcom side well within the window (worker task046-fulo live and listening). So the defect is not (only) codex boot latency: herder spawn's NAME-CAPTURE loop never sees a bind that hcom itself completed — capture-loop-side. Additional symptom for the ticket: the registry row gets minted with EMPTY hcom_name, so herder send cannot resolve the worker afterwards (needs re-enroll or reconcile once TASK-046 lands). Investigation focus: what the capture loop polls (hcom list? events? launch tag?) and why a completed codex bind is invisible to it — note hcom 0.7.23 changed tag-line emission (x-ref TASK-040 reTag) and lale's earlier data showed pty-only/session_id-none partial binds; the capture key may be looking at the wrong field for codex. WORKAROUND OF THE DAY (proven end-to-end): spawn codex normally, watch hcom list for the bus name, deliver the initial prompt directly via hcom send to the live name — injected first try, worker acked and implementing.
---

created: 2026-07-08 06:36
---
ROOT CAUSE (vibe diagnosis #6902, read-only, live-validated): both capture signals are STRUCTURALLY dead for codex under hcom 0.7.23. awaitBind (spawn.go:1621) learns the childs name only via (a) sidecar registry enrichment, which requires pane-correlation launch_context.pane_id==paneID (sidecar.go:211), or (b) direct roster match on launch_context.pane_id (spawn.go:1626). Claude rows carry launch_context.pane_id via the hook handshake (hooks_bound:true). Codex rows never complete the hook handshake (hooks_bound:false, session_id empty) and their launch_context has ONLY the internal process_id — no pane_id — so neither signal can EVER fire; window length is irrelevant (matches the 8-min experiment and all three corpses). The late-appearing name is the pty/process-bound registration, which makes the bus name live but feeds nothing herder watches.

FIX RANKING: F1 (primary, sidecarcmd-only): sidecar correlates its panes agent process to the roster via HCOM_PROCESS_ID read from /proc/<pid>/environ of the RUNNING agent (authoritative, unlike TASK-043 inherited-shell env) — proven live byte-equal to roster launch_context.process_id; HERDER_GUID + HCOM_INSTANCE_NAME in environ as belt-and-braces; feeds awaitBind through the existing enrichment path, no spawn.go change, TASK-033-compliant positive child-specific signal. F2 (spawn-side variant): collides with A2 — only if F1 latency proves insufficient. F3 (upstream): hcom 0.7.23 codex hook binding broken — filed on TASK-029 regardless; also the only thing that revives codex sid-reporting for TASK-053.

DISPATCH DECISION (hera): F1 GO now, before A2 merges — sidecarcmd-only, but NOTE A2s scope also touches sidecar enrichment call sites; whichever branch merges second gets an explicit conflict-check + regate. Interim workaround remains the dispatch path.
---

created: 2026-07-08 06:51
---
[hera, from vibe review 2026-07-08 #7247] F1 delivered by task045-nina (482bfc7, fenced to sidecarcmd, worker gate green). Vibe review found ONE blocking defect: poll-loop enrichment at sidecar.go:152 requires a non-empty session_id, but codex rows have empty sids — if first correlation lands in the poll loop instead of bootstrap (real race: discoverRow can return the non-correlated fallback row; roster row can appear before /proc/<pid>/environ is scannable), hcom_name is never written and the TASK-045 symptom recurs with no recovery path. Same dead-recovery class as the TASK-053 miss; caught by the reachability check (run-log doctrine). Fix + 1 test requested from worker; on hand-back: hera gate + opus adversarial review. Sequencing: A2 also touches sidecar.go — second merge gets conflict-check + regate.
---
<!-- COMMENTS:END -->
