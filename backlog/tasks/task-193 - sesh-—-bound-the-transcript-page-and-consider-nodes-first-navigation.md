---
id: TASK-193
title: sesh — bound the transcript page and consider nodes-first navigation
status: Done
assignee:
  - mika
created_date: '2026-07-13 19:24'
updated_date: '2026-07-14 01:25'
labels:
  - sesh
dependencies: []
priority: high
ordinal: 192000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Surface UX/perf unit. The navigation question is now DECIDED by the owner
(2026-07-14): the current recency page nests sessions under person → node
headings, which fights pagination — a page of 50 gets re-bucketed under
group headers and page cuts fall mid-group. Owner ruling: "node is a column,
not a grouping."

1. Sessions list goes FLAT: one recency-ordered table, node (and person)
   as columns, same 50-row pages. No grouping sections.
2. Nodes page becomes the entry point ('/'): the cheap per-node last-seen
   table linking to each node's sessions view (a fleet version column was
   deliberately deferred — write-path work, tracked as the fleet version
   census task). The flat recency list stays reachable (all-nodes view),
   and each node links to sessions FILTERED by that node, paginated
   exactly like the main list.
3. Transcript pages render the ENTIRE session as one response — measured
   8.7MB of HTML for a single large session on the live store. Bound it the
   same way the homepage was bounded: paginated or windowed message
   rendering (latest window + older links), raw download stays whole-file
   via the existing raw route.

Read-side only; frozen index schema through the Store seam; plan-allowlist
gate discipline and the single-flight/serve-stale projection semantics from
the bounded-recency + read/write-split design notes apply to any new or
filtered queries (a per-node filter must not reintroduce corpus scans or
per-request rebuilds).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Flat recency table (node/person as columns, no group sections); pagination boundaries no longer interact with grouping
- [x] #2 '/' serves the nodes view; per-node sessions views are filtered + paginated with the same bounds as the main list; all-nodes recency list still reachable
- [x] #3 Transcript page response is bounded for arbitrarily large sessions; raw route unchanged; large-session fixture proves the bound
- [x] #4 New/changed queries pass the plan-allowlist gate; per-node filtering adds no corpus scans and respects serve-stale projection semantics
- [x] #5 Docs current per decision-001 (README surface section records the owner navigation ruling)
<!-- AC:END -->

## Evidence (Done, 2026-07-14)

Merged to main at bad8952 (--no-ff, linear branch cd20a0f -> 90f30f1 ->
7f28b86, 34 files), pushed; deployed live as sesh-v0.1.5 (store + release
published, client update verified sesh-v0.1.3 -> sesh-v0.1.5).

Review: substance review over three cycles, 7 findings, all closed with
proven detectors (off-lock node-slice publication with a blocked-slice-build
choke gate; inspected-entries work seam bounded to the target slice with a
corpus-walk negative self-check). Final verdict READY TO MERGE, bus #66608;
merge-gate battery 58/58 green pre- and post-merge.

Live probes post-deploy (~180 ms client): '/' (nodes entry) 0.37 s = RTT
floor; /nodes 301; /sessions?page=48 0.94 s; /sessions page 1 warm
2.1-2.6 s — the known page-1 hydration residual, tracked as the projection
hydration-aggregates task (TASK-203), not a regression. Spec I1-I11 and
wire contract byte-identical; ?node= query params never journaled.
