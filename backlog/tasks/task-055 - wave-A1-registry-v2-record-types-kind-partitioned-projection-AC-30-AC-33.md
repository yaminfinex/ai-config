---
id: TASK-055
title: 'wave A1: registry v2 record types + kind-partitioned projection (AC-30, AC-33)'
status: In Progress
assignee:
  - codex-66dd90b8
created_date: '2026-07-08 05:55'
updated_date: '2026-07-08 06:15'
labels: []
dependencies: []
priority: high
ordinal: 55000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Spec-derived (docs/specs/herder-spec.md RATIFIED 1964ae6; plan: napkins/run-herder-dx/spec-plan-wave-a.md unit A1). New registry/v2 types per spec 5.1 (session/node/namespace/epoch; kind absent = session). Loader: JSONL scan, quarantine malformed lines (warn, never fail CLI), partition by kind BEFORE per-guid collapse, file order authoritative (recorded_at display-only). Projection API: Sessions()/Nodes()/Epochs() + anomaly list (unknown-node rows, double label holders, double-seated sessions — flagged, deterministic, loud). Legacy v1 rows load through the 5.4 mapping READ-ONLY (no rewrite in this unit). Tests: golden registries — mixed kinds, torn rows, duplicate labels, v1-only file.
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 06:03
---
A1-gate criterion (spec-ravu #6484): the golden v1 registry fixtures must be cut from the REAL row shapes catalogued in napkins/run-herder-dx/spec-memo-migration-inventory.md (6 shape variants, byte-duplicate rows, teams-era rows, corpse-actives) — NOT synthetic rows. That memo is ground truth for what the loader must survive; reviewer should check fixtures against it. spec-ravu on-call for spec ambiguity at wave gates — route worker questions there directly.
---

created: 2026-07-08 06:15
---
DONE report received (wave-a1-gino #6654, commit 5172774). hera gate re-run from worktree: vet clean, tests ok (herder 11 pkgs incl new registry/v2, bottle 5), 21/21 suites green (new check-registry-v2.sh included); tools/bottle/tests genuinely absent repo-wide, worker 0-count honest. Diff 9 files +680/-22; the -22 is the LIVE legacy-loader quarantine change -> adversarial review dispatched despite plan scoping review to A2-A4 (live path = engine risk): opus @review-a1-zumi, focused on quarantine behaviour change, AC-30 phantom-session risk, 5.4 mapping, anomaly determinism, fixture fidelity vs memo. Merge gates on verdict. Worker BACKLOG items: (1) ai-doctor env warnings pre-existing; (2) open design question for A2/A4 — do CLI consumers switch to v2 projection directly or keep adapting via legacy Record API through the transition (route to spec-ravu at A2 dispatch).
---
<!-- COMMENTS:END -->
