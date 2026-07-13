---
title: "sesh — ship plan (milestones over the lane tickets)"
date: 2026-07-09
status: RATIFIED ORDERING for TASK-085..088. The tickets cut the work by component
  (who owns what); this plan cuts it by time (what integrates when, and when it's
  usable). Both hold simultaneously — a lane worker's backlog order comes from here.
---

# sesh ship plan

Five milestones, M0–M4. Each ends in a demonstrable state with named acceptance
scenarios (spec §6, [S-n]) as its gate — a milestone is done when its scenarios pass on
a real machine, not when its code merges. **M2 is the first useful ship** (the team can
browse one node's sessions); **M4 is done-per-spec**.

The guiding shape is a walking skeleton: bytes flow end-to-end on localhost first with
everything else stubbed, then each milestone thickens one aspect. No lane builds its
component to completion in isolation — integration risk is spent early, on purpose.

## M0 — contract freeze (small, blocks parallelism, days not weeks)

One shared doc PR, authored jointly by the 085+086 workers, sign-off by design authority
(tomo) before merge. Pins exactly:

- Wire: final paths, header names (fingerprint, hostname, OS user, SESSION_OWNER),
  request/response bodies, and error semantics — at minimum: fingerprint mismatch,
  offset beyond high-water (gap), offset below high-water (overlap/replay), out-of-grant,
  malformed identity. Each error names the shipper's required reaction.
- Fingerprint algorithm and window (proposal to beat: SHA-256 over bytes [0, 1024),
  recorded once the file reaches 1 KiB; identity is UUID-only below that — spec §4.1).
- Cursor-recovery GET shape ("what do you have for this file identity?").
- Index schema (the per-message row: message uuid, session id, file uuid, role,
  timestamp, ordinal, byte span, quarantine flag) — this is what lane 3 codes against,
  so freezing it here unblocks 087 at M0, against fixtures, before the store exists.

Deliverable: `tools/sesh/docs/specs/sesh-wire.md` (or spec §8 amendment). Everything below
parallelizes after this merges.

## M1 — walking skeleton: bytes flow (localhost, one node, mirror only)

- 086: `sesh serve` with ingest + mirror only — idempotent byte-range PUT, high-water
  ACK, recovery GET, mirror on disk. No auth beyond localhost binding. No index.
- 085: `sesh ship` with discovery (fsnotify + rescan), cursor registry, tailing,
  backfill-from-zero, truncation reset, move-tracking, deletion GC. No facts beyond
  hostname/OS user, no correlation.

**Gate:** S1 (backfill byte-parity), S3 (truncation), S4 (move), S5 (deletion vs
retention), S9 (replay idempotency), plus kill-and-restart both processes mid-file with
no loss or double-index. **Demonstrable:** this box's full session history, byte-exact,
in the mirror, staying current.

## M2 — readable: index + surface (FIRST USEFUL SHIP)

- 086: parse-on-ingest index per the frozen schema — message-uuid dedup, partial-line
  holdback, quarantine, `sesh reindex`.
- 087: the recency page + transcript drill-down + raw-lines fallback (started at M0
  against fixtures; integrates here).

**Gate:** S2 (resume churn renders one clean transcript), S10 (parser-break drill:
quarantine → raw fallback → reindex recovers), recency ordering check. **Demonstrable:**
anyone pointed at the URL browses this node's sessions. Ship it to the team now —
feedback on the surface starts flowing while M3/M4 build.

## M3 — attributed: facts + correlation

- 085: SESSION_OWNER via /proc — codex fd-exact, claude cohort unanimous-or-absent,
  correlations remembered in the cursor registry; facts on the wire end-to-end; darwin
  build (correlation compiled out) proven on a real laptop.
- 087: display-owner precedence + source labels + honest-absence rendering (small).

**Gate:** S6a (codex exact), S6b (cohort stamp/absence), S7 (cross-user wall), S11
(macOS facts-only). **Demonstrable:** the recency page groups by person, honestly.

## M4 — team-wide: tailnet + rollout (088, plus small 086 auth work)

- 086: tsnet listener, WhoIs stamping, grant check.
- 088: grant policy + deny-path verified BEFORE any real transcript flows off-box;
  packaging + units; rollout node-by-node — at least one macOS laptop and one shared
  multi-user node (two shippers); reboot-survival; store-host migration drill (URL
  change only).

**Gate:** S8 (grant deny + WhoIs stamp), order-free late-node backfill, unit
reboot-survival, migration-without-loss. **Demonstrable:** done-per-spec — every node's
sessions, one page, grant-scoped.

## Dispatch mapping (for the board)

| Milestone | 085 shipper | 086 store | 087 surface | 088 deploy |
|---|---|---|---|---|
| M0 | co-author freeze | co-author freeze | starts vs fixtures | — |
| M1 | core engine | mirror ingest | builds vs fixtures | — |
| M2 | — | index + reindex | integrate + ship | — |
| M3 | correlation + darwin | — | precedence render | — |
| M4 | — | tsnet + grant | — | everything |

Dispatch 085 + 086 + 087 together after M0 sign-off (087 needs only the frozen index
schema); 088 when M2 is demonstrable (its deny-path work can't start before there's a
store to protect, and must finish before M4 transcripts leave the box).

## Standing risks the ordering already hedges

- Upstream format churn mid-build → only the ingest parser (one place, 086) and the
  mirror absorbs everything regardless (I2); M1 is format-blind by design.
- Correlation turns out flakier than the manual validation → M2 already shipped without
  it; M3 degrades to facts-only, which is the macOS posture anyway.
- Tailnet/grant friction → M2 value is intact on localhost while M4 sorts it out.
