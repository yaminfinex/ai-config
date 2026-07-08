---
id: TASK-028
title: hcom 0.7.23 upgrade audit — re-ground the bootstrap/codex integration
status: Done
assignee:
  - audit-028-zoru
created_date: '2026-07-07 12:23'
updated_date: '2026-07-08 03:40'
labels: []
dependencies: []
priority: medium
ordinal: 28000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
hcom v0.7.23 is available (noticed 2026-07-07 mid run-herder-dx; machine runs 0.7.22 via mise). The herder integration is source-grounded in v0.7.22: sessionstart rewrite byte-faithfulness (TASK-001/002), codex developer_instructions merge + resume/fork strip predicate (TASK-014/017), -p background switch (TASK-010), pin/seed behavior (TASK-011). Before or when updating: diff v0.7.22..v0.7.23 source for changes to bootstrap.rs, hooks, launch/strip logic; re-run the full battery + the live smokes that pinned those behaviors; update mirrored predicates/constants if upstream moved. Degrade-safe design should hold (parse failure -> stock output) but the mirrored STRIP PREDICATE and -p switch are behavioral mirrors that can silently drift. Do NOT update mid-run; user decides timing.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Audit complete (audit-028-zoru, read-only; report on thread audit-028, 2026-07-08). Verdict: UPGRADE-WITH-CHANGES. Six of seven integration surfaces COMPATIBLE against real 0.7.23 source (receipt shape/query, list --json single-object contract, roster launch_context, events sub semantics, send flags + HCOM_DIR incl. new protected-path guard herder never trips, queue-until-deliverable delivery core untouched — 0.7.23 churn is mostly Windows pty + omp tool). ONE BREAK (P1, silent): bootstrap.rs:92 changed the stock tag line to single quotes; herder's reTag (hook.go:235) requires double quotes => tag extracts empty => renderBootstrap silently drops the group-address line for tagged/team spawns; battery blind to it (canned double-quote fixtures). hera VERIFIED the claim locally (hook.go:235, hook_test.go:23, check-hook-bootstrap.sh:71). Bonus findings: codex developer_instructions now TOML-encoded upstream (net WIN — likely fixes a latent drop of herder's codex bootstrap under 0.7.22); strip predicate + -p/--print mirror byte-compatible. Fix routed to TASK-040 (quote-agnostic reTag + dual-style fixtures; live tagged-spawn smoke deferred to upgrade time). UPGRADE SEQUENCE for the user: merge TASK-040 -> hcom update -> live tagged-spawn smoke -> done. No hcom update was run.
<!-- SECTION:NOTES:END -->
