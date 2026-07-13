# sesh store-served distribution — design (TASK-168)

Status: **ratified by owner 2026-07-13** — all seven DPs land as recommended:
DP-4 as option (b) provenance digest, DP-7 as the reusable-key ask, DP-1/2/3/5/6
per their recommendations. Implementation may proceed via T-0 → T-A → T-B (§11).
Revised before ratification after an independent design review (codex,
2026-07-12) that verified it against the code, the frozen specs, quick's
reference implementation, and Tailscale's documented behavior; all findings are
folded in below. Implements option 1c from backlog doc-002
(sanctioned by owner 2026-07-12): the store serves its own release channel, so
**the distribution URL is the store URL** and onboarding is one curl. Every open
choice is a numbered decision point (DP-n) with a recommendation; none is
exercised here.

Owner riders (from the sanctioning conversation, binding on this design):

- tsnet day 1 is fine — the tailnet-admin ask just has to be visible early and
  owner-sized. The ask below is the Tier-B shape the owner approved in
  principle (one tag + one grant), corrected for a Tailscale fact the review
  caught: tag owners cannot mint auth keys, so the ask includes one key.
- URL changes are an accepted migration tool: "store URL changed → install
  again" is the recovery story, and this design makes it literally one command.
- Every follow-up implementation task keeps all relevant docs current
  (backlog decision-001); §10 enumerates the doc diff per task.

Frozen and untouched: wire v1 (docs/specs/sesh-wire.md), ACK durability, R23,
I1–I11, store-URL-only node→store coupling. The drop-in preservation rule gets a
*proposed refinement* in DP-4, flagged, not exercised.

## 1. Naming (owner-ruled this session)

- Tailnet node hostname: **`sesh`** (not `sesh-store`). Store URL:
  `http://sesh.<tailnet>.ts.net:8765`.
- ACL tag: **`tag:sesh`**.
- App capability: **`infinex.xyz/cap/sesh`** (DP-1). The current
  `sesh.dev/cap/store` violates the Tailscale convention that capability names
  are namespaced by a domain the org controls; we don't own sesh.dev. The
  constant lives only in `internal/store/listen_tsnet.go` + README today and no
  grant referencing it has ever been deployed, so the rename is free now and
  expensive after the first ACL ships. Verbs unchanged: `ship`, `read`.

**DP-1** — rename capability to `infinex.xyz/cap/sesh` and hostname/tag to
`sesh`/`tag:sesh` before any tsnet rollout. *Recommended: yes.* Touched files:
`listen_tsnet.go`, `internal/cli/root.go`, `etc/install-ship.sh` usage text,
`tests/check-deploy-artifacts.sh`, README, doc-002 cross-refs.

## 2. System overview

```
owner machine                     store host                        fleet node
─────────────                     ──────────                        ──────────
just release ──scp/rsync──▶ <data-dir>/releases/<ver>/…
                                    │
                                    ▼ (same listener as ingest)
                     GET /install.sh, /releases/…   ◀──curl──  teammate (Slack link)
                                                               │ installer runs:
                                                               ▼
                                                    sesh setup --store-url $BASE
                                                               │ renders unit + drop-in
                                                               ▼
                                                    sesh-ship.service ──PUT──▶ store
                                                               ▲
                                              sesh update ─────┘ (same /releases)
```

Three components, three follow-up tasks (§11): `sesh setup` (T-A), the release
channel + `sesh update` (T-B), naming/capability rename (T-0, tiny, first).
Store hosting/deploy (quick-host unit, backup, `just deploy-store`) is doc-002
T3, unchanged by this design and not re-specified here.

## 3. Endpoints

Served from the **ingest listener** (`--addr`, :8765) so `$BASE` ==
`SESH_STORE_URL` with no port arithmetic (DP-2). All GET-only, no transcript
data, no state mutation:

| Path | Returns |
|---|---|
| `GET /install.sh` | installer script, `{{BASE}}` interpolated from the request Host + scheme (quick apex.go pattern) |
| `GET /releases/latest/VERSION` | version string, text — the **only** `latest` endpoint |
| `GET /releases/<ver>/sesh-<os>-<arch>` | binary, immutable |
| `GET /releases/<ver>/SHA256SUMS` | manifest for that version, immutable |

