---
id: doc-001
title: sesh shipper and ingest efficiency findings
type: other
created_date: '2026-07-10 01:38'
---


## Executive verdict

The current full-pass shape is not the main mistake. After admission was bounded, the
remaining avoidable costs are persistence and enrichment work performed too often:

1. **Adopt pass-batched cursor persistence.** Eight ACKs currently rewrite, fsync, and
   rename the entire 750-cursor JSON registry eight times. This is the largest measured
   shipper term and can be reduced without changing `RunOnce`, the wire, or at-least-once
   correctness.
2. **Adopt a short TTL for `/proc` correlation.** Correlation is 70% of a quiescent pass
   on the measured tree. A cache can retain full authoritative file passes while bounding
   enrichment work.
3. **Adopt adaptive hint admission.** Admit the first hint immediately after an idle
   period, then retain the two-second start-to-start ceiling while writes remain
   continuous. This recovers interactive freshness without returning to runaway passes.
4. **Adopt transactional index batches.** Store indexing is asynchronous to the durable
   ACK, but SQLite statement and commit churn is the dominant store CPU term under batched
   transcript appends.
5. **Reject dirty-set hint passes and cached walk/stat state.** Their savings are smaller
   than the items above, while they split live tailing from the authoritative path and
   create invalidation machinery around missed events, moves, truncations, and overflow.
6. **Adopt graceful `serve` shutdown as a small reliability fix.** The profiling run found
   that `serve` dies on the default `SIGTERM` action instead of unwinding `main`; deferred
   cleanup does not run.

No spec erratum is recommended. All adopted items preserve a full authoritative
`RunOnce` whenever a pass is admitted. Dirty-set passes would require an explicit change
to the authoritative-pass/I3 interpretation and are rejected here.

## Method

All workloads used temporary home/state/store roots and ephemeral loopback ports. Nothing
used the live sesh state directory or ports 8765/8766. Scratch test code, instrumented
binaries, profiles, and syscall logs lived under `/tmp`; no behavior-changing code was
written to the branch.

The representative tree contained 750 Claude files in 117 project directories. Short
tests constructed already-ACKed cursors in memory so setup was outside measured pass
timings. The active short pass appended to eight files. The sustained run used eight
appenders at 20 Hz for 60.005 seconds after backfill, then tore down every process and
temporary root.

CPU attribution sources:

- a valid 66-second Go CPU profile for the shipper during the sustained workload;
- short Go CPU profiles for 320-row store/index bursts;
- direct component timings over repeated short passes;
- syscall counts from a separate, identically shaped 10-second `strace -f -c` run.

The store's sustained CPU profile file was empty because `serve` takes default `SIGTERM`
instead of unwinding `main`, so the deferred pprof flush never ran. Store total CPU comes
from sustained jiffies; internal store attribution comes from the valid short-burst CPU
profile. `strace` materially perturbs scheduling and admission, so its counts establish
operation mix and amplification, not production rates or latency.

## Cost accounting

### One quiescent authoritative pass

Repeated direct timings (10 iterations, 750 files) were:

| Component | Mean | Share of 20.92 ms full pass | What it does |
|---|---:|---:|---|
| Root discovery | 1.33 ms | 6% | Walk 117 directories and match 750 names |
| `/proc` scan | 11.52 ms | 55% | Read same-UID status, comm, and cwd |
| Full correlation | 14.58 ms | 70% | Scan plus per-file cohort joins |
| Cursor/file checks excluding correlation | 4.67 ms | 22% | 750 stats and fingerprint probes, plus maps/GC accounting |
| Full `RunOnce` | 20.92 ms | 100% | Discovery, correlation, cursor checks; no bytes dirty |

`RunOnce` without correlation measured 6.01 ms, so root walk plus all 750 cursor checks
are already inexpensive. A single-pass syscall segmentation confirmed the shape:

- discovery: 119 `openat`, 238 `getdents64`, 119 `close`;
- `/proc` correlation: 728 `openat`, 2,327 `read`, 726 `fstat`, 95 `readlinkat`;
- cursor checks: 750 `newfstatat`, 750 `openat`, 1,500 `read`.

The high `fcntl`/`epoll_ctl` counts are Go's file-poller bookkeeping around those opens,
not additional domain operations. Mirror reads and HTTP are absent on a quiescent pass.

### One active authoritative pass

With eight one-line appends and a real in-process HTTP store:

