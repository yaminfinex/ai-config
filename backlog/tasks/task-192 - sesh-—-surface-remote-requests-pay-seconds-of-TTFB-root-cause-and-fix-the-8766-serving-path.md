---
id: TASK-192
title: >-
  sesh — surface remote requests pay seconds of TTFB; root-cause and fix the
  :8766 serving path
status: Done
assignee:
  - mika
created_date: '2026-07-13 19:24'
updated_date: '2026-07-13 20:35'
labels:
  - sesh
dependencies: []
priority: high
ordinal: 191000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner report: homepage takes ~5s (measured here at 8-12s from a Sydney node). Probe evidence, all measured 2026-07-13 against the live store (sesh-v0.1.2, direct WireGuard path confirmed via tailscale ping, RTT 177ms):
- Server compute is innocent: same-instant VM-local requests render / in 5-37ms warm, ~0.5s on a projection rebuild.
- Remote TTFB dominates everything: / = 8-12s ttfb (erratic across runs), /?page=48 (23 rows) = 4.6s, /nodes = 1.6-2.2s (steady), transcript page = 12.4s ttfb then 8.7MB in 2.2s (bulk transfer is fine, ~4MB/s; release binary also 2.6MB/s).
- The decisive split: same tsnet node, same client, same moment — :8765 install.sh (3.4KB) = 0.45-0.5s consistently (~2-3 RTT, normal) vs :8766 /nodes (3.8KB) = 1.6-2.2s (~9-12 RTT). The pathology is specific to the surface listener path, scales with page weight and client RTT, and disappears for on-host clients.
- Both listeners share serveHTTP/http.Server and the same AuthHandler+WhoIs wrapping (internal/cli/root.go newTSNetServePlan), so the naive suspects are already eliminated; surface handlers buffer their render and write once (internal/surface/surface.go render()). Chunked encoding (surface, WriteHeader-then-Write) vs whatever the distribution handler emits is one remaining wiring difference worth chasing.
- Client-side tcpdump on tailscale0 would show the packet pattern but needs sudo (not available to agents; the owner can run one if asked).
Investigation plan for the builder: add cheap per-phase timing (WhoIs, handler, first-write) behind the surface path and reproduce — an instrumented binary can listen on an extra tsnet port on the live node (additive, no prod route changes) or reproduce in a tsnet-in-tests rig; find why remote :8766 responses cost RTT multiples before the first byte; fix; prove with before/after remote ttfb measurements. Transcript-page render cost is in scope only if it falls out of the same fix — its boundedness is a separate task.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Root cause identified and written down (task note or design-doc note per decision-001) with evidence, not conjecture
- [x] #2 Remote TTFB for / and /nodes within small constant multiples of RTT (target: /nodes under 1s, / under 1.5s from a ~180ms-RTT node) measured against the live store after deploy
- [x] #3 A regression gate covers the serving-path property the root cause violated, to the extent it is testable without a real tailnet (document honestly what is not)
- [x] #4 Docs current per decision-001 (README/ops notes if operational behavior or wiring changed)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC2 live evidence (2026-07-13, this ~180ms-RTT Sydney node, store sesh-v0.1.3, both fleet shippers active): / ttfb 8.5-10.5s BEFORE -> 1.41s steady AFTER (first hit 3.55s = projection rebuild under ingest, the documented residual); /nodes 1.8-2.5s BEFORE -> 0.36s AFTER (== install.sh control, pure RTT floor); /?page=48 8.5s BEFORE -> 0.38s AFTER. Root cause + fix merged as 12b2f63; design note docs/design/2026-07-13-sesh-store-read-write-split.md. Residual rebuild-under-ingest cost is the append-index corpus-cost follow-up's territory.
<!-- SECTION:NOTES:END -->
