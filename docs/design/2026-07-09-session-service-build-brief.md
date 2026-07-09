---
title: "Brief — session service build (shipper + store + surface + deploy)"
date: 2026-07-09
status: DISPATCH BRIEF — ready to cut into tasks the moment the owner settles the three
  micro-decisions (spec §10). The spec it implements is DRAFT; its *shape* is ratified
  (Q18–Q20), so build work may start on everything the micro-decisions don't touch.
---

# Brief: build the session service, spec-first

You are implementing `docs/specs/session-service-spec.md` — read it in full first; it is
the contract. This brief adds working mode, reading order, verify-early items, and the
task cut. Where this brief and the spec disagree, the spec wins.

## Read first, in order

1. `docs/specs/session-service-spec.md` — the contract (invariants I1–I11 are binding;
   acceptance scenarios §6 are the definition of done).
2. `docs/design/2026-07-09-session-shipping-prior-art.md` — *why* each mechanism is shaped
   the way it is, with the upstream bug reports that will bite if you deviate
   (fingerprint identity, ACK-then-advance, truncation reset, rescan-behind-inotify,
   message-uuid dedup).
3. `docs/design/2026-07-09-sessions-missions-boundaries-v2.md` §1A + §3 — component
   doctrine and identity model context.

## Binding rulings (condensed — the spec is authoritative if this drifts)

- **Dumb shipper.** No parsing, no policy, no owner-precedence logic on nodes. If you find
  yourself importing a JSONL schema into the shipper, stop.
- **Bytes on the wire, parse in one place.** The mirror is byte-faithful; the index is
  re-derivable from the mirror; parse failures quarantine, never block ingest.
- **File-driven, never process-driven.** Backfill of dead/pre-install sessions is the same
  code path as live tailing. No dependency on any process being alive.
- **Identity:** (tool, session_id) spine; file identity = uuid + fingerprint (working at
  size ~0); cursors survive renames; size regression = reset + re-ship; deletion ≠
  truncation.
- **Dedup by message uuid at ingest is correctness** — Claude sessions verifiably span
  files with overlapping content.
- **One shipper per OS user** (environ 0400 wall). /proc SESSION_OWNER correlation:
  codex fd-exact; claude (node, os-user, cwd) unanimous-or-absent; macOS none. Hooks are
  not a dependency. Correlations once observed are remembered.
- **Auth:** tailnet only, grant-scoped (not whole-tailnet), identity from WhoIs at the
  store — never from request content.
- **Kill list:** no search, no OTel transport, no hcom/herder/mission awareness anywhere.

## Verify EARLY, before building around it

1. **fsnotify on both session roots** on a live node — event coverage for create, append,
   rename-across-dirs (Claude `/cd`), delete; confirm the rescan interval catches whatever
   inotify misses. (Mechanism is settled; this calibrates the interval.)
2. **Codex fd-join and Claude cohort-join** re-run as code (not shell) on a node with live
   sessions — the manual validation passed 2026-07-08; the implementation must reproduce
   it before the cursor-registry schema freezes.
3. **tsnet WhoIs + grant** smoke test with a second tailnet identity — confirm the deny
   path *before* transcripts flow, not after.

## Task cut (four lanes, cut after spec §10 lands)

1. **Shipper** — discovery/tail/cursor engine + facts + /proc correlation; per-user unit
   files. Cross-platform (Linux + macOS; correlation compiled out on darwin).
2. **Store** — ingest API (idempotent byte ranges, WhoIs stamping, grant check), mirror
   storage, parse-on-ingest index with dedup + quarantine + re-derive command.
3. **Surface** — the one recency page + transcript render + raw-lines fallback.
4. **Deploy** — store host provisioning, tailnet join + grant policy, shipper rollout
   node-by-node (order-free by design).

Lanes 1 and 2 meet at the wire protocol (spec §8) — freeze that first, together, in a
short shared doc PR before parallelizing. Lane 3 depends only on the index schema; lane 4
is unblocked the moment the store boots anywhere.

## Working mode

Owner is bigboss on the bus; design authority for boundary questions is `tomo`
(grilling-context holder). Escalate any deviation from an invariant (I1–I11) before
implementing it — several "obvious optimizations" (parse on node to ship less, path-keyed
cursors, whole-tailnet auth) are specifically ruled out, not overlooked. Acceptance
scenarios §6 are the gate: each task lands with the scenarios it covers demonstrated.
