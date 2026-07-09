---
id: TASK-127
title: >-
  sesh+mish install alignment with the quick pattern (just recipes, versions
  diff, mish install unit)
status: To Do
assignee: []
created_date: '2026-07-09 20:14'
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

## Acceptance criteria

1. `just build/install` (+ sesh deploy/restart recipe) exist and work for both tools; local vs installed artifacts are independent copies, never symlinks into the repo.
2. A versions/staleness surface exists for sesh (local vs running service).
3. mish v1 install lands on PATH via the house installer shape; its README deferred-install note replaced with the real recipe.
4. Both tools' own suites + house gate green; sesh deploy-artifacts gate green.

## Coordination

sesh/mish orchestrator lanes are closed; this is a hera-lane task. The sesh service is live-deployable — any change to install-ship.sh or units must preserve rollout compatibility for already-installed nodes (drop-in preservation covers config; binary path changes would need a migration note).
<!-- SECTION:DESCRIPTION:END -->
