---
id: TASK-058
title: 'wave A4: one-shot v1 registry migration — dormant default (AC-36, spec 5.4)'
status: In Progress
assignee: []
created_date: '2026-07-08 05:55'
updated_date: '2026-07-08 08:32'
labels: []
dependencies: []
priority: high
ordinal: 58000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Plan unit A4 (spec-plan-wave-a.md). Triggered at first v2 WRITE on a v1 file (post-A3 so migration rows are node-attributed): rotate v1 to archive untouched -> reseed one-row-per-non-retired-guid -> closed=>retired, active=>unseated (DORMANT DEFAULT per ratification — no live probing; live occupants re-seat via sidecar observation, enroll, or wave-F reconcile) -> sids[] from provenance.tool_session_id else continuity:assumed -> namespace minting -> legacy keys (team, short_guid) dropped -> idempotent re-run. Tests: golden real-shape v1 sample (corpse-actives, byte-duplicate rows, teams-era rows); migrate twice = identical file. Sizing: 1159 rows/485 guids/1.1MB (spec-memo-migration-inventory.md). Depends: A3 (TASK-057). Adversarial review mandatory.
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 08:30
---
[hera 2026-07-08] A3 merged; dispatching A4 (one-shot v1 migration) to a fresh codex worker.
---

created: 2026-07-08 08:32
---
[hera 2026-07-08] Dispatched: codex worker wave-a4-pita (guid 2eb8dbfc), worktree wave-a4-migration (workspace wB), brief napkins/run-herder-dx/brief-wave-a4.md. Brief updates vs the original plan: trigger re-interpreted for the post-A2 MIXED file (any legacy-v1 row present; ravu on ambiguity), crash-window recovery between rotate and reseed required, archive scheme must not corner A5, reseeded rows are full snapshots node-stamped via the A3 envelope. Dormant default (D9): NO live probing. Brief-template lessons applied (commit-on-branch explicit, no self-arranged reviewers). Adversarial review mandatory (migration = engine risk).
---
<!-- COMMENTS:END -->
