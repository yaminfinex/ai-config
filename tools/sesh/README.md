# sesh — session-transcript mirroring service

Per-user shippers mirror Claude Code and Codex CLI session transcripts
byte-faithfully to one central store, which parses on ingest and serves a team
recency page.

Authority chain: `docs/specs/session-service-spec.md` (product contract) >
`docs/specs/sesh-wire.md` (wire and index contract).

## Install (field nodes: one command, no repo, no toolchain)

The store serves its own release channel, so **the distribution URL is the
store URL** and onboarding a fresh node is one command (plus, on headless
Linux, one-time lingering):

```sh
curl http://sesh.<tailnet>.ts.net:8765/install.sh | sh
loginctl enable-linger $USER    # headless Linux only, once per node
```

The installer detects OS/arch, reads `latest` once, downloads the binary and
its `SHA256SUMS` entry from immutable version paths, verifies the checksum,
installs to `~/.local/bin/sesh` via atomic rename, and ends by running
`sesh setup --store-url <the URL it was fetched from>` — binary, unit,
store-URL drop-in, and running service in one pass. Re-running the same
command is the upgrade + reconfigure path; extra installer args pass through
to setup (`... | sh -s -- --force`). Day-to-day upgrades are simpler still:
`sesh update` (below).

## Build and install (development)

Standalone Go module (Go ≥ 1.26); no imports from the host repository — this
tool is expected to move repos. The house convention installs user-owned binaries in GOBIN
(`go env GOBIN`, falling back to `$(go env GOPATH)/bin`). Local and installed binaries are
independent copies:

```sh
cd tools/sesh
just build
just install
just versions
```

Cross-compiles: `CGO_ENABLED=0`, darwin/linux × arm64/amd64 (the release
matrix `just release` builds).

## Layout

```
cmd/sesh/        entry point
internal/wire/   frozen types transcribing docs/specs/sesh-wire.md 1:1
internal/ship/   watcher, tailer, cursor registry, correlation
internal/store/  ingest handler, mirror, generations, recovery
internal/index/  parser, logical-session resolution, dedup, quarantine
internal/surface/ recency + transcript pages
internal/setup/  sesh setup engine + embedded systemd unit / launchd templates
internal/cli/    cobra command tree
tests/fixtures/  real captured session JSONL (see tests/fixtures/README.md)
tests/check-*.sh per-scenario gate harnesses (S1..S11)
etc/             install-ship.sh deprecation pointer (absorbed by sesh setup)
```

## Operator Surface

Implemented commands:

```sh
sesh ship --store-url http://127.0.0.1:8765
sesh serve --addr 127.0.0.1:8765 --surface-addr 127.0.0.1:8766
sesh serve --tsnet
sesh setup --store-url http://sesh.<tailnet>.ts.net:8765
sesh update [--check]
sesh reindex
sesh status
sesh admin drop-file <tool> <session_id> <file_uuid> --yes
```

`sesh setup` installs (or reconfigures) the per-user shipper service to run
the binary executing the command: it pins the resolved absolute binary path
into the unit, writes the store URL into the node-local config (Linux: systemd
user drop-in; macOS: launchd plist), preflights the user bus before any write,
and warns when linger is off. Files it writes carry a provenance digest: a
file that still matches its digest is replaced on re-run (URL changes
included — that is the one-command store migration); an operator-edited or
pre-digest file is never overwritten without `--force`, and a `--force`
rewrite still preserves operator env keys other than `SESH_STORE_URL`.

`sesh update` converges this node's binary AND running service to the store's
published latest. The base URL is the `SESH_STORE_URL` the node already
couples on (drop-in / plist; `--store-url` overrides pre-setup); the
replacement target is the unit's pinned executable, asserted equal to the
running updater. Version semantics are equality-only — no ordering is
invented over git-describe strings — so an operator's deliberate `latest`
rollback propagates as a fleet downgrade, always visible (`from -> to` is
printed unconditionally). Replacement is crash-safe: the previous binary is
retained as `sesh.prev` via hardlink before an atomic rename, the target
path is never missing at any crash point, and stray temp files are cleaned
on the next run. Failures before the restart leave the prior install running
(full rollback); failures after the new binary ran keep it in place and
surface any R23 refusal verbatim (failed-but-forward — it may already have
migrated the cursor registry). `--check` exit codes, stable for scripting:
0 up to date, 1 update available, 2 the check failed; downloads nothing.

