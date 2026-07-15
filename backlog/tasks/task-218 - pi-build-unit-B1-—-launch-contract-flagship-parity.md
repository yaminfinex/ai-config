---
id: TASK-218
title: pi build unit B1 — launch contract (flagship parity)
status: In Progress
assignee: []
created_date: '2026-07-15 00:52'
updated_date: '2026-07-15 00:53'
labels: []
dependencies: []
ordinal: 216000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
IMPLEMENT unit. Binding enumeration: docs/design/pi-first-class-design.md §13 B1 (merged to main; read it IN FULL — its six numbered items are the complete scope, each a bounded delta to an existing mechanism, file:line-anchored against the current repo).

Settled decisions (owner ruling + 11-round reviewed design — do NOT relitigate):
- Flagship parity: herder wraps `hcom pi` exactly as claude/codex; no journal/receipts/epochs/leases/lanes (all deleted by design).
- Global-bus-only: spawn --team with --agent pi REFUSES with cause+remedy; NO PI_CODING_AGENT_DIR pin ships.
- Provider required at spawn (--provider; missing/empty/unknown = typed refusal); exactly one named provider credential in launch env; PI_OFFLINE=1, PI_TELEMETRY=0.
- Vendor version RECORDED (never pinned) at launch/bind — PATH-resolve -> EvalSymlinks -> owning package.json, NEVER executing pi; current+previous timestamped observations.
- Bind predicate: tool==pi AND hooks_bound AND nonempty session UUID (name-only is negative); hard bind-timeout cleanup on BOTH prompted and promptless spawn paths (mirror grok shape).
- Resume/fork reconstruct from REGISTRY FACTS, never ambient env; missing provider fact = refusal.
- Doctrine carriage via HCOM_NOTES gated on probe P10; shim pi-start transform is the fallback; shim-first PATH chain is the interception invariant (P9).

Scope = §13 B1 items 1-6 verbatim: (1) registry+spawn additive fields (provider/model/vendor_version on v2 record; --provider; pi in --model allowlist); (2) --team refusal; (3) IsHcomCapable + exec-into-hcom-pi env branch; (4) bind capture fields+predicate+cleanup; (5) lifecycle reconstruction; (6) gates L1-L7 (§11) + probes P8/P9/P10 discharged and recorded + real-global-bus live smoke (collision-safe naming/cleanup, owner spend per §12 item 2 — STOP-AND-REPORT before spending, do not run the live smoke without orchestrator go).

Probes use isolated scratch state; NEVER touch the live hcom database, registry, or fleet seats. Full house battery + adversarial review. Activation stays OPT-IN (§13 closing paragraph) — this unit does not activate the family.
<!-- SECTION:DESCRIPTION:END -->
