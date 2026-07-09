---
id: TASK-093
title: sesh U1 — wire + index-schema freeze doc (M0)
status: To Do
assignee: []
created_date: '2026-07-09 05:27'
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
- [ ] #1 Doc pins every item in the plan U1 Approach list; no TBDs remain
- [ ] #2 Both co-authors state explicit sign-off on thread sesh-u1
- [ ] #3 Design-authority sign-off recorded; doc merged to sesh-build
- [ ] #4 U3/U4/U6 later cite the doc without amendment; internal/wire types transcribe it 1:1
<!-- AC:END -->
