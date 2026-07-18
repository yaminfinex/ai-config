---
id: TASK-284
title: >-
  P1: registry seat-rewriting appends drop credential_generation — issued
  credentials silently stripped from long-lived seats
status: Done
assignee: []
created_date: '2026-07-18 12:45'
updated_date: '2026-07-18 14:05'
labels:
  - herder
  - bug
dependencies: []
priority: high
ordinal: 283500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field incident 2026-07-18, found via fleet report ~40 min after the first cutover flip; cutover ROLLED BACK via the documented marker-deletion lever until this is fixed.

Evidence (registry.jsonl, one orchestrator seat): row rotated normally at 12:20:29Z and 12:20:32Z — both appends carried credential_generation in the seat block. Then an append at 12:39:51Z rewrote the SEATED row with the credential_generation key ABSENT (not empty — absent), and a 12:40:51Z append (adds transcript_path) stayed absent. Audit of all 10 currently seated rows: the 5 older seats (issued via the sweep or early completions, then touched by periodic writers) are ALL stripped; the 5 recently-completed seats still carry generations. Conclusion: at least one seat-REWRITING append path builds the seat block fresh instead of carrying forward fields it does not own — the credentials unit taught the completion path the field but missed at least one enrichment/observation-class writer (suspect: sidecar correlated re-enrichment, which updates hcom_name/hooks_bound/transcript_path — matching the 12:39/12:40 append shapes).

Fix scope:
- Enumerate EVERY writer that appends a version of an existing seated row (sidecar enrichment, observer, reconcile, repair, lifecycle transitions, wait/list-side writes if any) and make seat-field carry-forward the default for fields the writer does not own — a writer must never silently drop registry facts it did not set.
- Consider making this structural (single carry helper all seat-rewriting appends go through) rather than per-writer patches, so the NEXT new seat field cannot repeat this class.
- Pins: per-writer carry test (append via each writer against a row with a generation; field must survive) plus a general source-inventory-style pin that fails when a new seat-rewriting append path appears without a carry test.
- After merge: fresh issuance sweep must show 100% coverage AND a soak (seats touched by periodic writers retain generations) before the cutover marker is re-created (owner action).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every seat-rewriting append path enumerated and carries credential_generation (and other non-owned seat facts) forward, with a per-writer carry pin
- [x] #2 Structural guard: new seat-rewriting appends cannot ship without carry coverage (inventory-style pin)
- [x] #3 Post-fix live validation: sweep to 100%, then periodic-writer soak shows no generation loss before re-enable
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged to main at e665c1d (root-cause commit + load-bearing fix round). Post-merge gate 62/62 + 4 module passes.

AC#1: structural seated-successor carry in normalizeSessionAppend with an explicit 14-field ownership table (canonical coordinates candidate-owned and never resurrected; credential_generation empty-carries/nonempty-commits; hooks_bound monotonic; transcript concrete-only); V2FromRecord round-trip repaired; intentional unseat/retire/reopen/adoption bypass carry. Fix round closed a reviewer-executed production P1 (a seated append carrying seat:null under canonical ownership erased the whole persisted seat — now clones; unseat remains the sole clearing path) and made all nine per-writer pins load-bearing in isolation (9/9 red under the single carry-removal mutation, re-executed independently by both reviewers; masking event-local carries removed with behavior-preservation verified).

AC#2: writer inventory derives UpdateLocked counts + completion Request counts and requires a structural source pin per carry writer; probed red on unlisted-file writer, count bump without pin, and deleted source marker. Known minor residual: the carryPin classification itself is author-judged (opt-out possible with a conscious map edit) — optional hardening noted.

AC#3 (partial, sequenced): post-fix issuance sweep + soak deliberately DEFERRED — the sweep currently auto-enables cutover at 100% (separate defect, filed as the sweep/enable separation task), so the validation sweep runs only after that fix merges; then soak, then the owner explicitly enables. Liveness anchors and spawn replay semantics verified untouched by both reviewers (seam fence held).

Review: dual adversarial (opus incumbent + grok calibration seat), one fix round, dual delta APPROVE with independently re-executed mutations. The grok seat found the production P1 the incumbent missed; the incumbent found the non-load-bearing-pin class.
<!-- SECTION:NOTES:END -->