`sesh status` reports cursor offsets, poisoned files, last ACK age, and store
reachability. It exits nonzero when the configured store is unreachable or any
cursor is poisoned.

`sesh admin drop-file` is an irreversible operator repair. It refuses to run
without `--yes`, removes exactly one mirrored file identity plus its index rows,
leaves sibling files in the same logical session intact, and records the action
in `drop_log`. Hard precondition: stop `sesh serve` before running `drop-file`;
the admin command is a separate process and does not quiesce live ingest or
queued append-index events.

`internal/surface` reads the frozen index schema through its `Store`
seam; `surface.SQLStore` satisfies it from the live store DB + mirror, and
`sesh serve` runs the surface on its own loopback read listener
(`--surface-addr`, default 127.0.0.1:8766 — the port the interim Tailscale Serve
exposure proxies; ingest stays on `--addr`). The surface includes `/` recency,
`/s/{tool}/{id}` transcript pages, `/s/{tool}/{id}/raw` raw mirror fallback,
and `/nodes` last-PUT status. Gates: `tests/check-surface-fixtures.sh`
(fixture-backed renders) and `tests/check-surface-live.sh` (real serve + ship,
S2 renders once).

## Release channel

The ingest listener additionally serves the distribution surface (operator
routes; informational note in the wire doc — nothing about PUT/ACK/recovery
changes, and no shipper *shipping* behavior switches on them):

| Path | Returns |
|---|---|
| `GET /install.sh` | installer, `{{BASE}}` interpolated from the request |
| `GET /releases/latest/VERSION` | version string — the **only** `latest` endpoint |
| `GET /releases/<ver>/sesh-<os>-<arch>` | binary, immutable |
| `GET /releases/<ver>/SHA256SUMS` | manifest for that version, immutable |

GET and HEAD; `/releases/<ver>/*` served immutable-cacheable, `latest` and
`/install.sh` no-cache; 404 for unknown version/asset; 503 while `latest` is
absent (channel not yet published). There is deliberately no `latest/<asset>`
route: readers take `latest/VERSION` once, then immutable paths, so a flip
mid-download cannot mix two releases. In tsnet mode these routes accept
**either** grant verb (`ship` or `read`); no-verb callers are denied.

Publishing is owner-side, two verbs like quick (publish ≠ deploy-store):

```sh
just tag 0.1.0      # monorepo-prefixed tag sesh-v0.1.0; push manually
just release        # defaults to sesh-host:/var/lib/sesh/releases
```

Versions are git-describe strings over the `sesh-v*` tags only (other tags
in the monorepo are ignored), so a tagged release publishes as `sesh-v0.1.0`
and an untagged commit as `sesh-v0.1.0-3-g<hash>` — equality-only semantics,
no ordering invented. The default publish dest is the quick-host deployment;
`sesh-host` is an ssh alias (`ops/README.md` has the IAP ProxyCommand block
and the group membership publishing needs — no sudo in the publish path).

Matrix build (CGO_ENABLED=0, darwin/linux × arm64/amd64, -trimpath, stamped
version, dirty trees refused) → rsync to a remote **staging** dir → remote
`sha256sum -c` → atomic `mv` to `releases/<ver>` (refused if it exists —
version dirs are immutable) → `latest` flipped via temp + rename + sync,
only after promotion. A crashed publish never leaves a partial tree at a
final version path, and the previous `latest` stays fully usable; stray
staging dirs are cleaned by the next run. Rollback = rewrite `latest` to the
previous version string: nodes converge down on their next `sesh update`, a
deliberate and visible fleet downgrade. Gate:
`tests/check-release-publish.sh`.

