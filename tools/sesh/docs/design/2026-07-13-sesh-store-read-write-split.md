# sesh store: read/write connection split (surface TTFB root cause)

Status: implemented. Companion to
`2026-07-12-sesh-store-served-distribution.md` (serving topology unchanged;
this note changes only how read-serving paths reach SQLite).

## Symptom

Remote loads of the team surface paid seconds of time-to-first-byte while
VM-local probes of the same pages rendered in milliseconds and same-size
distribution responses (`/install.sh`, `/releases/...`) from the same tsnet
node returned in 2–3 RTTs. Measured 2026-07-13 against the live store from a
~180 ms-RTT client: `/nodes` (3.8 KB) 1.6–2.5 s steady, `/` (20 KB) 8–12 s
erratic, while a 20 KB Range read of a release binary took 0.7 s and the
50 KB `/assets/htmx.min.js` — same listener, same auth — took 0.9 s.

## Root cause (measured, not conjectured)

The store ran every SQLite access — ingest PUT bookkeeping, the append-index
consumer, and all surface/read queries — through one `database/sql` pool
capped at a single connection (`SetMaxOpenConns(1)`).

Each append event is indexed inside one write transaction whose cost is
corpus-scale, not append-scale: at a 1.3 GB / ~500 k-row corpus replayed on a
fast dev box, a 241-byte append held the connection ~0.5–0.6 s, dominated by
logical-session maintenance (exclusive phases: `dedupe` ~0.30 s,
connected-group `unify` ~0.07 s, `inherit` ~0.06 s, plus parse/insert/commit;
visible via `SESH_DEBUG=1` per-phase timing, whose laps are mutually
exclusive and additive). On the slower store VM the same holdings
run ~2 s. With even two active shippers the write side saturates the single
connection, so every read queues behind full append transactions — and a
surface page issues several sequential queries, paying that queue repeatedly.
That is why TTFB grew with page weight (more queries per page), why a 19-byte
surface 404 (one lookup) cost ~2 s while a 50 KB embedded asset (no DB) was
fast, and why on-host probes at quiet moments saw milliseconds. Client RTT
was never the driver; the listener split in early probes was a red herring —
the fast endpoints simply never touched the database.

Discriminating live evidence: `GET /v1/nodes` on the ingest listener (same
query, same connection, different listener) was exactly as slow (2.1–3.4 s)
as `GET /nodes` on the surface listener.

## Fix

The database is WAL-journaled, and WAL readers run concurrently with the
single writer — the serialization was purely an artifact of sharing one
pooled connection. The store now owns two handles:

- `Store.DB()` — the single write connection. Ingest, the index consumer,
  and admin mutations keep SQLite's single-writer discipline unchanged.
- `Store.ReadDB()` — a small read-only pool (`mode=ro`, 4 conns, same busy
  timeout), opened after schema init. The surface seam
  (`surface.NewSQLStore`) and node status (`/nodes`, `/v1/nodes`) read
  through it and never queue behind append transactions.

Read snapshots are per-query latest-committed WAL state, which the surface
already tolerated (its queries were interleaved with writers before); the
recency projection's read-own-writes stamp property is unchanged. (Delta,
same day, after the first fleet bulk sync: that read-own-writes property is
now superseded — see "Delta: projection single-flight + serve-stale" below.)
Bounded read queries cannot pin the WAL against checkpointing in any way
that matters at this scale.

Under the replayed corpus with continuous appends, `/nodes` went from
0.26–0.44 s to sub-millisecond and `/` from 1.6 s to ~0.8 s locally (the
residual is the projection rebuild, which legitimately runs when the stamp
moves; it now neither blocks nor is blocked by ingest).

## Regression gate

