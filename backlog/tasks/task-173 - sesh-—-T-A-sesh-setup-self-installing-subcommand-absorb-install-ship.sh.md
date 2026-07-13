---
id: TASK-173
title: 'sesh — T-A: sesh setup self-installing subcommand (absorb install-ship.sh)'
status: Done
assignee: []
created_date: '2026-07-13 00:50'
updated_date: '2026-07-13 01:14'
labels:
  - sesh
dependencies:
  - TASK-172
priority: high
ordinal: 172000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Per ratified design docs/design/2026-07-12-sesh-store-served-distribution.md §5 + §11 T-A and doc-002 T1. Embedded unit/plist templates, os.Executable()+EvalSymlinks path pinning, store-URL drop-in with DP-4b provenance digest (sha256 trailing comment; digest-intact → replace on explicit new URL; edited/absent digest → refuse without --force), user-bus preflight before any write, linger warning, --dry-run. just deploy delegates to it. Per-OS config parsing per §5 (Linux env drop-in preserves unknown keys + quoting; macOS plist URL rewrite leaves ProgramArguments untouched). Docs rows per §10.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 sesh setup --store-url URL [--force] [--dry-run] reproduces install-ship.sh behavior on Linux and macOS incl. preflight-before-write, linger warning, refuse-to-clobber
- [x] #2 DP-4b unit-tested both ways: operator-edited drop-in (incl. URL-only edit) refuses without --force; digest-intact drop-in replaced when new URL explicit
- [x] #3 Linux drop-in rewrite preserves unknown env keys (e.g. SESH_STATE_DIR) and quoting; macOS plist rewrite preserves ProgramArguments; round-trip tests
- [x] #4 just deploy calls sesh setup; install-ship.sh reduced to deprecation pointer after dry-run parity check
- [x] #5 Docs current per design §10: README install/runbook sections, justfile comments; decision-001 honored
<!-- AC:END -->
