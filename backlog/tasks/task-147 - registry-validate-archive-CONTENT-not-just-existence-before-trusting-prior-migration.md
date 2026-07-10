---
id: TASK-147
title: >-
  registry: validate archive CONTENT, not just existence, before trusting prior
  migration
status: Done
assignee: []
created_date: '2026-07-10 02:13'
updated_date: '2026-07-10 10:32'
labels: []
dependencies: []
priority: low
ordinal: 147000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Defense-in-depth from the born-v2 poison-fix adversarial review (informational, non-blocking there). The migration path trusts archive EXISTENCE as proof of a legitimate prior v1 migration. Demonstrated: if an archive whose bytes equal a poisoned live file is planted in .archive/, the poison still launders (reseeded as trusted migrated_v1). Unreachable under the current threat model (the born-v2 refusal prevents the system itself from ever minting such an archive; a genuine v1 crash-window only leaves legit v1 bytes), so this requires an external actor who already owns the state dir — but cheap to harden: reject an archive that itself contains rows the classifier marks legacyV1, with a typed error consistent with the existing refusal surface.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Archive content validated on the trust path; planted-poison-archive repro refused with typed error
- [x] #2 Genuine migration archives still verify (existing suite green)
- [x] #3 Full house gate green
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Riding the batch-atomicity unit (@worker-gole) — same package, one worker/review; brief napkins/run-herder-dx/task-126-147-brief.md.

Done 2026-07-10 with TASK-126 (one unit, merged). Archive content loaded on the trust path; planted v1-row archives refused with typed archive-specific cause/remedy; genuine migration + recovery archives green; hot path untouched (archive read only in the anomalous minted+v1-row state). Refactor commit proven semantically equivalent by the reviewer. Latent coupling noted for the record: empty/corrupt planted archives are caught by the byte-equality backstop, not the refuse gate itself — if that backstop ever changes, the gate needs Quarantined() awareness.
<!-- SECTION:NOTES:END -->
