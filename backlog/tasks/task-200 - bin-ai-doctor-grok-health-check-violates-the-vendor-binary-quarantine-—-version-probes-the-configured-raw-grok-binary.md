---
id: TASK-200
title: >-
  bin/ai-doctor grok health check violates the vendor-binary quarantine —
  version-probes the configured raw grok binary
status: To Do
assignee: []
created_date: '2026-07-14 01:24'
labels: []
dependencies: []
ordinal: 199000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
LIVE EVIDENCE 2026-07-14: two independent workers running the repo-mandated pre-change bin/ai-doctor gate both tripped the house rule 'NEVER invoke the raw vendor grok binary (not even --version)' — ai-doctor's grok health section transitively executed the configured vendor binary (/home/grace/.grok/downloads/grok-0.2.99-linux-x86_64) for a version probe, reporting 0.2.99. Both runs used throwaway HOME/XDG roots so live ~/.grok was verified untouched (orchestrator-checked mtimes), but the trap is structural: the mandated gate itself performs the forbidden invocation, so every compliant worker trips it — with a real HOME this is the exact class that caused both prior live ~/.grok contamination events (raw --version probe let the vendor binary rewrite active_sessions.json + user-guide docs). Fix space: derive the version WITHOUT executing the vendor binary (filename/metadata/download manifest), or gate the grok section behind an explicit opt-in flag, or route the probe through the herder shim contract which owns safe vendor interaction. Acceptance: a default bin/ai-doctor run executes zero vendor grok binaries (pin with a PATH-instrumented test if feasible) while still reporting grok install status; the vendor-quarantine rule gets a code-level enforcement point instead of relying on operator discipline.
<!-- SECTION:DESCRIPTION:END -->
