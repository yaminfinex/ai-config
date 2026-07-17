---
id: TASK-271
title: 'Implement: attested break-glass repair verb (identity migration U2)'
status: Done
assignee: []
created_date: '2026-07-17 04:27'
updated_date: '2026-07-17 10:01'
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
- [x] #1 Design checkpoint note approved by orchestrator BEFORE any code
- [x] #2 Every plan §U2 test scenario implemented and green, including: no-attestation refusal; failed nonce/terminal corroboration refusal; the Branch B forgery-path test documenting the accepted bypass with audit row + rate limit asserted; successful bus-name rebind with label/role/lineage byte-identical; correction-cell test (tombstoned stale live-verified binding is a non-candidate, not outranked); absent-vs-unavailable test (live source errors never arm history adjudication); rotation-survival test (tombstone + binding id byte-identifiable after reseed, no id re-keyed); wrong-nonempty launch-context no-rewrite both legs; out-of-vocabulary rebind refused; rate-limit window refusal; crash mid-batch leaves prior row intact
- [x] #3 Season terminal-state fixtures: each recorded shape (bus-name unrecoverable, duplicate seated row aftermath, retired-row-owns-live-sid, wrong-nonempty pane) reproduced and cured by the documented attested sequence, or honestly reported as the one upstream-gated shape — architecture §3.3 table verified row by row
- [x] #4 reissue-credential operation authenticated on this surface, ending in re-completion under the rotation commit protocol; never credential-gated
- [x] #5 Keep-list re-audit of the final diff; operator docs for the verb landed under docs/
<!-- AC:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-17 07:34
---
Design checkpoint APPROVED after one amendment round. Note covered: exact-guid two-operation verb surface (rebind + reissue-credential) as sole production caller of the completion attested arm (source-inventory pin updates from none to exactly the repair package); /dev/tty challenge-sentence attestation, no flag/stdin/env path; cross-pane seat-control ceremony amended to a READ-ONLY observer of the claimed pane — visible-source-only, two consecutive stable reads, operator places nonce manually, nonce REMOVAL required before final attestation, both operator hazards (Enter-submits-into-live-composer; draft destruction) named in CLI warning + docs; attested_binding event as one self-contained snapshot in one UpdateLocked (proof pre-lock, preflight re-verify + rate limit + tombstone selection in-lock; crash leaves prior snapshot authoritative); tombstones keyed by durable binding id with legacy factless values materialized-then-tombstoned in the same snapshot; launch-context = authorization record only, never rewritten, upstream-gated branch honest; reissue lands the authenticated operation + completion commit point with the U3 token boundary stated honestly; fixed 10-min per-guid committed-operation window checked under lock, refusal names limit + remaining time, failed attempts loud but non-consuming (anti-DoS-by-typo rationale). Branch B honesty maintained throughout (tripwire not wall, no tamper-evidence claim). Code unlocked.
---

created: 2026-07-17 08:34
---
Adversarial review round 1 (incumbent opus + grok calibration, serialized slots, both released byte-clean): FIX ROUND REQUIRED. Credit where earned (both reviewers): no ceremony bypass exists (tty-only attestation, read-only pane observer verified hostile), atomicity real (in-lock failure injection left registry byte-identical), tombstone/adjudication semantics genuinely pinned, all six architecture damage-shape rows covered by fixtures. Seven consolidated findings: 2 P1 — source-inventory fence narrowed to one composite literal (variable-form arming invisible, gofmt defeats it, assignment-form rewrite loses the legit caller; all executed) and the in-lock rate-limit wholly unpinned (deleting all three in-lock checks stays green; sole test only trips preflight; true race commits once via the ANCHOR with the loser refusal naming the wrong mechanism vs the contract's named limit+window); 3 P2 — Branch B forgery test contains no forgery (stubbed proof, no pty/pane loopback: the accepted bypass is invisible), global SID-projection redefinition smuggled to satisfy one fixture (~6 reader packages unpinned, incumbent-only find), attestation prompt unbounded (timeout leg unimplemented/untested); 2 P3 — empty-challenge guard absent (fails closed today by coincidence), lifecycle-carry of the new histories untested. Lens h (builder flake claim) adjudicated ENVIRONMENTAL by the incumbent on four self-verified legs after rejecting the builder's file-level reasoning as insufficient (path-level isolation, post-body failure mode, zero registry surface in test, pre-existing boarded class with matching signature); false-friend test name flagged for the existing cleanup task. Fix round dispatched.
---

created: 2026-07-17 10:01
---
MERGED f5c28e3 (--no-ff, pushed); post-merge battery on main green 61/61 + 4 modules (tail read, counted). Final head 81c364c = design + 7-finding hardening + P2-D pin. Chain: design checkpoint (approved after cross-pane ceremony amendment) -> red-first build -> review round 1 (opus incumbent kivu + grok calibration lote, 7 findings incl. 2 P1: defeatable source-inventory fence, wholly-unpinned in-lock rate-limit) -> fix round -> delta (APPROVE-pending-P2-D: 7/7 closed mutation-verified, one new pin-strength gap) -> P2-D micro pin -> final APPROVE (8/8, every finding reverted-and-verified). Incumbent verified merge faithfulness (approved head byte-identical to shipped). Orchestrator gates: independent + final-head + post-merge all 61/61, identifier sweep clean. Branch B honesty maintained (tripwire, no tamper-evidence claim); real /dev/ptmx forgery test documents the accepted same-uid bypass; global SID-projection override reverted to scoped v2-binding adjudication. Residuals boarded: TASK-278 (cullcmd flake, not-delta-attributable), TASK-223 (grokbridge race, not folded). appendAuthorization conscious-ack corrected (RepairLaunchContext refuses not rewrites; bypass exists because completion would refuse pre-write and strand the mandated attestation).
---
<!-- COMMENTS:END -->
