# Index ingest residual measurements

Status: implemented and measured on 2026-07-15.

## Settled logical-group append

The representative fixture is a settled logical session containing 10 files
and 10,000 indexed rows. A measured append adds one unique message to one
member. The pre-change path reported zero maintenance writes, confirming that
the existing no-op predicates had already eliminated unchanged ordinal
rewrites. It still fed all 10,001 group rows through the logical-partition
dedupe window and repeatedly enumerated the group while walking connectivity.

Ten-append samples on the pinned Linux/SQLite toolchain measured 278.8–281.0 ms
per append, including 46.8–47.0 ms in the whole-partition dedupe phase. This is
not negligible at the representative size, so measurement selected the
implementation branch rather than the acceptance-by-measurement branch.

For an append to an already indexed file, the indexer now inherits the file's
settled logical label and ordinal before insertion. It checks the touched
file's overlap edges for a cross-label connection. If none exists, dedupe is
restricted to the non-empty keys in the appended rows, using full-key seeks on
the overlap index. Linkage-changing appends retain the connected-component
relabel, ordinal, and whole-component dedupe path. If targeted dedupe removes a
row, surviving-file ordinals are compacted as before.

After the change, the same samples measured 5.79–5.99 ms per append, with
0.162–0.168 ms in targeted dedupe and zero maintenance rows. The structural
append gate rejects the whole-partition statement on this path and requires
the targeted statement; the differential checksum tests remain the state
equivalence oracle for the covered append histories.

One pre-existing equivalence hole remains outside this optimization. If every
row from a group member is removed by dedupe, its placement no longer exists
in the disposable message table. A later append to that same file can split
incrementally, while Reindex replays the removed overlap rows and rejoins it.
The optimized path does not admit this case as settled: a both-arrival-order
differential fixture proves its incremental checksum remains byte-identical to
the pre-optimization reference behavior and separately proves the known
Reindex divergence remains observable. Recovering vanished-member placement
requires its own bounded recovery design; this change does not claim to solve
it.

## Oversized complete lines

For a 1 MiB base64-shaped line, the previous reader allocated 8,454,192–
8,454,752 bytes per operation because its first buffer-full fragment reserved
the complete 8 MiB line limit. Fragment-sized 2× geometric growth removes that
fixed reserve and keeps retained capacity at no more than twice the observed
complete-line bytes, capped at 8 MiB. Pinned benchmark samples measured
2,031,664–2,032,232 bytes allocated for a 1 MiB line and 4,128,816–4,128,849
bytes for 1 MiB + 1 byte; 1 MiB + 64 KiB measured 4,128,816–4,128,838 bytes.
Allocation therefore grows at
geometric boundaries rather than jumping directly to the cap. Boundary
benchmarks cover 64 KiB through 8 MiB, including the exact just-over-1-MiB and
1-MiB-plus-64-KiB shapes. The over-limit and large trailing-partial tests plus
the retained-capacity ratio gate keep memory bounded independently of the
section length.

## Quarantine observation ledger

Reindex now leaves the old ledger durable while rebuilding disposable message
state and stages replacement ledger rows in memory. The final ledger delete
and replacement inserts occur in one transaction. An injected failure after
the delete and before the inserts rolls that transaction back, preserving the
prior `observed_at` value. A successful reindex continues to regenerate invalid
timestamps and retain valid ones.
