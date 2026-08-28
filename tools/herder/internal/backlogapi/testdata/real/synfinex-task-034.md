---
id: TASK-034
title: >-
  Notional Shift design: exact per-market market→collateral unit conversion for
  broad-precision asset sets
status: Done
assignee:
  - '@claude'
created_date: '2026-07-07 02:26'
updated_date: '2026-07-07 02:26'
labels: []
dependencies: []
ordinal: 34000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Design (then adversarially review) the per-market power-of-ten Notional Shift: qty_unit × px_unit = 10^d × collateral_unit, applied exactly once at the market→collateral boundary of the rust-raw engine. Removes the engine's implicit One-Unit Contract (every market's price-int × qty-int product must equal one collateral-int), which makes hosting assets of broadly different price magnitudes (BTC next to PEPE-class tokens) impossible under honest cross-margin. Deliverable is an implementation-ready design doc at experiments/single-sequencer/docs/notional-shift.md: every market→collateral crossing enumerated by symbol, mechanics per site, wire/config change and validation rules, unit-derivation guidance for stream authors, alternatives weighed, plus an adversarial review of the design itself. Implementation is a follow-up task once the design survives review.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Design doc exists at experiments/single-sequencer/docs/notional-shift.md and enumerates every market-to-collateral crossing by code symbol, with per-site mechanics
- [x] #2 Doc specifies the wire/config change, config-time validation rules, and the exactness (no-new-rounding) argument
- [x] #3 Doc contains an adversarial review covering at least: overflow headroom of shifted marks/entries, all rounding sites, avg-price bookkeeping, fee/ratio interactions, config-field unit rulings, liquidation/debt paths, funding, markets without marks, hostile shift values, and mark/fill interleaving
- [x] #4 Doc lists what the change invalidates (test vectors, recorded outputs, workload assumptions)
<!-- AC:END -->



## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Read engine/sequencer.rs, matcher.rs, num.rs, wire/records.rs in full; map every market-to-collateral crossing by symbol.
2. Write experiments/single-sequencer/docs/notional-shift.md: unit model, crossing map with per-site mechanics, storage/wire/config changes, validation, exactness argument, newtype boundary, cost accounting, stream-author guidance, alternatives, invalidation list.
3. Adversarial review section: run every attack in the brief plus self-found ones; fold surviving amendments back into the design body.
4. Add Named Terms to experiments/single-sequencer/docs/GLOSSARY.md.
5. Check ACs; implementation split into a follow-up task after design review.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Design doc written at experiments/single-sequencer/docs/notional-shift.md; Named Terms (One-Unit Contract, Notional Shift, Collateral-Scale Price, Mark Pair, Admission Price Ceiling, Position-Quantity Headroom) added to experiments/single-sequencer/docs/GLOSSARY.md. Load-bearing decisions: (1) marks stored as a raw+scaled Mark Pair, entry prices stored as Collateral-Scale Prices, resting aggregates stored scaled — the margin loops keep today's exact instruction count; (2) collateral-scale entry averaging chosen over raw-grid averaging (nothing consumes a raw entry after open; conservative variant documented); (3) cum_notional/avg_price stay in market units; close_value stays raw reporting; (4) config validation ignores invalid records state-preserving (engine convention, no config reject responses); (5) the adversarial review's biggest find: pre-shifted position notionals can overflow i64 on the price-batch health path and abort the run — answered with the derived Admission Price Ceiling, mark admission, and the Position-Quantity Headroom rule (d>0 only, preserving d=0 byte-compatibility), with the pre-existing cross-market-sum overflow documented as accepted residual risk for a follow-up hardening task.

Revision per owner direction: backwards compatibility, golden bytes, and byte parity are non-goals. Removed the length-gated decode (field read unconditionally, SCHEMA_VERSION 1→2, old recordings regenerated), removed the d=0 byte-identity regression gate, retired go-raw byte-parity carve-outs, and un-gated the mark-admission and Position-Quantity Headroom rules so they apply uniformly to every market — which also closes the pre-existing engine-abort overflow exposure for unshifted markets instead of preserving it for byte-compatibility. Adversarial review renumbered 15→14 items.

Second adversarial review round (2026-07-03): ce-doc-review headless (coherence + feasibility + adversarial personas) plus an independent codex pass over the code. Three invariant-breaking blockers found and amended into the design: the headroom exemption's flip attack, the modify-path admission bypass, and same-d reconfigs moving the derived bounds. Also amended: liquidation no-mark fallback (Mark Pair signature + one bounded Trunc), marks-before-config ignored entirely (owner ruling), config now carries declared unit exponents with a machine-checked identity (owner ruling), trailing-bytes decode check. New open-questions section (13) records the undecided items: silent config ignore vs live risk controls (owner ruled: keep open), price outgrowing the Admission Price Ceiling, store-raw vs dual storage pending a bench measurement (owner ruling), cross-market sum hardening, funding headroom. Review section items 15-21 record the attacks. Implementation handed over as TASK-035; design + review shipped on branch yamen/precision as a draft PR.

Task renumbered from TASK-030 on the design branch to TASK-034 when rebasing onto main, which had minted its own TASK-030 in the meantime.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implementation-ready design for the per-market Notional Shift (qty_unit × px_unit = 10^d × collateral_unit, exact multiply at the single market→collateral crossing) delivered at experiments/single-sequencer/docs/notional-shift.md: complete crossing map by symbol, storage/wire/config mechanics, config-time validation rules, zero-new-rounding audit, newtype boundary, hot-path cost accounting (zero margin-loop cost, one table multiply per fill side), stream-author unit derivation with the BTC+kPEPE worked example, alternatives (i128 lanes, status quo) weighed, invalidation list, and a 15-item adversarial review whose amendments are folded into the body. Verified by full reads of sequencer.rs, matcher.rs, num.rs, wire/records.rs and targeted greps (min_notional never enforced; funding/withdrawal absent; SBE root-length-gated decode confirmed for journal compatibility). Implementation is a follow-up task.
<!-- SECTION:FINAL_SUMMARY:END -->
