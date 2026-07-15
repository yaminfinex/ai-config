---
id: TASK-234
title: >-
  Auto-reap agent-browser sessions when their owning herder session ends, with
  an idle-timeout safety net
status: To Do
assignee: []
created_date: '2026-07-15 07:14'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 233500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Prevention follow-up from a field cleanup (6 orphaned agent-browser daemons + full Chrome trees, 84 processes, leaked by two ended reviewer sessions; all reaped after socket-level proof of no live client). Problem: agent-browser daemons detach to PPID 1 and are intentionally persistent; agent termination never closes them — cleanup is cooperative via 'agent-browser --session <name> close', and nothing connects herder lifecycle termination to it. Idle timeout (AGENT_BROWSER_IDLE_TIMEOUT_MS) is disabled by default.

Settled design (from the investigation, design-first checkpoint still applies for the ownership-record shape):
1. PRIMARY: record launched agent-browser session names against the launching HERDER_GUID; every normal cull/retire/terminal-close path calls close for them (idempotent, bounded) before the session record retires.
2. SAFETY NET: periodic orphan sweep (ai-doctor/host maintenance): enumerate .pid/.sock sidecars, read daemon HERDER_GUID from environ, reap only when owner GUID absent from liveness state AND grace period exceeded AND IPC socket has no external client; structured logs before/after.
3. Finite default AGENT_BROWSER_IDLE_TIMEOUT_MS for herder-launched agents (30-60 min; refreshed by use) with explicit opt-out for intentionally persistent sessions.
4. Never kill solely by age; live client or live owner always preserved and surfaced.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Normal cull/retire closes every browser session launched by that agent (daemon, Chrome tree, runtime sidecars gone)
- [ ] #2 Crash/SIGKILL simulation reaped by the safety net after grace period; active owners/clients never reaped
- [ ] #3 Repeated cleanup safe; auditable structured logs
- [ ] #4 Integration tests: multi-session agent, normal close, crash, stale sidecars, PID-reuse protection, persistent opt-out
<!-- AC:END -->