| Component | Repeated result | Notes |
|---|---:|---|
| Full active `RunOnce` | 58.7–61.9 ms | 750 files, eight dirty |
| Eight synchronous HTTP/store ACKs | 8.0–8.6 ms total | About 1.04 ms per PUT; includes mirror durability and store metadata |
| Eight asynchronous index events | 7.5–10.0 ms total | About 1 ms per one-line event |
| Eight cursor-registry saves | 23.1–26.3 ms total | Whole 750-cursor JSON file each time |
| Eight updates with one estimated save | usually 4.2–5.3 ms total | One known 82.9 ms outlier during unrelated owner load |

The active pass is accounted for by the 20.9 ms authoritative baseline, roughly 8.4 ms
of HTTP/store ACK work, roughly 24–25 ms of registry persistence, and small range-read and
scheduler overhead. `Client.PutBytes` was only 1.24% cumulative CPU in the sustained
shipper profile; network framing is not a useful target.

The sustained shipper profile made registry amplification load-bearing:

- `Registry.save`: 1.33 CPU-seconds, 32.92% of all shipper samples;
- `json.MarshalIndent`: 1.18 CPU-seconds, 29.21%;
- `procCorrelator.CorrelateAll`: 1.01 CPU-seconds, 25.00%;
- `Discover`: 0.16 CPU-seconds, 3.96%;
- `Fingerprint`: 0.14 CPU-seconds, 3.47%;
- `Client.PutBytes`: 0.05 CPU-seconds, 1.24%.

The separate traced workload observed 69 registry renames and exactly 138 shipper
`fsync` calls: each whole-registry save performs one file fsync, one atomic rename, and
one directory fsync. Tracing stretched passes enough to perturb the two-second cadence,
so those are amplification ratios rather than steady rates.

### Sustained shipper and store totals

Over the 60.005-second profiled workload:

| Process | CPU time | One-core percentage |
|---|---:|---:|
| Shipper | 3.670 s / 367 jiffies | 6.12% |
| Store + index consumer | 1.690 s / 169 jiffies | 2.82% |

The shipper number was captured with Go CPU profiling active and with a larger live
`/proc` population than the earlier unprofiled 3.28% acceptance run, so it is an
attribution run, not a replacement before/after benchmark. Its profile nevertheless
accounts for essentially all useful CPU and identifies the same registry and `/proc`
terms as the isolated timings.

For a realistic two-second batch (40 lines in each of eight files), asynchronous indexing
measured about 61–68 ms in the first quiet short runs. A CPU profile attributed 0.52 s
cumulatively to `Indexer.processAppend`, 0.36 s to `insertRows`, 0.43 s to SQLite exec,
0.19 s to SQLite commit, and 0.17 s each to WAL frame work and statement preparation.
The current row loop performs a dedup query and an insert per message, with each statement
otherwise eligible to auto-commit. A deliberately rough scratch transaction around eight
events reduced the median from roughly 99 ms under a noisier matched sequence to 16.6 ms;
earlier quiet non-transaction runs were 61–68 ms. This is not production-safe code, but
it supports a conservative **4x expected reduction** from proper transactions and prepared
statements. The traced store made 375 `fsync`, 733 `pwrite64`, and 737 `pread64` calls in
10 seconds, consistent with commit/WAL churn being the dominant operation mix.

Parse and mirror-read CPU were small (`parseComplete` was 1.88% cumulative in the burst
profile). The raw mirror durability rule itself must remain unchanged.

## Candidate verdicts

| Candidate | Verdict | Expected payoff | Complexity and correctness |
|---|---|---|---|
| Dirty-set hint passes | **Reject** | Avoid at most the ~21 ms authoritative baseline; an eight-file pass would still pay HTTP and registry persistence. Rough steady saving before other fixes: about 1 point of a core. | High. Creates a second, non-authoritative live path and invalidation rules for missed events, deletion GC, moves, recreation, overflow, and correlation. Requires proposed spec errata to weaken the every-pass authoritative model. |
| TTL-cached `/proc` correlation | **Adopt** | A 10 s TTL at a 2 s pass cadence eliminates about 80% of the 14.6 ms correlation term: roughly 0.6–1.2 points of a core depending on process population. | Low–medium. Correlation is enrichment; cached positive observations remain historical facts and I8 forbids retraction. New identities must force refresh so onboarding attribution is not delayed. |
| Adaptive admission after idle | **Adopt** | Restores isolated append-to-pass latency from up to ~2 s to one pass duration (roughly 25–80 ms on measured shapes), with no increase in the sustained pass ceiling. | Medium. Needs a clock-driven state machine and tests for idle burst, continuous writes, pending hints, periodic tick, cancellation, and backoff. Pass content remains full `RunOnce`. |
| Persisted/cached walk or stat state | **Reject** | Discovery is only 1.33 ms and all cursor checks only 4.67 ms: less than 0.3 points of a core at two-second cadence. | High relative to benefit. Correct invalidation recreates the dirty-set problem and risks missing same-size rewrites, moves, truncations, or overflow recovery. |
| Pass-batched cursor persistence | **Adopt; highest priority** | Eight saves become one: observed persistence wall time falls from ~23–26 ms to usually ~4–5 ms; sustained profile says up to 33% of current shipper CPU is here. Benefit grows with active-file count. | Medium. Advance in memory only after durable store ACK, flush once before `RunOnce` returns, and surface flush failure. A crash before flush merely replays already-ACKed bytes, which I4 requires the store to absorb idempotently. |
| Transactional/batched index writes | **Adopt** | Conservative 4x reduction in the dominant store-index term; scratch estimates ranged from 61–99 ms to ~16.6 ms for 320 rows. | Medium. Preserve event order, dirty-for-reindex recovery, deterministic dedup, and store write serialization. Durable ACK remains independent because indexing stays asynchronous and re-derivable. |
| Graceful `serve` shutdown | **Adopt; small** | Reliability/operability rather than steady CPU. Ensures deferred cleanup and profile/telemetry flushes run. | Low. Use signal context consistently with `ship`; prove listeners, index consumer, store DB, and deferred hooks exit. |

