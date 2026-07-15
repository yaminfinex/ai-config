---
id: TASK-188
title: >-
  sesh — Darwin test health: internal/update /var/folders symlink comparison
  fails; fsnotify rewalk flake
status: Done
updated_date: '2026-07-15'
assignee: []
created_date: '2026-07-13 07:49'
labels:
  - sesh
dependencies: []
priority: low
ordinal: 187000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Observed on real Darwin during the wedge investigation, pre-existing on clean main: internal/update tests fail on Darwin because /var/folders temp paths are symlinks and the test compares unresolved paths; TestPeriodicWatchRewalkRegistersNestedDirectory flakes (fsnotify timing). Neither runs in CI today — no Darwin gate exists. Fix both tests; consider what a minimal recurring Darwin check looks like (owner Mac, manual just test invocation documented, or nothing — decide and record).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Both tests pass on Darwin; path comparisons EvalSymlinks-normalized; flake root-caused or deflaked with a real sync point
<!-- AC:END -->

## Evidence (Done, 2026-07-15)

Two-round lane; final AC verified by the owner on real Darwin
(2026-07-15): `just test-darwin` fully green — full suite ok including
all previously failing packages, `go test -race ./internal/update
./internal/ship` ok, rewalk `-count=200` ok.

- ROUND 1 (builder-tore, reviewer-guni codex; merge 1c7d128): the
  update guard (production) resolves BOTH pinned and running paths at
  compare time — macOS /var/folders (symlink to /private/var/folders)
  no longer refuses the same file; pins never rewritten (historical
  Mac pins migrate at compare time); resolution errors fail closed
  with actionable both-spellings + setup-repin messages (review
  finding). Rewalk test rebuilt on the production runWithWatcher
  periodic composition with canonical WatchList membership as the sync
  point — proven by ticker-wiring sabotage going red (review finding;
  the original Darwin failure was assertion/backend coupling: kqueue
  does not synthesize the events the old test waited on). Review also
  caught the replacement test itself failing the same symlink class.
  `just test-darwin` recipe added, documented owner-on-request (no
  cron/CI — decision the task required).
- Owner-Mac run at 1c7d128 FAILED -> returned to lane per verdict.
  Triage: round-1 core fixes held (rewalk green, 11 guard failures
  gone); residual classes: darwin-gated update test fake keyed by
  unresolved path; grok/pi boundary-test checkers not symlink-safe
  (reproduced on Linux via symlinked TMPDIR — checker artifacts, NOT
  boundary holes; the pi mutant correctly discovered the outside file
  and failed only the string compare); cli status tests never
  installed the darwin-native plist (production was correct);
  environmental: owner Mac go module cache had root-owned files
  (x/oauth2 download permission denied) causing all [setup failed].
- ROUND 2 (builder-guri, reviewer-meze codex; merge 608d0a5,
  tests-only, zero production): canonical fake keying; boundary
  checkers canonicalize EXPECTED paths only with exact-set assertions
  — detector strength proven by three deliberate production-walker
  widenings going red in both spelling variants; cli tests write the
  OS-native config via the production setup.RenderPlist with
  self-diagnosing failure output; symlinked-TMPDIR variants permanent
  with spellings-differ premises; suite-wide symlinked-TMPDIR run
  added as a class detector (caught a fourth latent case: fakeProc fd
  spelling).
- Owner-Mac final run: green after `sudo chown -R $(whoami)
  ~/go/pkg/mod` fixed the environmental cache permissions.
- No version tag from this lane; the client-visible guard-message
  improvement rides the next release (hera ruling).
- Recorded recurring-Darwin-check decision: manual `just test-darwin`
  on the owner Mac on request; no cron/CI gate.
