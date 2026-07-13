---
id: TASK-174
title: 'sesh — T-B: store-served release channel + sesh update'
status: Done
assignee: []
created_date: '2026-07-13 00:50'
updated_date: '2026-07-13 01:33'
labels:
  - sesh
dependencies:
  - TASK-173
priority: high
ordinal: 173000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Per ratified design docs/design/2026-07-12-sesh-store-served-distribution.md §3-§4, §6-§7, §11 T-B. Endpoints on the ingest listener with route-scoped any-of-verbs auth (ship|read): GET/HEAD /install.sh ({{BASE}} interpolated), /releases/latest/VERSION (only latest endpoint), /releases/<ver>/sesh-<os>-<arch>, /releases/<ver>/SHA256SUMS; immutable-path fetch discipline (VERSION once). install.sh embedded, ends by exec sesh setup --store-url $BASE with arg passthrough. sesh update [--check]: URL from installed config, equality-only version semantics, crash-safe replacement (hardlink prev, target never missing), unit restart + running-image verification, §6 failure taxonomy (pre-restart = full rollback; post-start = keep forward binary, surface R23 verbatim). just release: matrix build + remote staging + sha verify + atomic mv (refuse overwrite) + durable latest flip. Wire doc gets informational operator-surface note only; frozen wire untouched.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Fresh Linux node onboards with one curl + enable-linger, no repo/toolchain
- [x] #2 sesh update converges binary and running service; target path never missing at any crash point (injected-failure tests); failure taxonomy per design §6
- [x] #3 Failed/interrupted publish leaves previous latest usable, no partial tree at final version path; republish of existing version refused
- [x] #4 Distribution endpoints admit ship-only and read-only callers, deny no-verb callers in tsnet mode (middleware tests)
- [x] #5 latest rollback propagates as visible downgrade (from → to printed), tested; --check exit codes stable
- [x] #6 Docs current per design §10: wire-doc informational note, README install rewrite + URL-migration runbook (§9 incl. baseline inventory + retention deadline)
<!-- AC:END -->
