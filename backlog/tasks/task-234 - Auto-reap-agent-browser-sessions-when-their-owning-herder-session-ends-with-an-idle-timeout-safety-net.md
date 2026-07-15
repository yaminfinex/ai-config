---
id: TASK-234
title: >-
  Graceful cull: pre-cull release notice + ack window; resource cleanup is the
  agent's job
status: To Do
assignee: []
created_date: '2026-07-15 07:14'
updated_date: '2026-07-15 07:17'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 233500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner-corrected design (2026-07-15) after a field cleanup (6 orphaned agent-browser daemons, 84 chrome processes, leaked by two culled reviewers). REJECTED shape: herder closing agent-owned resources on cull (close-on-cull per resource type) — that is an overstep; herder cannot know every resource class agents acquire (browsers, tunnels, containers, temp cloud resources), and per-resource close logic in the lifecycle layer does not generalize.

RULED shape — protocol over knowledge:
1. DOCTRINE (cheapest, immediate): agents close their own external resources before reporting DONE, and orchestrator briefs say so. Spawn-context/skill text carries it (evergreen skill wording rides the owner-reviewed harvest path).
2. GRACEFUL CULL PROTOCOL (herder's actual lane): cull gains a pre-cull release notice — deliver 'release external resources, then ack' to the target, wait a BOUNDED window for ack (or observed idle-after-notice), then cull regardless. Cull must never hang on an unresponsive/context-dead agent; --now skips the notice for emergencies. The notice is generic — herder names no resource types.
3. SAFETY NET for crashed/never-acked agents: a periodic host-maintenance/doctor sweep OUTSIDE herder core, per resource class, with the proven guards (owner GUID absent from liveness + grace period + no live client at socket level + never age alone). The agent-browser sweep is the first instance; others follow the same pattern.
4. Optional belt-and-braces: finite default AGENT_BROWSER_IDLE_TIMEOUT_MS in herder-launched agent env (self-heal without anyone knowing about the resource) — env-level, not lifecycle-level.

Design checkpoint required before implementation: notice delivery mechanism + ack shape + window bounds + interaction with retire/adopt/compact paths.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Normal cull/retire closes every browser session launched by that agent (daemon, Chrome tree, runtime sidecars gone)
- [ ] #2 Crash/SIGKILL simulation reaped by the safety net after grace period; active owners/clients never reaped
- [ ] #3 Repeated cleanup safe; auditable structured logs
- [ ] #4 Integration tests: multi-session agent, normal close, crash, stale sidecars, PID-reuse protection, persistent opt-out
- [ ] #5 Worker-brief/spawn doctrine text: close external resources before DONE (run-local now; evergreen via owner-reviewed skill harvest)
- [ ] #6 herder cull sends a generic release notice and waits a bounded ack/idle window before proceeding; never hangs; --now bypass
- [ ] #7 Doctor/host sweep for agent-browser orphans with the proven guards (owner-absent + grace + no live client, never age alone)
- [ ] #8 Herder core contains zero resource-type-specific close logic
<!-- AC:END -->
