---
id: TASK-058
title: 'wave A4: one-shot v1 registry migration — dormant default (AC-36, spec 5.4)'
status: In Progress
assignee: []
created_date: '2026-07-08 05:55'
updated_date: '2026-07-08 08:57'
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

created: 2026-07-08 08:48
---
[hera 2026-07-08] Worker DONE (#9803): single commit 1d25330. HERA GATE GREEN from worktree: vet/test both modules (registry -count=1 fresh), 24/24 suites incl new check-registry-migration.sh. Adversarial review dispatched: review-a4-mori (guid 6f88e537, own tab), brief napkins/run-herder-dx/brief-review-a4.md — leads with the declared copy-truncate-instead-of-rename deviation (all three crash windows: post-copy pre-truncate re-trigger vs 0444 archive; post-truncate partial reseed recovery + idempotence; ENOSPC mid-reseed with archive as sole pre-state), plus trigger discriminator robustness, retired-guids-archive-only legality, byte-stability (map-iteration/timestamp/ordering determinism — A1 lesson), the rode-along sidecar resurrection fix (in-scope vs creep ruling), namespace path/record coexistence, real-shape torture coverage, golden-enshrinement check (A2 lesson). Worker BACKLOG item filed as TASK-066 (namespace_id consumer resolution). MEDIUM+ blocks merge.
---

created: 2026-07-08 08:57
---
[hera 2026-07-08] Opus adversarial verdict (review-a4-mori, #9974): NOT CLEAN — 1 BLOCKER + 1 MEDIUM, both in the crash windows the brief targeted. BLOCKER (reviewer REPRODUCED): crash mid-reseed with surviving node-row prefix -> recovery reseeds from archive (v1, no node rows) while ensureMigrationNode skips the mint (marker matches the partial live's node row) -> ZERO node_registered rows, all sessions node-stamped -> node gate refuses every subsequent write, registry wedged with misleading guidance. The passing empty-live test sits next to the broken most-likely case (node row is written FIRST in the reseed). MEDIUM: archive tmp written without fsync (dir fsync commits rename, not data) — power-loss can leave dir-entry-present/content-torn archive while live is already truncated+reseeded = pre-state audit trail unrecoverable; existing archive trusted on len>0 with no verification -> a torn archive from a prior crash silently migrated from. Rulings: copy-truncate strategy ACCEPTED (fix the windows); namespace path/record coexistence ACCEPTED (same-source, cannot disagree); sidecar no-resurrect collateral ACCEPTED in-scope. LOW (retired guids invisible to list --all + dangling forked_from): DEFERRED to A5 by plan definition (archive consultation is TASK-059 scope) — recorded, not waved. NIT (golden filename asserts opposite) + D9 upgrade-dormancy note -> fix round / rollout notes. Probed clean: trigger discriminator, reseed determinism (A1 class avoided), dedupe, real shapes, 0444 handling, mixed files. Fix round dispatched to wave-a4-pita (delivered).
---
<!-- COMMENTS:END -->
