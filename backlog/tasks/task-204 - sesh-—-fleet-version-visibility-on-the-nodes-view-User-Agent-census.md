---
id: TASK-204
title: sesh — fleet version visibility on the nodes view (User-Agent census)
status: Done
assignee:
  - mika
created_date: '2026-07-14 02:00'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 203000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Deferred from the surface IA rework: the nodes view has no version column because no version data exists read-side (last_seen carries hostname/os_user/last_put_at only). Original shape from the distribution options memo (backlog/docs/doc-002 T4): the store records each shipper's version from the wire User-Agent at PUT time into node bookkeeping (write-path change, wire-compatible; premise correction from build recon: no client sent a versioned UA before this task — Go's default went out on PUTs, so the ship client gained the one-line User-Agent: sesh-ship/<version>, ruled standard header hygiene, not a wire amendment), and the nodes view renders it plus highlights nodes outside the current+previous support window (ops/README version-skew policy). Store bookkeeping only — NOT the frozen wire-visible index schema; same class as the fact_observations_session index precedent.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Store records shipper version per node from the existing User-Agent; no wire protocol change
- [x] #2 Nodes view shows the version column; out-of-window versions visibly flagged
- [x] #3 Docs current per decision-001 (ops/README version-skew + surface README)
<!-- AC:END -->

## Evidence (Done, 2026-07-14)

Merged to main at 91db1ff (--no-ff, linear bda1817 -> 4f7fb60 -> eb55f31
-> 5707549 -> 085be7f -> 4c1f80b, 18 files), pushed; deployed live as
sesh-v0.1.9 — the release that begins populating the census (0.1.8 and
older clients show "unknown" until they update; self-heals via sesh
update).

Shape: ship client sets User-Agent: sesh-ship/<version> (premise
correction: no versioned UA existed before); store records it per node on
the existing last_seen upsert (zero extra statements; charset-allowlisted,
64-byte bounded at capture, garbage -> NULL, never blocks a PUT); nodes
view + /v1/nodes render it with a support window (current + previous
patch) anchored to the store's own build version — numeric compare,
dev/ahead/unknown never wrongly flagged, never 500.

Review: 1 finding (P2: /v1/nodes was census-blind — endpoint SELECT never
loaded the column; populate arm ruled, both JSON shapes + hostile-string
encoding proven in closure). Security validation clean on first pass
(multi-KB/control/NUL/invalid-UTF-8/markup UA inputs degrade to NULL;
html/template escaping covers display + badge; no UA/version on journal
lines). Merge-gate hygiene: four task-id comment literals stripped before
merge. Final verdict READY TO MERGE; post-merge battery 58/58 green.
