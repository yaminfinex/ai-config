---
id: TASK-055
title: 'wave A1: registry v2 record types + kind-partitioned projection (AC-30, AC-33)'
status: In Progress
assignee:
  - codex-66dd90b8
created_date: '2026-07-08 05:55'
updated_date: '2026-07-08 06:03'
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
<!-- COMMENTS:END -->
