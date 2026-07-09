---
id: TASK-093
title: sesh U1 — wire + index-schema freeze doc (M0)
status: Done
assignee:
  - sesh-ship-suki
created_date: '2026-07-09 05:27'
updated_date: '2026-07-09 05:51'
labels:
  - sesh
dependencies: []
priority: high
ordinal: 93000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: design (doc-only). Co-authored: store-lane worker drafts, shipper-lane worker amends (sequential pen, never simultaneous). Deliverable: docs/specs/sesh-wire.md on branch sesh-build — the frozen wire + index-schema contract (plan R24), the ONLY shipper<->store contract; after merge it binds above the plan.

Must pin: URL paths + header names (fingerprint, hostname, OS user, SESSION_OWNER); error catalog with the shipper required reaction per error (divergent replay -> conflict/generation path; offset gap -> rewind to returned high-water; out-of-grant -> hold and surface); fingerprint algorithm + window (proposal to beat: SHA-256 over bytes [0,1024) recorded once size >= 1KiB, UUID-only identity below); recovery GET shape (UUID-only lookup allowed pre-fingerprint; response carries stored fingerprint + per-generation high-waters); index row schema (message uuid, logical session id, file uuid + generation, role, timestamp, ordinal, byte span, quarantine flag) that U6 writes and U7 reads; numeric defaults (rescan 60s, max PUT body 4MiB) — plan defaults stand unless this doc beats them.

Read first: /home/grace/Coding/ai-config/napkins/sesh-build/playbook.md, plan U1 section + Key Technical Decisions (git show 05dfc47:docs/plans/2026-07-09-001-feat-sesh-session-service-plan.md), spec section 8 (git show e58f50a:docs/specs/session-service-spec.md). Thread: sesh-u1. Orchestrator routes design-authority (tomo) sign-off before merge.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Doc pins every item in the plan U1 Approach list; no TBDs remain
- [x] #2 Both co-authors state explicit sign-off on thread sesh-u1
- [x] #3 Design-authority sign-off recorded; doc merged to sesh-build
- [ ] #4 U3/U4/U6 later cite the doc without amendment; internal/wire types transcribe it 1:1
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged to sesh-build at 557b3d8 (branch sesh-wire, ff). Provenance: soho draft 3bfd9fe -> suki amendments ea5423a (8 items incl. confirm-then-open conflict handshake, adjudicated ACCEPTED) -> option-2 unification amendments d7cb025 + 557b3d8 after buro empirical finding (claude v2.1.195 resume rewrites content ids per-file; uuid-overlap is the primary unifier, >=2 non-empty pairs, canonical id = earliest file by first-ingest of gen 0). Sign-offs: suki #25024+#25285, soho #25035+re-ack, design authority manual-zolu final confirm #25301 on 557b3d8 exactly. AC #4 (U3/U4/U6 cite without amendment) trails — verified at those units' verdicts. Full trail: hcom thread sesh-u1.
<!-- SECTION:NOTES:END -->
