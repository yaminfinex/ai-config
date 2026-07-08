---
id: TASK-084
title: >-
  registry hardening: v2 writer refuses v1-shape appends at write time (D12);
  archive-collision refusal must stop suggesting archive removal
status: To Do
assignee: []
created_date: '2026-07-08 23:53'
labels: []
dependencies: []
priority: high
ordinal: 84000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
COMPANION to the write-freeze incident (see the shim-resolution task filed the same hour): a single v1-shaped row appended post-mint froze EVERY registry write fleet-wide, because the poison is only detected at the NEXT writer via migration-recovery arming. Two hardenings:

(1) REJECT POISON AT THE DOOR: spec D12 — v1 rows are legal only pre-mint. The v2 writer (UpdateLocked append path) must refuse to append a v1-shaped record to a minted registry with a typed error naming the likely cause (registry-writing binary older than the registry schema) — so one bad writer gets refused instead of freezing the file for everyone. If write-time shape validation needs spec wording, route an erratum through the spec steward lane; the behavior itself is implementation.

(2) REFUSAL TEXT IS ACTIVELY DANGEROUS: the archive-collision refusal says "restore or remove the archive before retrying" — REMOVING the archive un-arms the byte check and lets re-migration RUN against a live registry, dormant-defaulting every live session (the TASK-057 brick class). Reword: never suggest archive removal; name the safe remedy (identify and excise post-mint v1 rows, with backup; point at the incident runbook). Verified live 2026-07-08: the refusal printed exactly that suggestion during the incident.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Appending a v1-shaped record to a minted registry is refused at write time with a typed error naming the old-binary cause; the registry file is untouched and subsequent writes by healthy binaries succeed
- [ ] #2 Archive-collision refusal text no longer suggests removing the archive; it names the safe excision remedy
- [ ] #3 Suite covers both: poison append refused at the door, and the reworded refusal on archive mismatch
- [ ] #4 Spec erratum routed via the steward lane if write-time validation needs D12 wording; otherwise an explicit no-spec-change note
- [ ] #5 gate green: go vet+test both modules, all check suites
<!-- AC:END -->
