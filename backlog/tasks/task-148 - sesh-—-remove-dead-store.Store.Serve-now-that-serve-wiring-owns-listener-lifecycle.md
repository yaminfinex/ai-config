---
id: TASK-148
title: >-
  sesh — remove dead store.Store.Serve now that serve wiring owns listener
  lifecycle
status: In Progress
assignee: []
created_date: '2026-07-10 02:24'
updated_date: '2026-07-10 10:07'
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
- [ ] #1 store.Store.Serve and its test-only usages are removed; go build and go vet green
- [ ] #2 Live serve path behavior unchanged; existing cli/store tests and check scripts green
<!-- AC:END -->