## Store hosting and deployment (quick-host)

The standing deployment rides the quick-host VM as a sibling service:
dedicated `sesh` system user, system unit `sesh-serve.service` running
`sesh serve --tsnet` with data under `/var/lib/sesh`, and GCS backups every
15 minutes under a sesh-only bucket prefix. The store embeds tsnet as its
own tailnet node, so quickd, Caddy, and the VM's tailscaled are untouched
and restarts are independent. Everything lives in `ops/`:

- `ops/bootstrap.sh` — idempotent VM bring-up (user, dirs, units,
  TS_AUTHKEY first-start handoff); a re-run with nothing changed is a no-op
- `just deploy-store` — build → IAP scp → crash-safe binary swap
  (`sesh.prev` retained via hardlink, atomic rename; the only known-good
  binary is never overwritten in place) → restart → prints the version of
  the **running** store image
- `ops/backup.sh` + timer — sqlite snapshot-API copy (never the live file),
  upload ordering that keeps the backup restorable, tsnet identity included
- `ops/README.md` — the owner execution runbook (admin key → bootstrap →
  deploy → deny-verify → first tagged release → announcement), the restore
  drill, the escape triggers for leaving the shared host, and the
  version-skew policy

Gate: `tests/check-store-deploy.sh`.

## Interim read-only exposure runbook

Until tailnet-native auth is verified, keep ingest private to the local machine. The ingest listener
rejects non-loopback binds:

```sh
sesh serve --addr 127.0.0.1:8765 --surface-addr 127.0.0.1:8766
```

Expose only the read-only surface port with Tailscale Serve:

```sh
tailscale serve --bg --http=443 http://127.0.0.1:8766
```

Do not expose `127.0.0.1:8765`; it accepts transcript bytes. The read listener
serves the browser surface only, while ingest remains loopback-only until
tailnet-native auth and rollout verification land.

Owner sign-off before exposing the read-only surface: PENDING (`@bigboss`).

## Tailnet-native grant runbook

Tailnet-native mode embeds tsnet in `sesh serve`. The store joins the tailnet as its own
node, authenticates each caller with WhoIs, stamps the authenticated user or
node identity into the fact log, and denies callers without the matching
Tailscale app-capability verb before ingest bytes or read handlers run.

```sh
TS_AUTHKEY=tskey-auth-... \
sesh serve --tsnet \
  --tsnet-hostname sesh \
  --addr :8765 \
  --surface-addr :8766
```

`--tsnet-dir` defaults to `<data-dir>/tsnet`; `--tsnet-auth-key` can be used
instead of `TS_AUTHKEY`. The hard grant is the Tailscale app capability
`infinex.xyz/cap/sesh`; values grant one or both verbs:

- `{"verb":"ship"}` permits PUT ingest (and the distribution routes).
- `{"verb":"read"}` permits the read-only surface (and the distribution routes).

### The tailnet-admin ask (one-time, Tier-B: one tag + one grant + one key)

Paste-ready for the tailnet admin. Note the Tailscale fact baked in here:
**tag owners cannot mint auth keys** (key generation is admin-class), so the
ask includes one reusable tagged key, and key expiry is disabled on the node
once it joins (standard for servers).

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
> 3. Once the node joins: disable key expiry on it.
>
> After that, changing who can ship/read is just editing that grant — no
> further asks from me unless the node re-keys.

Every member device can ship and read — right for an internal team;
tightening later is an edit to that one grant, no sesh change. Tagged nodes
are *not* `autogroup:member`, so any tagged node doubles as the out-of-grant
deny probe for rollout verification. (If the owner prefers re-key
self-sufficiency, the alternative third item is an OAuth client with the
auth-keys scope tagged `tag:sesh` — bigger ask once, zero asks after.)

WhoIs stamps user login names for user-owned nodes. Tagged or otherwise
login-less nodes stamp as the node name reported by WhoIs, usually the node's
MagicDNS name; expect the store fact log to show node names rather than user
emails for those clients.

