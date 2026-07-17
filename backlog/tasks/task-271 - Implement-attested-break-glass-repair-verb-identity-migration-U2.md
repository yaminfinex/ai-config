---
id: TASK-271
title: 'Implement: attested break-glass repair verb (identity migration U2)'
status: In Progress
assignee: []
created_date: '2026-07-17 04:27'
updated_date: '2026-07-17 07:22'
labels:
  - herder
  - identity-migration
dependencies:
  - TASK-270
priority: high
ordinal: 270500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
IMPLEMENT unit, stage 2 of the ratified identity migration. GROUND TRUTH (settled, double-reviewed + owner-ratified — do NOT relitigate; deviations are STOP-AND-REPORT to @hera, read this twice): docs/design/2026-07-17-identity-migration-plan.md §U2 and docs/design/2026-07-17-identity-architecture-target.md §3.3 (the damage-shape table there is this stage's acceptance sheet). Memo §4 keep-list = HARD constraints. DEPENDS ON stage 1 (shared completion step): the verb terminates in its attestation-consuming mode.

OWNER-RATIFIED AND BINDING: Branch B is the trust anchor (posture reduction, tripwire-not-wall) — the verb's claim is 'a deliberate, named, logged action by the OS account controlling the pane'; same-uid takeover through the verb is accepted posture; value = narrowness + rate limit + loudness at use + normal-path audit record (NOT claimed tamper-evident). Branch A is not adopted; do not implement it.

Goal: one new verb (new package, e.g. internal/repaircmd/) rebinding a SINGLE named identity field — stored bus name, recorded session id, or launch context, exactly this vocabulary; registry seat coordinates and label/role/lineage are refused by construction — on a single row. Proof = explicit attestation naming row+field+new value (unforgeable from flags alone or piped input) + seat-control corroboration (nonce round-trip through the claimed live pane; terminal-id match where intact). Rate-limited, loud on stderr, appends an attested evidence-classed binding, preserves label/role/lineage, one field per invocation, never runs from automated paths. An attested rebind tombstones the specific superseded binding by field + durable binding id in the SAME locked batch (history retained) per T6 correction semantics. Wrong-nonempty launch context: never rewritten (keep-list fence) — record attested authorization, prescribe the vendor-row recreate protocol; if upstream's reclaim guard refuses, report the shape as upstream-gated pointing at the documented recovery recipe, never pretend termination. The verb surface also carries reissue-credential (authenticated here — attestation + corroboration under Branch B, no field rebound, ends in re-completion minting a new token under the rotation commit protocol per architecture §3.1); full credential semantics land in stage 3, but the authentication surface and operation shape are this stage's scope. WRITE-SPINE SCOPE: attested-binding event kind with tombstone markers keyed by durable binding id, normalizer ownership + carry rules, atomic locked batch (rebind + completion in one UpdateLocked transaction). Operator documentation under docs/.

DESIGN CHECKPOINT REQUIRED BEFORE CODE (verb surface, attestation UX, event/tombstone shape, batch atomicity). Adversarial review is orchestrator-dispatched after your DONE; do not arrange reviewers yourself. The orchestrator's bus address is @hera; there is no @orchestrator alias. Commit on your unit branch before DONE. Hygiene: no agent names, task numbers, run identifiers, or SHAs in code comments, fixtures, goldens, or refusal text.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Design checkpoint note approved by orchestrator BEFORE any code
- [ ] #2 Every plan §U2 test scenario implemented and green, including: no-attestation refusal; failed nonce/terminal corroboration refusal; the Branch B forgery-path test documenting the accepted bypass with audit row + rate limit asserted; successful bus-name rebind with label/role/lineage byte-identical; correction-cell test (tombstoned stale live-verified binding is a non-candidate, not outranked); absent-vs-unavailable test (live source errors never arm history adjudication); rotation-survival test (tombstone + binding id byte-identifiable after reseed, no id re-keyed); wrong-nonempty launch-context no-rewrite both legs; out-of-vocabulary rebind refused; rate-limit window refusal; crash mid-batch leaves prior row intact
- [ ] #3 Season terminal-state fixtures: each recorded shape (bus-name unrecoverable, duplicate seated row aftermath, retired-row-owns-live-sid, wrong-nonempty pane) reproduced and cured by the documented attested sequence, or honestly reported as the one upstream-gated shape — architecture §3.3 table verified row by row
- [ ] #4 reissue-credential operation authenticated on this surface, ending in re-completion under the rotation commit protocol; never credential-gated
- [ ] #5 Keep-list re-audit of the final diff; operator docs for the verb landed under docs/
<!-- AC:END -->
