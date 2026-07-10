---
id: TASK-127
title: >-
  sesh+mish install alignment with the quick pattern (just recipes, versions
  diff, mish install unit)
status: Done
assignee: []
created_date: '2026-07-09 20:14'
updated_date: '2026-07-10 21:44'
labels: []
dependencies: []
priority: medium
ordinal: 127000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## Capture (owner request 2026-07-09, post-merge of sesh-build/mish-build)

Owner wants the install story for sesh and mish understood and aligned with the pattern proven in ~/Coding/quick: explicit separation of the LOCAL (in-project) binary from the INSTALLED binary, driven by `just` recipes. Survey completed pre-merge (see comparison below); this task is the realignment work.

## Survey findings (verified 2026-07-09, full detail in orchestrator run notes)

quick reference pattern (~/Coding/quick/justfile): three copy-based tiers — local `./quick` via `just build` (git-versioned ldflags), PATH copy via `just install` (go install → GOBIN), server copy via `just deploy-server` (scp + sudo install + systemctl restart). `just versions` diffs local build vs live server for staleness. No symlinks anywhere. mise pins go+just.

sesh (tools/sesh): service artifacts REAL and test-gated — systemd --user unit (pinned absolute ExecStart /usr/local/bin/sesh, Restart=on-failure), launchd template, installer etc/install-ship.sh with preflight (login-less SSH abort + enable-linger remedy) and drop-in preservation (no clobber without --force), all linted by tests/check-deploy-artifacts.sh incl. a repo-path-coupling grep gate. Staleness = runtime R23 refusal (stale binary vs newer cursor registry refuses cleanly, unit restart-loops). GAPS vs quick: no justfile — build is raw `go build ./cmd/sesh`, install/copy steps are README runbook commands run by hand; no `versions`-diff staleness surface.

mish (tools/mish): NO install tier by design — README states install/packaging/bin/mish deferred "until the house installer shape from sesh is copied after its rollout"; v1 installs nothing on PATH. Only run-from-source + temp-dir harness builds. README PATH recipe is dev-toolchain resolution (go/backlog/node via mise), not shipping mish.

## Scope

1. Add `just` recipe tier for sesh: build (git-versioned ldflags), install (copy, GOBIN or /usr/local/bin consistent with the unit's pinned path), deploy/upgrade recipe that wraps binary replacement + `systemctl --user restart sesh-ship` (and launchd kickstart), and a `versions`-style staleness diff (local build vs running shipper vs store, complementing the R23 runtime refusal).
2. Same recipe tier for mish + implement its deferred install unit by copying the sesh house-installer shape (per its own README plan). mish is a plain CLI — no service.
3. Decide and document the house-standard install location (quick uses GOBIN; sesh service pins /usr/local/bin) — one convention, stated in both READMEs.
4. Keep the check-deploy-artifacts.sh invariants green (no repo-path coupling; preflight; drop-in preservation).

## Acceptance Criteria
<!-- AC:BEGIN -->
1. `just build/install` (+ sesh deploy/restart recipe) exist and work for both tools; local vs installed artifacts are independent copies, never symlinks into the repo.
2. A versions/staleness surface exists for sesh (local vs running service).
3. mish v1 install lands on PATH via the house installer shape; its README deferred-install note replaced with the real recipe.
4. Both tools' own suites + house gate green; sesh deploy-artifacts gate green.

## Coordination

sesh/mish orchestrator lanes are closed; this is a hera-lane task. The sesh service is live-deployable — any change to install-ship.sh or units must preserve rollout compatibility for already-installed nodes (drop-in preservation covers config; binary path changes would need a migration note).
<!-- SECTION:DESCRIPTION:END -->

- [x] #1 just build/install (+ sesh deploy/restart recipe) exist and work for both tools; local vs installed artifacts are independent copies, never symlinks into the repo
- [x] #2 A versions/staleness surface exists for sesh (local build vs running shipper vs store), complementing the runtime stale-binary refusal
- [x] #3 mish v1 install lands on PATH via the house installer shape; its README deferred-install note replaced with the real recipe
- [x] #4 Both tools' own suites green plus the sesh deploy-artifacts gate; house install-location convention (GOBIN) stated in both READMEs
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Fixed on branch task-127-install-alignment (809c63d, single amended commit; worker codex, orchestrator-verified, cross-family reviewed). Quick-shaped just recipe tiers for both tools: build (git-versioned ldflags via new buildinfo packages, ldflags path verified against module paths end-to-end by the reviewer), install (go install -> GOBIN, independent copies, no symlinks, no sudo), sesh deploy (wraps install-ship.sh) + restart + versions. Installer defaults to GOBIN (GOPATH/bin fallback, loud failure without go); renders the resolved absolute path into the installed systemd unit at install time — committed unit keeps a portable placeholder, repo-path-coupling gate intact; preflight and drop-in preservation behavior unchanged (reviewer proved no silent drop-in shadowing on upgrade: old default installs byte-identical, custom installs refuse loudly). mish installs on PATH via the same shape, CLI-only, deferred-install README note replaced. New regression gate check-install-recipes.sh proves the running-image staleness check against a real process (/proc/pid/exe) in a hermetic workspace. Two verification cycles caught and fixed: (1) orchestrator: make-style $$ escaping made versions never detect a running service (just passes $ through) — fixed + live-process gate added; (2) reviewer: Darwin branch executed the on-disk binary masking post-install staleness — relabeled on-disk with honest comment; migration note prescribed a flow that errored in its own target scenario — now deploy-then-restart with the versions-timing caveat; garbled unit header comment fixed. Accepted observations, no change: eager backtick evaluation (quick parity, fails outside a git repo), sed replacement-string escaping class (pre-existing), GOPATH-list fallback edge, unquoted interpolation on exotic paths, gate binary unstamped (proof targets /proc resolution), mish version intercept exact-match shape. Both tool gates green x3 (worker x2 + orchestrator x2 runs incl. bash tests/run-all.sh for mish). NOTE: nothing installed or started on this machine — all verification via scratch roots; sesh services remain stopped per owner directive; first real deploy is the owner's call.
<!-- SECTION:NOTES:END -->