## Rollout Runbook

The store URL is the only coupling between a node and the store (owner
ruling); everything below preserves that. Nothing in units or scripts assumes
where this repo lives.

### Order: store first, then nodes in any order

**1. Store up.** On quick-host (the standing deployment) this step is
`ops/bootstrap.sh` + `just deploy-store` — the full owner runbook, including
the backup restore drill, is `ops/README.md`. On any other host:

```sh
GOOS=linux GOARCH=amd64 go build ./cmd/sesh   # or the matching platform
TS_AUTHKEY=tskey-auth-... sesh serve --tsnet --tsnet-hostname sesh \
  --addr :8765 --surface-addr :8766 --data-dir /var/lib/sesh
```

The store URL in tsnet mode is plain `http://` on purpose: the tailnet
itself WireGuard-encrypts and peer-authenticates every connection, so TLS
here would add certificate lifecycle without any confidentiality gain —
TLS termination lives in the interim `tailscale serve` path, not in
tsnet mode. Using `https://` against the tsnet listener fails at transport.

**2. Grant applied.** Push the Tier-B ACL/grant (Tailnet-native grant
runbook above): `autogroup:member` gets both verbs against `tag:sesh`, and a
release is published to the channel (`just release`).

**3. DENY VERIFIED — before any real transcript flows** (Lane-4 binding:
grant scope before content). The probe path must use syntactically valid
UUIDs: non-UUID segments answer 400 `malformed_request` before the grant
check runs, which proves nothing. From a tailnet device OUTSIDE the grant —
any `tag:sesh`-tagged node qualifies, since tagged nodes are not
`autogroup:member`:

```sh
SID=$(uuidgen); FID=$(uuidgen)
curl -si -H 'X-Sesh-Wire-Version: 1' \
  "http://sesh.<tailnet>.ts.net:8765/v1/files/claude/$SID/$FID"
# REQUIRED: 403 out_of_grant (or connection refused by ACL). Anything else
# stops the rollout here.
```

From an in-grant device, the same GET must answer 404 `not_found` — that
pair (403 outside, 404 inside) is the deny evidence. Record both outputs in
the rollout log; the owner ratifies field readiness against them.

**4. Nodes, any order.** Late nodes need no special handling: the shipper's
first pass is the same authoritative rescan as every later one, so a node
onboarded a week late backfills its full local history (up to Claude's
30-day retention) unaided. Per node, per shipping user — one command, no
repo, no toolchain (Linux and macOS take the same command):

```sh
curl http://sesh.<tailnet>.ts.net:8765/install.sh | sh
```

Developers working from a checkout can use `just deploy <store-url>`
instead; it installs the local build and delegates to the same `sesh setup`
code path. Later upgrades on any node: `sesh update`. For older
installations pinned to `/usr/local/bin/sesh`, run the installer (or
`just deploy` + `just restart`); setup pins the resolved absolute path of
the binary that ran it, and the restart completes the migration.
`just versions` reports cleanly once the new binary is running; the old
binary predates the version command. Remove the root-owned copy after that
check. Install and upgrade do not require sudo.

Linux reboot survival on nodes nobody logs into additionally requires
lingering — `sesh setup` warns when it is off:

```sh
loginctl enable-linger $USER
```

Shared multi-user node: run `sesh setup` once per OS user (each gets
its own unit, registry, and uid; the cursor-registry flock refuses a second
shipper per user). Verify with `systemctl --user status sesh-ship` under
each uid and two distinct `X-Sesh-OS-User` values in the store's `last_seen`.

### Per-node verification checklist (field, per platform)

- [ ] `sesh status` exits 0: store reachable, cursors advancing, none poisoned.
- [ ] Node appears on the surface `/nodes` page with a fresh last-PUT.
- [ ] Reboot the node (or `launchctl bootout gui/$UID/dev.sesh.ship` +
      re-login on macOS): unit is running again without operator action.
