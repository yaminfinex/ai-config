---
id: TASK-214
title: sesh — client binary carries the whole store (32MB); split or prune the non-server build
status: Done
updated_date: '2026-07-14'
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
- [x] #1 Binary composition measured and recorded (what each heavy dep costs; which subcommands need it)
- [x] #2 Client artifact no longer embeds store-only machinery, or a recorded owner decision accepts single-binary; measured size before/after
- [x] #3 install.sh + sesh update + version census verified unchanged from the fleet's perspective
- [x] #4 Docs current per decision-001
<!-- AC:END -->

## Evidence (Done, 2026-07-14)

Lane: branch task-210-client-binary (board id renumbered to TASK-214
mid-lane; id collision with the pi-amendment TASK-210), builder-movi,
sole substance reviewer reviewer-vuru (codex), hera merge gate.

- AC1: composition recorded in
  docs/design/2026-07-14-sesh-client-binary-split.md (readelf + nm -size
  per-module, NOBITS excluded, differential build as ground truth).
  Heavy deps enter only via serve/reindex/admin constructors.
- AC2: package split — cmd/sesh (slim client: fsnotify/cobra/pflag/x/sys
  only) + cmd/sesh-store (full). Sizes: linux/amd64 33,534,114 ->
  7,286,946 B; linux/arm64 31.5MB -> 6.75MB; darwin/arm64 31.7MB ->
  6.87MB; darwin/amd64 33.5MB -> 7.41MB (−78%).
- AC3: release.sh/install.sh/update path/User-Agent untouched
  (reviewer-verified byte-identical vs base). Live: fat v0.1.11 client
  self-updated to slim v0.1.12 over the real channel; installed binary
  7,286,946 B, zero heavy modules (go version -m); census flipped this
  node to sesh-v0.1.12; nodes page 200 in 0.36s; grok cursors shipping.
- AC4: README, ops/README (update-on-store-host hazard: guard primary,
  sesh.prev/deploy-store recovery), design note.
- Review findings (both FIXED, re-verified): P1 store build fails closed
  on `sesh update` pre-download (regression: client-only channel leaves
  store binary byte-identical; --check stays read-only) — fix also
  exposed and repaired check-release-publish publishing a store-flavored
  binary as the channel artifact; P2 check-client-slim.sh flipped from
  denylist to module ALLOWLIST + go list -deps internal-package deny +
  12MB ceiling, proven by three sabotages (zstd-class module, store
  import, module-free internal package).
- Verdict READY TO MERGE #74548; merge 98365c2 --no-ff; post-merge house
  battery ALL GREEN (count 58 -> 59, check-client-slim joined); pushed.
- Deploy: tag sesh-v0.1.12 exactly on merge commit; deploy-store (VM
  reports sesh-v0.1.12, now built from cmd/sesh-store); release
  published sesh-v0.1.12 slim artifacts.
- Accepted post-deploy gaps: darwin artifacts cross-compiled +
  graph-verified, not executed on real Mac (verify at next Mac touch);
  live-fleet remaining v0.1.11 node flips at its own sesh update.
