---
id: TASK-147
title: >-
  registry: validate archive CONTENT, not just existence, before trusting prior
  migration
status: To Do
assignee: []
created_date: '2026-07-10 02:13'
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
- [ ] #1 Archive content validated on the trust path; planted-poison-archive repro refused with typed error
- [ ] #2 Genuine migration archives still verify (existing suite green)
- [ ] #3 Full house gate green
<!-- AC:END -->
