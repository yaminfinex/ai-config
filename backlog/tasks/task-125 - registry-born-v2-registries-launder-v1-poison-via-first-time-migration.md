---
id: TASK-125
title: 'registry: born-v2 registries launder v1 poison via first-time migration'
status: Done
assignee: []
created_date: '2026-07-09 12:54'
updated_date: '2026-07-10 02:13'
labels: []
dependencies: []
priority: high
ordinal: 125000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## Capture (from reviewer-tofu adversarial review of TASK-084, msg #31487, 2026-07-09)

Empirically demonstrated: on a minted v2 registry with NO 0001-v1-migration archive (any machine that never had a v1 registry), an old binary appending one raw v1 row causes the next UpdateLocked to see migrationNeeded=true, ensureMigrationArchive finds no archive, archives the POISONED live bytes as 0001-v1-migration.jsonl, and the full reseed adopts the poison as a legitimate migrated_v1 row. No refusal fires; the poison becomes the trusted archive. TASK-084's poison-at-door gate only holds where a real v1-migration archive exists to byte-collide.

## Direction (reviewer's)

Migration should distinguish "registry was born v2 / already fully migrated" (e.g. v2 node rows present + no prior migration marker) and refuse to run first-time v1 migration on it — typed refusal naming the old-binary cause, consistent with the TASK-084 error surface.

## Acceptance Criteria
<!-- AC:BEGIN -->
1. Repro from the capture encoded as a failing test first (born-v2 registry + injected raw v1 row → next UpdateLocked must refuse, not launder).
2. Legit first-time migration of a genuine v1 registry still works (existing migration suite green).
3. Refusal message names cause + remedy (excision), consistent with TASK-084 wording rules.
4. Full house gate green.
<!-- SECTION:DESCRIPTION:END -->

- [x] #1 Repro encoded as failing test first: born-v2 registry + injected raw v1 row causes next UpdateLocked to refuse, not launder
- [x] #2 Legit first-time migration of a genuine v1 registry still works (existing migration suite green)
- [x] #3 Refusal message names cause + remedy (excision), consistent with existing refusal wording rules
- [x] #4 Full house gate green
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Dispatched 2026-07-10 to gpt-5.6-sol high-reasoning worker (@worker-nuvo, branch task-125-born-v2-poison), brief napkins/run-herder-dx/task-125-brief.md; failing-test-first ordering required.

Done 2026-07-10, merged --no-ff to main. Red-before-green proven: repro test committed first (be8419b, fails pre-fix with err=nil + poison adopted), fix second (f6fc2f2, typed LegacyV1AppendError before callback/archive mutation). Adversarial review (opus): APPROVE — laundering reproduced end-to-end on reverted code (archive minted from poisoned bytes, poison reseeded as trusted migrated_v1), refusal path asserted precisely (typed error, callback not run, no archive, live file byte-unchanged); boundary edges hold (genuine v1 migrates, matching-archive recovery allowed, non-matching fails closed downstream, corrupted-v2 not misclassified, node-gate precedence kept). Mid-batch non-atomic refusal explicitly untouched — remains the separate batch-atomicity task. Informational hardening spun off: archive-content validation (reject archives containing legacyV1 rows).
<!-- SECTION:NOTES:END -->
