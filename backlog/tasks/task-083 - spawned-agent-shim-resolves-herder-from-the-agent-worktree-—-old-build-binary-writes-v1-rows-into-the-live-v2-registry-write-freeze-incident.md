---
id: TASK-083
title: >-
  spawned-agent shim resolves herder from the agent worktree — old-build binary
  writes v1 rows into the live v2 registry (write-freeze incident)
status: To Do
assignee: []
created_date: '2026-07-08 23:53'
labels: []
dependencies: []
priority: high
ordinal: 83000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
LIVE P1 INCIDENT (2026-07-08, reported by tomo, repaired by hera — registry write path frozen ~5 min): herder spawn --worktree mission-spec --base sessions-missions-design created a worktree from a ref that PREDATES the v2 registry migration. The spawned agent booted, its bootstrap shim resolved herder from the AGENT OWN worktree checkout (cluster-A TASK-013/019 genre, new costume: spawned-agent shim resolution rather than suite env), and the old build appended a V1-FORMAT row (no kind/event/recorded_at) to the SHARED live registry. Every subsequent v2 locked write then loaded a projection containing a LegacyV1 row, armed migration recovery, collided with the real migration archive (0001-v1-migration.jsonl, different bytes), and fail-closed refused — fleet-wide write freeze until the poison row was excised.

FIX: the write path an agent uses against the shared registry must match the registry schema generation. Directions: (a) spawned-agent shim pins the SPAWNER herder build (HERDER_BIN or equivalent) for registry-writing commands instead of resolving from the agent checkout; or (b) build/schema handshake — a herder build refuses to append to a registry whose latest schema generation it does not understand, with a message naming the mismatch and remedy (upgrade checkout / use spawner build). Note (b) overlaps the companion write-time hardening task; this task owns the SHIM RESOLUTION side. Incident evidence preserved: registry backup registry.jsonl.pre-081-p1-repair.235228.bak.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A worktree spawn based on a pre-v2 ref can no longer append v1-shape rows to the live registry: shim pins the spawner build for registry writes, or the old build is refused with a mismatch message naming the remedy
- [ ] #2 The incident repro (spawn --worktree X --base <pre-migration ref>, agent boots, agent-side herder write) is covered by a suite or a documented manual repro executed once
- [ ] #3 Chosen direction (pin vs handshake vs both) recorded with rationale
- [ ] #4 gate green: go vet+test both modules, all check suites
<!-- AC:END -->
