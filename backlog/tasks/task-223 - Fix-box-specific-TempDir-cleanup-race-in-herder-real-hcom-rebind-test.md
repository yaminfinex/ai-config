---
id: TASK-223
title: Fix box-specific TempDir cleanup race in herder real-hcom rebind test
status: To Do
assignee: []
created_date: '2026-07-15 03:28'
labels: []
dependencies: []
priority: low
ordinal: 222500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TestRealHcomReapedRowRebindPreservesQueuedDelivery (tools/herder, grokbridge-area real-hcom contract test) intermittently fails the monolithic 'go test ./...' run on this machine with a TempDir RemoveAll late-file error ('directory not empty' class) — behavior under test completes, cleanup races. Hit twice in one day by two independent lanes; passes in isolation every time (0.15-0.17s); an opus reviewer could NOT reproduce it in 3x monolithic runs at two heads, so status is UNREPRODUCED-ELSEWHERE, likely load/parallelism sensitive. Repro context: full-module uncached go test under a running house battery. Fix direction: make the test drain/close whatever writes into its TempDir before returning (waitgroup or explicit shutdown on the real-hcom fixture), not a retry wrapper. Trap for the investigator: verify runs with -count=1 or the test cache will 'confirm' green without executing anything.
<!-- SECTION:DESCRIPTION:END -->
