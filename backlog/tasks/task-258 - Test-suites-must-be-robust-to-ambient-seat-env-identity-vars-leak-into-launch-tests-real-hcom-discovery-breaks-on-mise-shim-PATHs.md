---
id: TASK-258
title: >-
  Test suites must be robust to ambient seat env: identity vars leak into launch
  tests; real-hcom discovery breaks on mise-shim PATHs
status: To Do
assignee: []
created_date: '2026-07-16 03:34'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 257500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Two battery voids in one day, in two different environment classes, from the same root: parts of the test suite depend on ambient environment they neither scrub nor pin.

Class 1 — ambient identity env leaks into suite processes. A seat running the battery carries live identity/launch env (agent-kind preassignment flags, live guid/session vars). Launch-path tests read them and refuse (preassign refusals), failing tests that pass in a clean shell. Some suites already scrub transport/identity env in their fixtures (the wire-attribution tests scrub HCOM_*/HERDER_*/HERDR_*); the launch-path suites do not. Fix: every suite that exercises identity- or launch-sensitive code scrubs (or explicitly pins) ambient HCOM_*/HERDER_*/HERDR_* and agent-launch vars in test setup — audit for the class rather than patching the one observed site.

Class 2 — real-hcom binary discovery breaks under mise-shim-first PATHs. The real-hcom tests walk PATH for an hcom executable, skipping only the repo's own shim directory. When the mise shims dir precedes the real install dir (true for some agent seats and any shell that prepends shims), they pick the mise shim; with the fixture's faked HOME, mise then refuses the operator's real config as an untrusted ancestor local config (trust DB lives under the fake HOME; mise's ancestor walk finds the real config from any cwd under the operator home) and every invocation dies. Deterministic repro exists for both cwd geometries. The HERDER_TEST_HCOM_BIN override already exists and works. Fix directions: extend the discovery skip-filter to mise shim directories (or any path resolving to the mise binary), and/or resolve through the version-manager query once and pin, and document the override in the gate/battery docs so operators of shim-based shells set it.

Acceptance is by clean battery from BOTH environment classes that failed: an identity-bearing agent seat, and a shim-first orchestrator shell.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Launch/identity-sensitive suites scrub or pin ambient transport/identity env in setup (class audit, not single-site patch)
- [ ] #2 Real-hcom discovery is immune to mise-shim-first PATHs (filter or pinned resolution), with the env override documented where the battery is documented
- [ ] #3 Full battery green from an identity-bearing seat env AND from a shim-first shell without per-run workarounds
<!-- AC:END -->
