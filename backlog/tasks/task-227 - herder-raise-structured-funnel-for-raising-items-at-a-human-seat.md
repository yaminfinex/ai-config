---
id: TASK-227
title: 'herder raise: structured funnel for raising items at a human seat'
status: Done
assignee: []
created_date: '2026-07-15 05:02'
updated_date: '2026-07-15 08:38'
labels:
  - herder
dependencies: []
priority: high
ordinal: 226500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner directive 2026-07-15 (SUPER HIGH): agents need a deliberate, well-formed way to open an item on the owner's desk. Add a raise verb to herder (comms is herder's domain; mission-control holds the seat and renders; mish resolves missions — the raise funnel is transport).

Verb shape (design fully adjudicated by owner — do not relitigate; decision record: ~/Coding/missions/missions/2026-07-15-mission-control/artifacts/raise-doctrine-design.md):
  herder raise --context '<cold-open sentence(s)>' --expects decide|act|reply|read [--thread <slug>] [--mission <slug>] -- '<body>'

Settled decisions:
- REFUSES without --context and --expects; refusal names the missing field and the remedy (cause+remedy style).
- Emits an ORDINARY bus send to the CONFIGURED seat (no new message shape): context as first line, intent derived from expects (reply/decide -> request, else inform).
- Mission association: explicit --mission flag wins, else mish resolve at cwd.
- Seat name is configuration, not hardcoded (owner seat may rename).
- Day-one scope: raises at seats only; no agent-to-agent generalization.
- Future (NOT this task): MCP tool wrapping the same funnel.

Independent of the join/spawn-mission lane — parallelizable.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A raise lands as a managed thread in mission control's inbox with correct expects
- [x] #2 A bare raise attempt (missing --context and/or --expects) refuses with actionable text naming the missing field
- [x] #3 Unit tests cover the refusal matrix (each missing/invalid field combination)
- [x] #4 Intent derivation: reply/decide -> request; act/read -> inform; covered by tests
- [x] #5 No new bus message shape; the send is an ordinary hcom send to the configured seat
<!-- AC:END -->











## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged 16f4a9e (--no-ff; one conflict in check-help-contract.sh resolved as union of 227/228 verb lists, validated against cli.go registrations by the incumbent post-merge). Shipped: herder raise --context --expects decide|act|reply|read [--thread] [--mission] -- body. Wire contract (mc-parsed literals): context line 1; 'Expects: <v>' line 2; 'Mission: <slug>' line 3 when resolved; blank; body. Intent: decide/reply->request, act/read->inform. Seat config raise.seat in node-local config.json, fail-closed, no compiled default, read-only config access. Mission: explicit --mission wins via mish resolve; ambient cwd fallback; no_context -> valid missionless; refusals stop before send (zero partial sends proven). Review: opus incumbent fix-list -> F1 (line-boundary guard under-inclusive: 8 Unicode separators beyond \r\n enabled a proven read->decide Expects-spoof for Python/JS peers) fixed red-first with all ten chars individually mutation-pinned + calibration N1 folded (resolved-slug line-break refusal); delta APPROVE with independent re-verification. Grok calibration APPROVE (ledger row 19). Gates: independent 60/60 at 47c3770, re-gate 60/60 at caea1d2, post-merge 60/60 on main (first restarted after a disclosed mid-gate board-commit violation — run voided, restarted, green). Follow-ups filed: dual-resolver divergence watch; help-contract coverage gap; ExitError-path test nit recorded here (behavior correct, unpinned).
<!-- SECTION:NOTES:END -->
