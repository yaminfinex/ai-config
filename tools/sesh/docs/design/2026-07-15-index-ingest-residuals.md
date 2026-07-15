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
equivalence oracle.

## Oversized complete lines

For a 1 MiB base64-shaped line, the previous reader allocated 8,454,192–
8,454,752 bytes per operation because its first buffer-full fragment reserved
the complete 8 MiB line limit. Fragment-sized geometric growth reduces this to
1,245,232–1,245,791 bytes per operation, a roughly 85% reduction and bounded
storage proportional to the observed line. The existing over-limit and large
trailing-partial tests remain the peak-boundary gates.

## Quarantine observation ledger

Reindex now leaves the old ledger durable while rebuilding disposable message
state and stages replacement ledger rows in memory. The final ledger delete
and replacement inserts occur in one transaction. An injected failure after
the delete and before the inserts rolls that transaction back, preserving the
prior `observed_at` value. A successful reindex continues to regenerate invalid
timestamps and retain valid ones.
