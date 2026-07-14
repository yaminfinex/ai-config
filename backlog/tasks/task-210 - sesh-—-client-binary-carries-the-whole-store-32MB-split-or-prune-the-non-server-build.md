---
id: TASK-210
title: sesh — client binary carries the whole store (32MB); split or prune the non-server build
status: To Do
assignee: []
created_date: '2026-07-14 11:05'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 209000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner observation (2026-07-14): sesh is quite a large binary for the
non-server side. Measured: 32MB, and `go version -m` shows the client
build embeds tsnet (wireguard-go + tailscale web-client-prebuilt) and
modernc sqlite — all store-side machinery. A shipping client never
executes any of it: it reaches the store over plain HTTP through the
host's own tailscaled, and ship/status/update/setup touch no database.

Fix space (measure before choosing): (a) build-tag or package-split so
the fleet client binary excludes store/serve/surface/tsnet/sqlite —
likely single-digit MB; (b) two release artifacts (client default,
store variant deployed by ops only — deploy-store already builds its
own binary, so the store artifact may need no public distribution at
all); (c) leave single-binary and accept the weight for distribution
simplicity. Constraints: install.sh + `sesh update` flow must stay
one-command; the update endpoint serves per-platform artifacts already;
version census/User-Agent semantics unchanged; wire v1 untouched.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Binary composition measured and recorded (what each heavy dep costs; which subcommands need it)
- [ ] #2 Client artifact no longer embeds store-only machinery, or a recorded owner decision accepts single-binary; measured size before/after
- [ ] #3 install.sh + sesh update + version census verified unchanged from the fleet's perspective
- [ ] #4 Docs current per decision-001
<!-- AC:END -->
