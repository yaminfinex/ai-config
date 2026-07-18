---
id: TASK-282
title: >-
  credential DX: verbs default to self-resolution from live correlates; explicit
  --credential-file becomes the override; raw API stays explicit
status: Done
assignee: []
created_date: '2026-07-18 12:05'
updated_date: '2026-07-18 20:46'
labels:
  - herder
  - dx
  - design
dependencies: []
priority: high
ordinal: 281500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner-ratified direction (2026-07-18, verbatim intent): "low ceremony for sane defaults, explicit at the API layer, and escape hatches." Post-cutover field experience showed every call site cargo-culting the identical incantation (--credential-file \$(herder credential path --guid \$HERDER_GUID)), which launders ambient env back into authority selection and adds per-verb friction for humans — when every call site performs the same incantation, the incantation belongs in the callee.

DESIGN TASK (type: design; design note + adversarial design review BEFORE any implementation task is cut). This amends the double-reviewed "ambient evidence may verify but never select" boundary, so the note must show the relaxation preserves the three properties the explicit flag currently buys:

1. IMPLICIT LAYERS CANNOT ACT: self-resolution is a herder VERB default only; the raw seatcred API (Stage/Authenticate/VerifySelectedBus) keeps demanding explicit presentation, so extensions/hooks/wrappers that do not call herder verbs acquire nothing. Enumerate which surfaces get the default (send, spawn attribution, adopt, cull, compact, enroll) and which never do.
2. FAIL-CLOSED SELECTION: the default resolves the caller seat from LIVE correlates (pane/process/bus row — not env claims; env guid at most a corroborating hint), refuses on conflict or ambiguity exactly like today's VerifySelectedBus, and NEVER silently falls back to ambient attribution when resolution fails — the refusal names the explicit-flag escape hatch.
3. ESCAPE HATCHES: --credential-file remains the explicit override for (a) broken-correlate recovery seats (fork-mismatch class) where live resolution refuses, (b) deliberate act-as, (c) harness/isolated-registry use. A credential path --self helper (or equivalent) may exist for scripting but must ride the same live-correlate resolution.

Also in scope: operator shell story (the human at a terminal gets the low-ceremony default too); refusal-matrix pass over the credential refusals introduced at cutover, including the pre-cutover legacy-sender refusal that names --credential-file before any sweep has run (should name the issuance sweep remedy); rollback story unchanged.

Constraints: no evidence-class widening; no env-derived path auto-open (resolution is live-evidence-based, never HERDER_GUID-derived file paths); the poisoned-env harness gains cases proving a poisoned env cannot steer the default resolution; deviations from the ratified cutover design are named as deltas in the design note, not silently absorbed.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Design note covers: default-resolution semantics per verb, refusal matrix with escape hatches, the three preserved properties argued explicitly, operator story, harness deltas
- [x] #2 Adversarial design review (cross-family) passes before any implementation task is filed
- [x] #3 Owner sign-off on the boundary amendment recorded
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Design-of-record merged to main at 4a91e7d (docs-only: docs/design/2026-07-18-credential-self-resolution.md, 944 lines, rev 9 + one merge-hygiene commit dropping an agent name from the header). 8 adversarial design review rounds (cross-family, codex reviewer), APPROVED at rev 9; owner sign-off recorded in chat 2026-07-18 ('ok signed off'). Deltas D1-D6 named per constraint (D3 carries a herdr process_info surface dependency: ns-inode + start-time, hard-refuse when absent; D5 applier-composition adopt recovery, no waiver; D6 bash attribution verification); fossil-row residual documented honestly (no safe removal verb; label recovery via fresh-pane enroll + rename --take-from --confirm-live). Poisoned-env harness deltas and refusal-matrix pass are in the design's disposition tables. Implementation filed as TASK-286 (settled-decisions, stop-and-report). Identifier sweep run and read before merge; docs-only gate-skip per stakes rule (not docs/specs/). Designer + reviewer seats culled at close.
<!-- SECTION:NOTES:END -->
