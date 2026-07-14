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
scans, no per-request rebuilds, no per-request corpus walks in SQL or in
memory, no second SQL ranking path — each projection entry now carries the
session's node label (hostname, OS user: the latest fact observation across
its member wire sessions, the same winner page hydration picks), the
rebuild also derives per-node ranked slices from that list, and the global
ranking, the per-node slices, and the stamp swap atomically as one
snapshot. A filtered request pages its node's prebuilt slice — O(page)
work — at the cost of duplicating key tuples per node (~a hundred bytes per
session; single-digit MB at a 10^5-session fleet corpus). Because node
labels now feed the projection, the version stamp gained a third b-tree MAX
probe over `fact_observations` (append-only, so the INSERT-only stamp
argument holds unchanged). Hydration divergence, stated: the FILTERED view
renders the selection snapshot's node label — one snapshot for select and
display, so a response can never list a row under node A while labeling it
B mid-migration — while the unfiltered list keeps live-hydrated labels (it
has no filter invariant to hold); all other row fields hydrate live on both
views. Read-side only; everything else in this note — single-flight,
serve-stale, the staleness bound — is untouched and now also covers the
filtered view (a lagging node label can only lag the filtered view's
membership and node column until the triggered refresh lands).

### Delta: projection-carried aggregates and membership (2026-07-14)

The staleness bound above said "page hydration always reads live tables".
That liveness is what page one was paying for: hydration recomputed each
listed session's aggregates — message row counts, max activity timestamp —
and its file membership by walking the session's `sesh_index_messages`
rows per render (full-key seeks on `sesh_index_messages_logical`, but that
index covers only `(tool, logical_session_id)`, so every one of the
session's rows costs a table lookup). Page one lists the most recent =
largest sessions, so the first page paid hundreds of thousands of row
visits per render (measured post-quiesce: `/sessions` page 1 2.1–2.6 s
steady from a ~180 ms client vs 0.83–0.94 s for a deep page of small
sessions, corpus 5193; store-side page work on a fixture of fifty
2000-row sessions: ~430 ms). Node-filtered page one paid the same.

Each projection entry now carries, from the same rebuild snapshot: the
session's non-quarantined/quarantined row counts, its max parsed
non-quarantined timestamp (the row's string via SQLite's single-MAX
bare-column read — a julianday alone cannot reconstruct a nanosecond
instant), and its member file-generation keys (indexed mapping, wire-claim
fallback for unindexed generations — the same membership rule the live
path applies). The rebuild runs two corpus passes (ranking+aggregates,
then membership; ~150 ms at a 5k corpus, amortized exactly as before) and
the two passes are not one transaction: churn straddling them can leave a
ranked entry without members (skipped: honest absence) or an orphan
membership (ignored) for one projection lifetime, converged by the same
conservative-stamp re-verifying rebuild that already covers churn
straddling the stamp/ranking gap.

Page hydration now reads live tables only for genuinely per-request data,
each a full-key seek per page item: file bookkeeping times (`files` by its
4-column primary key — `last_put_at` moves on every accepted PUT and
renders as "mirrored at"), node facts, owner claims. Nothing on the
sessions-list hot path touches `sesh_index_messages` at all. The
staleness bound therefore restates as: the ranked list, its total, and
everything an entry carries — row counts, max timestamp, membership, and
the node label the per-node filter selects on — serve the rebuild snapshot
and can lag within the exact same serve-stale bound as the list itself (a
row count is at most one triggered-refresh behind for a watched page;
unwatched staleness remains unbounded until the first request's refresh
lands). Rendered node labels keep the node-label delta's split untouched:
the unfiltered list renders live-hydrated labels, the filtered view
renders its selection snapshot's label (one snapshot for select and
display). The single-session path (`Session`, i.e. the transcript route)
keeps fully live hydration — it renders that one session's rows anyway.

Gates: the max-size-sessions fixture (fifty 2000-row sessions on page
one) asserts the warm page runs a fixed handful of full-key-seek queries,
zero statements against `sesh_index_messages` outside the stamp probe and
the rebuild passes — the structural form of "no per-listed-session row
walks" — plus the serve-stale behavior of a lagging count, and proves the
detector against the deliberately regressed live-hydration shape. The 5k
plan gate covers the new files-by-generation-key query shape term-by-term.
Measured store-side warm page work on the max-size fixture: ~430 ms →
~0.6 ms; on the 5k small-session fixture: ~1.8 ms → ~0.7 ms.

## Follow-up (out of scope here)

Write-side append cost still grows with corpus/session size
(`unify`/`dedupe`/`inherit` are corpus-scale per append). The read split
removes readers from that queue, but ingest throughput itself will degrade
as the corpus grows; that is index-consumer work, not serving-path work.