## Spec compatibility

The recommended work does not weaken I3:

- discovery still selects files, never processes;
- cold backfill and live tailing still call the same `shipFile` state machine;
- every admitted pass is still the complete authoritative `RunOnce`;
- the 60-second periodic full rescan remains the overflow and missed-event guarantee;
- cursor movement still follows a durable store ACK and replay remains at-least-once.

Dirty-set hint passes would change “every admitted pass is authoritative” into “hint passes
are best-effort deltas; periodic passes are authoritative.” If that architecture is ever
reconsidered, it must first be proposed as spec errata with explicit new guarantees for
event loss, deletion GC, moves, recreation, correlation, and backfill parity. This memo
does not recommend that erratum because cheaper compatible changes address the measured
dominant costs.

## Filed-ready build tasks

### Batch cursor-registry durability at the authoritative-pass boundary

**Problem**

Each cursor mutation serializes the full registry, fsyncs the temporary file, renames it,
and fsyncs the directory. Several active files therefore multiply whole-registry work
inside one authoritative pass even though a crash before local persistence is already
safe at-least-once replay.

**Acceptance criteria**

- A pass containing multiple successful ACKs performs at most one durable registry
  replacement, including backfill that needs multiple PUT chunks.
- A cursor changes in memory only after the corresponding durable store ACK or required
  catalog transition; an unreachable/refusing store never advances it.
- Before `RunOnce` returns, all mutations from that pass are durably persisted or the
  pass returns a surfaced persistence error.
- Killing the shipper after store ACK but before the batch flush replays safely after
  restart and converges without duplicate mirror bytes or lost source bytes.
- Deletion GC, path moves, owner observations, truncation, fingerprint transitions,
  poison state, recovery, and partial-pass errors persist correctly in the same batch.
- A 750-cursor/eight-dirty benchmark shows one rename and two fsyncs per pass, and at
  least a 70% reduction in registry-persistence wall time versus per-cursor saves.
- Existing unit and scenario gates remain green uncached.

**Settled decisions**

- Keep the JSON registry and atomic temp-file + fsync + rename + directory-fsync format;
  this task changes commit frequency, not storage technology.
- The batch boundary is one `RunOnce`, not a timer and not a number of bytes.
- Store ACK remains the only event that advances an offset. Local batch persistence may
  lag within the running pass because a crash produces safe idempotent replay.
- Flush successful mutations even when another file in the same pass holds or fails.
- Do not weaken recovery refusal, schema-generation checks, lifetime locking, or surfaced
  durability errors.

### Cache process correlation without caching file authority

**Problem**

Every two-second authoritative pass repeats a full `/proc` scan even though owner
correlation is best-effort enrichment and positive observations are never retracted.

**Acceptance criteria**

- Repeated passes over an unchanged discovered identity set perform at most one full
  correlation sweep per 10-second TTL.
- A newly discovered identity without a persisted owner forces an immediate correlation
  attempt rather than waiting for TTL expiry.
- Cached positive observations may be recorded, but absence never erases a persisted
  owner and cross-user `/proc` reads remain forbidden.
- Expiry, PID churn, unreadable proc entries, and process death preserve honest absence
  and never stop byte shipping.
- Linux correlation tests cover cache hit, expiry, identity-set change, owner persistence,
  PID reuse/churn, and same-cwd ambiguity; Darwin remains facts-only.