There is deliberately no `latest/<asset>` route: installer and updater fetch
`latest/VERSION` once, then everything else from immutable `/releases/<ver>/`
paths — so a `latest` flip mid-download cannot mix artifacts from two releases
(a race quick's updater actually has). GET and HEAD both supported (quick
serves both). Cache headers: `/releases/<ver>/*` immutable;
`latest/VERSION` and `/install.sh` no-cache.

Backing store: `<data-dir>/releases/<ver>/` + a `latest` file (version string,
written via temp+rename+dir-fsync — atomic *and durable* pointer flip; same
fsync discipline the shipper already applies to cursor-registry writes). The
releases dir is **rebuildable class** for backup purposes (re-publish
regenerates it) but rides the migration drill for free since it lives in the
data dir.

Auth: in tsnet mode these paths accept **either** verb (`ship` or `read`).
This needs new middleware, not just new routes: today `AuthHandler` wraps each
listener once with a single verb (`internal/cli/root.go` wires ingest=ship,
surface=read; `listen_tsnet.go`'s handler checks exactly one capability), so
T-B must add route-scoped any-of-verbs auth for the distribution paths, with
tests for read-only callers reaching `/install.sh` on the ingest port and
no-verb callers being denied. 404 for unknown version/asset; 503 while
`latest` is absent (channel not yet published).

Wire-doc hygiene: one **informational** line in sesh-wire.md's non-contract
section — these endpoints are operator surface; no shipper *shipping* behavior
switches on them (`sesh update` is an explicit operator verb, not the ship
loop). No wire amendment: nothing about PUT/ACK/recovery changes.

**DP-2** — distribution on the ingest listener vs the read surface (:8766).
*Recommended: ingest listener.* Surface placement would keep the ingest mux
minimal but breaks the one-URL property (installer would have to derive the
ingest URL by port convention — brittle) and puts ~50MB binary downloads on the
listener meant for humans browsing transcripts. The ingest mux already carries
GET (recovery); adding read-only routes does not change its contract.

## 4. install.sh

~60 lines of POSIX sh, embedded in the store binary (go:embed), `{{BASE}}`
interpolated server-side. Behavior:

1. Detect `uname -s`/`-m` → `sesh-<os>-<arch>`; unsupported → clear error.
2. `GET $BASE/releases/latest/VERSION` once, validate the string, then download
   `$BASE/releases/$VER/sesh-<os>-<arch>` and `$BASE/releases/$VER/SHA256SUMS`
   (immutable paths — no torn release across a `latest` flip) to a temp file;
   verify checksum (`sha256sum`/`shasum -a 256`, whichever exists); fail loudly
   on mismatch.
3. Install to `~/.local/bin/sesh` via temp + `chmod 0755` + atomic rename
   (DP-3). Warn (don't fail) if `~/.local/bin` is not on PATH — the service
   doesn't need PATH; only the human's shell does.
4. Exec `~/.local/bin/sesh setup --store-url "$BASE" "$@"` — extra args pass
   through (`curl …/install.sh | sh -s -- --force`), so the Slack announcement
   controls semantics without a new script.

Re-running the same command is the upgrade+reconfigure path (idempotent by
`sesh setup`'s rules, §5). The installer never touches units itself — all
service logic lives in Go where it's testable.

**DP-3** — flat `~/.local/bin/sesh` vs immutable version dirs
(`~/.local/lib/sesh/<ver>/` + symlink, doc-002's hardening option).
*Recommended: flat file now.* Atomic rename over the running path is safe on
Unix; `sesh update` retains `sesh.prev` (§6) which covers the realistic
rollback need at this fleet size. Version dirs add GC + unit-target policy for
a benefit R23 partially negates anyway (a rolled-back binary against a migrated
registry is refused by design). Revisit if update failures actually occur.

## 5. `sesh setup` interplay (absorbs etc/install-ship.sh)

Full spec is doc-002 T1; this section only fixes what the channel design needs:

- Embedded templates (systemd unit, launchd plist), absolute binary path from
  `os.Executable()` **plus `filepath.EvalSymlinks`** (os.Executable alone does
  not resolve symlinks; quick's updater does this explicitly and so must we —
  the unit must pin the real file the updater will later replace), store-URL
  drop-in, user-bus preflight before any write, linger warning, `--dry-run`.
  `just deploy` delegates to it so repo and field paths share one code path.
- **Config mechanics are two different shapes per OS** and T-A specifies both:
  on Linux the URL lives in a systemd env drop-in
  (`~/.config/systemd/user/sesh-ship.service.d/10-local.conf`) which may also
  carry operator env like `SESH_STATE_DIR` — parsing and rewriting must
  preserve unknown keys and systemd quoting; on macOS the URL and the
  executable path coexist in one plist — a URL rewrite must not disturb the
  ProgramArguments. Round-trip tests for both.
- **The URL-change case (DP-4).** Frozen rule: never clobber an
  operator-edited drop-in without `--force`. The sanctioned recovery story —
  "URL changed → install again" — hits exactly this: an existing drop-in with
  the *old* URL differs from what setup wants to write. An earlier draft
  proposed inferring "setup wrote this" from byte-equality with a canonical
  render; the design review killed that correctly: **shape equality cannot
  establish provenance** — an operator who edits *only* the URL value produces
  a file identical to setup's render "for some URL", and would be silently
  clobbered. Two sound options remain:
  - **(a) Always-refuse (status quo semantics, zero new code):** URL-migration
    announcements include the flag: `curl …/install.sh | sh -s -- --force`.
    Blunt: `--force` also overrides genuinely edited drop-ins on customized
    nodes.
  - **(b) Durable provenance digest:** setup appends a trailing comment line
    `# sesh-setup: sha256=<digest of the content above>` when it writes.
    On the next run, digest verifies → the file is exactly as setup wrote it
    (any operator edit, including URL-only edits, breaks the digest) → safe to
    replace with the new explicit URL. Digest absent/broken → refuse without
    `--force`, exactly as today. ~20 lines + tests; announcements need no
    flag; operator edits stay protected even *against* `--force`-happy
    copy-paste, because the refusal message can distinguish "operator-edited"
    from "pre-digest legacy file".

**DP-4 (revised)** — always-refuse (a) vs provenance digest (b).
*Recommended: (b).* It is the only shape that makes one-command URL migration
true on unmodified nodes without weakening protection on modified ones. If the
owner prefers zero new semantics, (a) is honest and costs one flag in one
Slack message per migration.

## 6. `sesh update`

`sesh update [--check]`, quick's cmd_update.go pattern plus the service half
quick doesn't need:

1. Resolve base URL: the installed drop-in (`SESH_STORE_URL`) read from the
   per-OS config location (Linux env drop-in; macOS plist — §5's parsing
   rules); `--store-url` flag overrides (for pre-setup use). No new config
   surface — the updater uses the URL the node already couples on. The
   replacement target is the unit's pinned executable path (resolved per §5),
   asserted equal to the running updater's own resolved path — never replace a
   binary the service doesn't actually run.
2. `GET /releases/latest/VERSION` **once**; all further fetches use immutable
   `/releases/<ver>/` paths. Equal to `buildinfo.Version` → "already up to
   date", exit 0. `--check`: report + exit 1 if different, download nothing.
   **Version semantics are equality-only**: git-describe strings have no
   defined ordering and none is invented. `latest != running` means "converge
   to latest", which makes an operator's deliberate `latest` rollback
   propagate as a fleet downgrade — that is the rollback feature, and
   `sesh update` always prints `from → to` so a downgrade is visible, never
   silent.
3. Download matching asset, verify its SHA256SUMS entry → temp file next to the
   target (same filesystem, atomic rename possible).
4. **Crash-safe replacement ordering** (the naive rename(current→prev) has a
   window where the configured path has *no binary at all*): first create
   `sesh.prev` as a hardlink (or copy) of the current binary — the target path
   is never unlinked; fsync; then atomically rename temp → target; fsync the
   directory. Defined recovery for every partial state: stray `.tmp` files are
   deleted on the next run; a `sesh.prev` with an intact target is inert; the
   target itself is never missing at any crash point.
5. Restart the unit (`systemctl --user restart sesh-ship.service` /
   `launchctl kickstart -k`), then verify the **running image** reports the new
   version (same `/proc/<pid>/exe` mechanism as `just versions`; macOS: on-disk
   check with the README's stated caveat). Updated-but-not-restarted is
   reported as failure, not success.
6. **Failure taxonomy — the guarantees differ before and after service start**:
   - Failure before step 5's restart (download, checksum, rename): the prior
     install is untouched and still running — full rollback guaranteed.
   - Failure at verification, after the new binary ran: it may already have
     migrated the cursor registry, and the old binary will then *correctly
     refuse* via R23. In that case `sesh update` keeps the (compatible) new
     binary in place, surfaces R23's message verbatim, and reports the update
     as failed-but-forward — it never claims a rollback it can't deliver, and
     never leaves the refused old binary as the service target.
   - If the restart itself fails, that is reported as failure regardless of
     which binary is on disk.

## 7. Release publishing (`just release`)

Owner-side, two-verb discipline exactly like quick (publish ≠ deploy-server):

```
just release:
  1. matrix build (CGO_ENABLED=0, darwin/linux × arm64/amd64, -trimpath,
     stamped version) into releases/<ver>/ + SHA256SUMS   [scripts, quick build.sh shape]
  2. rsync releases/<ver>/ to a REMOTE STAGING dir (releases/.staging-<ver>/)
  3. remotely verify staged SHA256SUMS (sha256sum -c)
  4. atomic remote `mv` staging → releases/<ver>/ — refused if <ver> already
     exists (version dirs are immutable; republishing a version is an error,
     not an overwrite)
  5. write `latest` via temp + rename + dir fsync — only after step 4
```

Staging-then-rename is the lesson quick's release recipe learned the hard way
(its justfile comment documents the killed-mid-scp failure): a crashed publish
must never leave a partial tree at a *final* version path that a retry mutates
or `latest` might point to. Stray `.staging-*` dirs are cleaned by the next
run. Failed publish leaves the previous `latest` fully usable. Rollback =
rewrite `latest` to the previous version string (a deliberate fleet downgrade —
see §6 step 2's equality-only semantics). Requires ssh access to the store
host — owner-only in practice, matching "the store is owner-operated".
Dirty-tree builds refuse to publish (git-describe `-dirty` suffix rejected by
the recipe).

## 8. Exposure: tsnet day 1, and the admin ask

Primary path (owner-approved shape): **tsnet from day 1** — the store joins the
tailnet as node `sesh`, WhoIs-stamps every caller, denies before body without
the capability. No Caddy, no tailscale-serve, no quickd involvement, regardless
of which host runs it.

### The tailnet-admin ask (paste-ready, one-time)

The design review corrected an earlier claim here: **tag owners cannot mint
auth keys** — Tailscale restricts key generation to admin-class roles
(Owner/Admin/IT admin/Network admin); tag ownership only authorizes *applying*
the tag. (The owner's instinct that they might lack key permission was right.)
So the one-time ask includes one key, and the node's key-expiry toggle:

> Hi — I'm standing up an internal service node (`sesh`) on the tailnet.
> One-time ask, three small parts:
>
> 1. This ACL addition:
> ```json
> "tagOwners": {
>   "tag:sesh": ["yamen@ibra.au"]
> },
> "grants": [{
>   "src": ["autogroup:member"],
>   "dst": ["tag:sesh"],
>   "ip":  ["tcp:8765", "tcp:8766"],
>   "app": {"infinex.xyz/cap/sesh": [{"verbs": ["ship", "read"]}]}
> }]
> ```
> 2. One **reusable** auth key with `tag:sesh` applied (Settings → Keys).
> 3. Once the node joins: disable key expiry on it (standard for servers).
>
> After that, changing who can ship/read is just editing that grant — no
> further asks from me unless the node re-keys.

If the owner wants true self-sufficiency for future re-keys, the alternative
third item is an **OAuth client with the auth-keys scope tagged `tag:sesh`** —
a slightly bigger ask up front, zero asks forever after. Owner's call on which
to request (DP-7).

Properties: every member device can ship and read (right for an internal team);
tightening later is an edit to that one grant, no sesh change; tagged nodes are
*not* `autogroup:member`, so any tagged node doubles as the out-of-grant
deny-probe for M4 verification (expect 403/refusal; in-grant unknown-UUID
expects 404) — the deny-verification pair from the README runbook works day 1.

**DP-7** — one reusable key + expiry-disable vs an OAuth client (auth-keys
scope) for re-keying self-sufficiency. *Recommended: the key* — the node
re-keys rarely (expiry disabled), and the smaller ask matches the owner's
stated preference for not creating admin work.

**DP-5** — grant `src`: `autogroup:member` vs named group.
*Recommended: autogroup:member.* A named group is Tier-C territory: finer
control, but each membership change is another admin touch — the wrong trade at
this team size.

### Fallback if the ACL ask stalls (kept in the design, not the default)

Stage-0 no-tsnet: bind ingest+distribution on the host's existing tailnet
interface (`--addr <tailscale-ip>:8765`), URL = the host's MagicDNS name.
Zero admin interaction, works today. Honest risk: **unauthenticated ingest**
reachable by every tailnet principal (write-only — the read surface stays
loopback/tailscale-serve per the current runbook, so no transcript exposure).
If used, the posture must be explicit: a serve flag named for what it does
(e.g. `--allow-unauthenticated-ingest`) so the unit file documents the stance.
Moving to tsnet later is the sanctioned URL-migration: publish to the new node,
Slack the new curl, nodes converge one command at a time; stragglers keep
buffering against the dead URL harmlessly (contractual hold-position) and are
visible on `/nodes`.

**DP-6** — if Stage-0 is ever used, does it need the explicit-flag guard?
*Recommended: yes — refuse non-loopback, non-tsnet ingest binds without the
flag.* Cheap, and it makes the interim stance visible in `systemctl cat`.

## 9. URL migration mechanics (any → any)

1. **Baseline inventory first**: snapshot the old store's `/nodes` list (or
   the fleet list, if the old store is already dead) — this is the checklist
   the migration is done against. The new store cannot provide it: with
   migrated data its `/nodes` already shows every node's historical rows, so
   "appears on the new `/nodes`" is not proof of migration.
2. Stand up the store at the new URL (new tsnet identity, or moved host with
   the same identity — the drill). Same data dir; nothing about ingest state
   changes.
3. Publish current release to the new URL's releases dir (or it traveled with
   the data dir).
4. Slack: `curl http://<new>/install.sh | sh` (plus `-s -- --force` only if
   DP-4 lands as always-refuse). Each node: one command — binary refreshed,
   drop-in rewritten to the new URL, service restarted.
5. Check off each baseline node by **last-PUT after the cutover time** on the
   new store — the only signal that distinguishes a migrated node from its
   migrated history.
6. **The migration has a deadline, not indefinite patience.** Un-migrated
   nodes hold position against the dead URL with the source as buffer — but
   I7's flip side is that *clients* delete transcripts on their own ~30-day
   cycle, so a straggler that misses the window loses the unshipped tail for
   good. Complete the checklist well inside the retention horizon (target:
   days, hard limit: shortest client retention among fleet nodes).

## 10. Docs plan (standing ask — backlog decision-001)

Every implementation task carries its doc updates as acceptance criteria:

| Doc | Change | Task |
|---|---|---|
| `tools/sesh/README.md` | hostname/tag/cap rename throughout (9 refs); install section rewritten around the curl flow; rollout + grant runbooks updated with the Tier-B ask; migration drill gains the URL-migration variant (§9) | T-0, T-A, T-B |
| `docs/specs/sesh-wire.md` | one informational line: distribution endpoints are operator surface, non-contract | T-B |
| `docs/specs/session-service-spec.md` | no change (verified: no invariant touches distribution; no naming refs) | — |
| `etc/install-ship.sh` | deprecation pointer → `sesh setup` after one release of dry-run parity | T-A |
| `tools/sesh/justfile` | `deploy` delegates to `sesh setup`; new `release` recipe; comments current | T-A, T-B |
| backlog doc-002 | already carries the owner decision note; cross-ref this design | done (this unit) |
| `internal/store/listen_tsnet.go`, `internal/cli/root.go`, `tests/check-deploy-artifacts.sh` | rename constants/help text/assertions | T-0 |

## 11. Decision points summary and follow-up tasks

| DP | Question | Recommendation |
|---|---|---|
| DP-1 | rename to `sesh`/`tag:sesh`/`infinex.xyz/cap/sesh` | yes, before any tsnet rollout |
| DP-2 | distribution endpoints on ingest listener | yes (with §3's route-scoped any-of-verbs auth) |
| DP-3 | flat binary path (no version dirs) | yes, contingent on §6's crash-safe replacement ordering |
| DP-4 | URL-change drop-in handling | provenance digest (b); always-refuse (a) is the zero-code fallback — shape-equality "pristine detection" is rejected as unsound |
| DP-5 | grant src = `autogroup:member` | yes |
| DP-6 | explicit flag for any no-auth ingest bind | yes (fallback path only) — note this *relaxes* the currently unconditional loopback enforcement in `internal/cli/root.go`, so it ships with refusal tests, not just the flag |
| DP-7 | admin ask: reusable tagged key vs OAuth client | key + expiry-disable (smaller ask) |

Follow-up implementation tasks (filed-ready; sequence T-0 → T-A → T-B; doc-002
T3/T4 unchanged and compose after):

**T-0 — sesh: naming + capability rename (`sesh`, `tag:sesh`, `infinex.xyz/cap/sesh`)**
*AC:* (1) constant, help text, test assertions, README, install-ship.sh usage
renamed; (2) no deployed grant exists yet (verified) so no migration path
needed; (3) grep for old names returns only historical backlog docs.

**T-A — sesh: `sesh setup` self-installing subcommand**
doc-002 T1, plus from this design: DP-4 semantics as ratified (digest
provenance or always-refuse), `os.Executable()`+`EvalSymlinks` path
resolution, §5's per-OS config parsing rules, and the §10 doc rows it owns.
*AC:* doc-002 T1 ACs + (5) DP-4 behavior unit-tested both ways: an
operator-edited drop-in (including a URL-only edit) always refuses without
`--force`; with digest provenance, a digest-intact drop-in is replaced when a
new URL is explicit; (6) Linux drop-in rewrite preserves unknown env keys
(e.g. `SESH_STATE_DIR`) and systemd quoting; macOS plist URL rewrite leaves
ProgramArguments untouched; round-trip tests for both; (7) §10 docs rows
landed.

**T-B — sesh: store-served release channel + `sesh update`**
Endpoints per §3 on the ingest listener with route-scoped any-of-verbs auth,
install.sh per §4 (VERSION-once, immutable fetch paths), update per §6
(crash-safe replacement, equality-only version semantics, failure taxonomy),
`just release` per §7 (staging + verify + atomic mv + durable latest flip).
*AC:* (1) fresh Linux node onboards with one curl + enable-linger, no
repo/toolchain; (2) `sesh update` converges binary *and running service*;
the target path is never missing at any crash point (hardlink-prev then
rename, tested with injected failures); pre-restart failures leave the prior
install running, post-start verification failures keep the forward binary and
surface R23 verbatim per §6's taxonomy; (3) failed or interrupted publish
leaves previous `latest` usable and no partial tree at a final version path;
republishing an existing version is refused; (4) distribution endpoints admit
ship-only and read-only callers and deny no-verb callers in tsnet mode, with
tests at the middleware level; (5) `latest` rollback propagates as a visible
downgrade (`from → to` printed) and is covered by a test; (6) wire-doc
informational note + README install rewrite + URL-migration runbook (§9,
incl. baseline inventory and retention deadline) landed (§10); (7) `--check`
exit codes stable for scripting.