`TestReadPathsServeWhileWriteConnectionHeld` (internal/cli) holds a write
transaction open on `Store.DB()` and asserts the surface pages and both
nodes endpoints still serve real content. Wiring a read path back onto the
write connection blocks the request and trips the gate. Not provable without
a real tailnet: the remote-RTT numbers themselves. The live BEFORE figures
(2026-07-13, ~180 ms-RTT client) are in the Symptom section; the AFTER
figures, measured from the same client against the live store immediately
after deploy (2026-07-13, both fleet shippers active): `/` ttfb 1.41 s
steady (first hit 3.55 s — projection rebuild under ingest, the documented
residual), `/nodes` 0.36 s (equal to the `install.sh` no-DB control, i.e.
the RTT floor), `/?page=48` 0.38 s. BEFORE → AFTER: `/` 8.5–10.5 s → 1.41 s;
`/nodes` 1.8–2.5 s → 0.36 s.

## Delta: projection single-flight + serve-stale (2026-07-13, post-deploy)

The "documented residual" above — the projection rebuild when the stamp
moves — turned out not to be residual under bulk ingest: with a node
shipping a multi-thousand-file corpus, the stamp moves between every
request, the bounded-recency design's read-your-own-writes choice (rebuild
inline whenever the stamp moved, no rebuild floor) degenerated to a
corpus-scale rebuild per page load (11–25 s per projection-backed request
from a ~180 ms client while `/nodes` held the RTT floor), and nothing
single-flighted concurrent rebuilds.

The projection is now single-flight + serve-stale-while-revalidating: at
most one rebuild in flight, requests observing a moved stamp serve the
previous projection immediately and trigger a background refresh, and only
the cold start blocks (shared by all concurrent cold requests). This
explicitly supersedes the read-your-own-writes property stated in the Fix
section and in the original bounded-recency projection comment
(`internal/surface/sqlstore.go`). The new bound, stated precisely: only the
ranked list and its total can lag (page hydration always reads live
tables), and every request that sees a moved stamp triggers a refresh.
Under continuous ingest a watched page (60 s poll) serves a list at most
one poll interval plus two rebuild durations behind the store — the poll
that observes a completed rebuild serves that rebuild's start-of-rebuild
snapshot and triggers the next one. Once ingest quiesces the list converges
after any in-flight rebuild plus at most one more: the rebuild reads its
stamp before its ranking query, so writes straddling that gap appear in the
published list but leave the stamp conservative and force one re-verifying
rebuild — never silent absorption. Unwatched staleness is NOT bounded: the
first request after an idle period serves the previous visit's projection,
however old, then converges as above. Deliberate trade — a page load never
blocks on a corpus-scale rebuild, which is exactly the onboarding-click
moment this regression fired on. The refresh goroutine is owned: it runs on
a store-lifetime context and `SQLStore.Close` (wired before the DB pool
closes in serve shutdown) cancels and drains it. Rebuild duration is
journaled at debug level under the same identifier-free contract as the
per-request timing. Gates: the single-flight/serve-stale tests beside the
large-corpus plan gate (`internal/surface`) — including the
churn-straddling-the-stamp interleaving, rebuild-failure latch clearing,
and canceled-cold-waiter edges; the live surface check now waits for
convergence instead of asserting read-your-own-writes.

### Delta to the delta: node label in the projection (2026-07-14)

The surface IA rework (nodes-first navigation, flat sessions table per the
owner ruling "node is a column, not a grouping") added a per-node filtered
sessions view. To keep that filter inside this design's bounds — no corpus
scans, no per-request rebuilds, no second SQL ranking path — each projection
entry now carries the session's node label (hostname, OS user: the latest
fact observation across its member wire sessions, the same winner page
hydration picks), and the node-filtered page slices the same in-memory
list. Because node labels now feed the projection, the version stamp gained
a third b-tree MAX probe over `fact_observations` (append-only, so the
INSERT-only stamp argument holds unchanged). Read-side only; everything
else in this note — single-flight, serve-stale, the staleness bound — is
untouched and now also covers the filtered view (a lagging node label can
only lag the filtered LIST membership; the rendered rows hydrate live).

## Follow-up (out of scope here)

Write-side append cost still grows with corpus/session size
(`unify`/`dedupe`/`inherit` are corpus-scale per append). The read split
removes readers from that queue, but ingest throughput itself will degrade
as the corpus grows; that is index-consumer work, not serving-path work.