- A representative 750-file benchmark reduces correlation CPU by at least 70% across
  five two-second passes, with every pass still executing full discovery and cursor work.

**Settled decisions**

- Cache correlation results/process observations only; never cache `Discover`, file size,
  fingerprint, cursor authority, or shipping decisions.
- Default TTL is 10 seconds and is internal, not a CLI/config surface.
- Identity-set growth invalidates enough cache state to attempt the new identity promptly.
- I8 remains unchanged: observations are remembered and never retracted.

### Admit the first filesystem hint immediately after idle

**Problem**

The fixed two-second minimum prevents runaway passes under continuous writes but makes an
isolated save wait behind the same cooldown even when the shipper has been idle.

**Acceptance criteria**

- The first filesystem hint after at least one admission interval without hint-driven
  work starts a full authoritative pass immediately, subject only to scheduling.
- Under continuous writes, authoritative pass starts remain no closer than the configured
  two-second interval.
- A burst during the cooldown creates at most one pending pass; it does not build an
  unbounded queue or trigger a catch-up storm.
- Periodic rescans, store backoff, cancellation, watcher overflow handling, and directory
  registration retain their current guarantees.
- Deterministic clock-based tests cover idle first hint, save burst, continuous appends,
  periodic tick racing a pending hint, backoff, and shutdown.
- Isolated append-to-ACK latency is below 250 ms on the representative tree while the
  sustained CPU result stays within 10% of fixed-interval admission.

**Settled decisions**

- Every admitted pass remains the complete `RunOnce`; there is no dirty-file fast path.
- The sustained ceiling is start-to-start and remains two seconds by default.
- No new public configuration flag is added.
- Prefer a testable timer/clock state machine over sleeps embedded across select branches.

### Make index ingestion transactional and statement-efficient

**Problem**

A two-second append batch can contain hundreds of JSONL rows. The indexer currently issues
per-row dedup queries and inserts plus graph/unification statements without an explicit
event transaction, causing repeated statement preparation, WAL commits, and fsyncs.

**Acceptance criteria**

- Each append event is parsed and applied atomically in an explicit SQLite transaction;
  a failed event leaves `dirty_for_reindex` set and no partially visible index state.
- Dedup and insert work uses prepared or set-based statements and eliminates the separate
  per-row existence round trip where correctness permits.
- Append-event ordering, logical-session unification, overlap dedup, quarantine, complete
  offsets, and deterministic reindex output remain byte-for-byte/row-for-row equivalent.
- Durable mirror ACK remains independent of indexing; index failure never rolls back or
  blocks already durable mirror bytes.
- A 320-row/eight-event benchmark reduces index CPU/wall time by at least 50% and reduces
  SQLite commit/fsync count by at least 75% without increasing append-event loss risk.
- Existing index, replay, backfill, resume, quarantine, and surface gates remain green.

**Settled decisions**

- Start with one transaction per append event; only batch several events together if a
  bounded consumer drain preserves FIFO order and dirty recovery.
- Do not weaken mirror fsync-before-ACK or make indexing synchronous with ACK.
- Keep SQLite and the re-derivable index model.
- Measure transaction and prepared-statement changes separately before adding schema
  constraints that could interact with logical-session unification.

### Shut down the store through its context

**Problem**

The store command currently takes the default `SIGTERM` action, so `main` does not unwind
and deferred cleanup cannot run.

**Acceptance criteria**

- `SIGINT` and `SIGTERM` cancel the store context and return through command execution.
- Ingest and surface listeners close, the index consumer exits, the database closes, and
  deferred cleanup hooks run before process exit.
- Shutdown is bounded and returns success for an operator-requested signal.
- Tests prove both listeners stop accepting, in-flight durable writes either ACK or fail
  without a false ACK, and no goroutine/process remains.

**Settled decisions**

- Use the same signal-context ownership pattern as the shipper.
- Do not add a remote shutdown endpoint.
- Preserve fsync-before-ACK and let already-entered store critical sections finish or fail
  explicitly before listener teardown completes.

## Overall conclusion

The authoritative full pass is a reasonable correctness architecture at this scale. Its
pure walk/stat work is only about 6 ms for 750 files. Replacing it with dirty-set state
would spend substantial correctness complexity to avoid a small term. The cleaner design
is to keep authority simple and make side effects proportional: one cursor commit per
pass, one `/proc` correlation per short TTL, one SQLite transaction per index event, and
an immediate first pass after idle. Those changes address the measured dominant costs
without reopening the backfill/live-tail split that I3 deliberately closed.