- [ ] User re-login does not duplicate the shipper (flock holds: exactly one
      `sesh ship` per user).
- [ ] Late-onboard check (one node): install ≥1 day after first use; confirm
      the surface shows that node's pre-install sessions (30-day backfill).

### Store host migration drill (zero shipper changes, zero loss)

The store's tailnet identity IS its URL; move the identity, not the nodes.

1. Record pre-state on the old host: `sesh status` from one node; note a few
   `high_water` values via recovery GETs; snapshot the `/nodes` page.
2. Stop the store process (shippers hold position by design — cursor
   untouched, source files remain the buffer; nothing is lost while dark).
3. Copy the whole data dir (mirror/ + store.sqlite + tsnet/) to the new
   host, e.g. `rsync -a /var/lib/sesh/ newhost:/var/lib/sesh/`. The tsnet
   state dir carries the node key: the new host comes up AS `sesh`,
   same MagicDNS name, same grant.
4. Start `sesh serve --tsnet ...` on the new host with the same flags.
   Decommission the old host's copy (never run two stores on one identity).
5. Verify, touching NO node: shippers resume on their own (next pass or
   backoff retry); recovery GETs return the recorded high_waters; `/nodes`
   repopulates; one node's live session keeps appending end-to-end.
6. Loss check: byte-compare one mirrored file against its source on a node
   (`cmp`), and confirm `sesh status` is 0 fleet-wide.

If step 3 cannot carry the tsnet dir, re-auth the new host with the same
`--tsnet-hostname sesh`; the MagicDNS URL is preserved and nodes still
change nothing.

### URL migration runbook (any URL → any URL)

When the store URL itself changes (new tailnet identity, new hostname), the
sanctioned recovery story is "URL changed → install again" — one command per
node, because the installer rewrites the drop-in and setup's provenance
digest lets an unmodified node migrate without `--force`.

1. **Baseline inventory FIRST**: snapshot the old store's `/nodes` list (or
   the fleet list if the old store is already dead). This is the checklist
   the migration is done against — the new store cannot provide it: with
   migrated data its `/nodes` already shows every node's historical rows, so
   "appears on the new `/nodes`" proves nothing about migration.
2. Stand up the store at the new URL (new tsnet identity, or a moved host).
   Same data dir; nothing about ingest state changes.
3. Publish the current release to the new URL's releases dir (or it traveled
   with the data dir).
4. Slack the one-liner: `curl http://<new>/install.sh | sh`. Each node:
   binary refreshed, drop-in rewritten to the new URL, service restarted.
   Nodes with operator-edited drop-ins are the exception and need
   `... | sh -s -- --force` (the refusal message says exactly this).
5. Check off each baseline node by **last-PUT after the cutover time** on
   the new store — the only signal that distinguishes a migrated node from
   its migrated history.
6. **The migration has a deadline, not indefinite patience.** Un-migrated
   nodes hold position against the dead URL with the source files as buffer —
   but clients delete transcripts on their own ~30-day cycle, so a straggler
   that misses the window loses its unshipped tail for good. Complete the
   checklist well inside the retention horizon (target: days; hard limit:
   the shortest client retention among fleet nodes).

### Field failure signature: stale binary vs newer registry

An outdated `sesh` build started against a registry written by a newer build
refuses cleanly — exit nonzero, registry untouched, unit in a restart loop
rather than shipping wrong. The log signature:

```
cursor registry <path> carries schema generation N but this sesh build only
understands generation M: this binary is older than the registry (likely
cause: an outdated sesh build on this node). Remedy: run the newer sesh
build that wrote the registry, or upgrade this installation and retry. The
registry file has been left untouched.
```

Operator action is exactly the message's remedy: upgrade the binary at its
pinned path and `systemctl --user restart sesh-ship` (macOS:
`launchctl kickstart -k gui/$UID/dev.sesh.ship`). Never delete or move the
registry to "fix" this; that discards the newer state and re-ships the world.
