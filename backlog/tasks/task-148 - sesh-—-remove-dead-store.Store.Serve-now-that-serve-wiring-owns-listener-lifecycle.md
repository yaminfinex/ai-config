---
id: TASK-148
title: >-
  sesh — remove dead store.Store.Serve now that serve wiring owns listener
  lifecycle
status: Done
assignee: []
created_date: '2026-07-10 02:24'
updated_date: '2026-07-10 10:10'
labels:
  - sesh
dependencies: []
priority: low
ordinal: 148000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement (small cleanup). After the store command moved to a coordinated HTTP shutdown path owned by the CLI wiring, store.Store.Serve became dead production code — its only remaining caller is its own test. Dead lifecycle code invites divergence: a future caller would get shutdown semantics that bypass the coordinated listener drain and index-consumer stop.

Settled decisions:
- Remove the method and migrate or delete its test; do not keep it as a deprecated shim.
- No behavior change to the live serve path.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 store.Store.Serve and its test-only usages are removed; go build and go vet green
- [x] #2 Live serve path behavior unchanged; existing cli/store tests and check scripts green
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Fixed on branch sesh-store-serve-cleanup (c131ab0, orchestrator-verified). Pure deletion, 44 lines removed, zero added: store.Store.Serve and its only caller (its own context-cancellation test) removed, along with the test's now-unused imports. No production caller existed; the live serve path (cli serveHTTP coordinated shutdown) is untouched — no cli file changed. Orchestrator re-ran pinned gate uncached: all packages + check scripts green. Cross-family review skipped by stakes call (pure dead-code deletion, zero additions); hera merge protocol is the second pair of eyes. Merge pending hera handoff.
<!-- SECTION:NOTES:END -->
