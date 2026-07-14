---
id: TASK-210
title: >-
  Amend pi design to flagship-parity delivery — delete DR-2/DR-3
  delivery+authority machinery
status: In Progress
assignee: []
created_date: '2026-07-14 22:26'
updated_date: '2026-07-14 22:28'
labels: []
dependencies: []
ordinal: 209000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OWNER RULING 2026-07-14 (gold-plating audit candidate 1, ruled IN): amend docs/design/pi-first-class-design.md to adopt flagship-parity delivery — herder wraps 'hcom pi' exactly as claude/codex are wrapped (add pi to the launch capability gate IsHcomCapable at tools/herder/internal/launchcmd/launch.go, pin the Pi-home env var to its DEFAULT location beside the existing config-dir pins, exec into the native hcom launcher) — and DELETE from the design: the durable spool journal + queued/injected/settled state machine, settlement-correlated receipts, crash replay + duplicate reconciliation + nudge budget, ownership epochs + activation fencing + launch-attempt protocol, progress-attested driver lease, capability/control lanes, the herder-owned TypeScript extension + its activation predicate, and spool bounds. RETAIN unchanged: credential env scoping (settled), launch-contract env pinning + recorded vendor version (install-latest, version recorded not pinned), herder as spawner/registry owner (settled), the observer/sesh session-JSONL adapter (orthogonal to delivery), doctrine content. REGISTER as an owner-signed delta: pi seats accept the flagship crash window (injection-time receipt, no replay, re-prompt recovery) — cite the flagship crash/parity memo and the gold-plating audit as the evidence base; keep the design's honesty-register style. The hcom-native pi characterization's keep-custom decision is SUPERSEDED by this ruling — say so with provenance, do not delete its record. DESIGN unit, docs-only. Settled, do not relitigate: the ruling itself, default homes, credential scoping. Ground truth: docs/design/2026-07-14-delivery-machinery-gold-plating-audit.md (candidate 1 is the binding scope), docs/design/2026-07-14-flagship-hcom-crash-parity.md, docs/design/2026-07-14-hcom-native-pi-characterization.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Design doc amended: flagship-parity launch contract in, all DR-2/DR-3 delivery+authority machinery out, retained set intact and explicit
- [ ] #2 Owner-signed crash-window delta registered with evidence citations; superseded keep-custom decision marked with provenance, not erased
- [ ] #3 Implementation surface after amendment is explicitly enumerated (expected: a few launch-contract lines + observer adapter) so the build unit can be filed directly
<!-- AC:END -->
