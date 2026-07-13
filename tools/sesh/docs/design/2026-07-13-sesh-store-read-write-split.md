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
fast dev box, a 241-byte append held the connection ~0.6 s
(`unify` ~0.37 s + `dedupe` ~0.30 s + `inherit` ~0.06 s; visible via
`SESH_DEBUG=1` per-phase timing). On the slower store VM the same holdings
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
recency projection's read-own-writes stamp property is unchanged. Bounded
read queries cannot pin the WAL against checkpointing in any way that
matters at this scale.

Under the replayed corpus with continuous appends, `/nodes` went from
0.26–0.44 s to sub-millisecond and `/` from 1.6 s to ~0.8 s locally (the
residual is the projection rebuild, which legitimately runs when the stamp
moves; it now neither blocks nor is blocked by ingest).

## Regression gate

`TestReadPathsServeWhileWriteConnectionHeld` (internal/cli) holds a write
transaction open on `Store.DB()` and asserts the surface pages and both
nodes endpoints still serve real content. Wiring a read path back onto the
write connection blocks the request and trips the gate. Not provable without
a real tailnet: the remote-RTT numbers themselves; those are verified by
before/after probes against the live store.

## Follow-up (out of scope here)

Write-side append cost still grows with corpus/session size
(`unify`/`dedupe`/`inherit` are corpus-scale per append). The read split
removes readers from that queue, but ingest throughput itself will degrade
as the corpus grows; that is index-consumer work, not serving-path work.
