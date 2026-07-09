# sesh — session-transcript mirroring service

Per-user shippers mirror Claude Code and Codex CLI session transcripts
byte-faithfully to one central store, which parses on ingest and serves a team
recency page.

Authority chain: `docs/specs/session-service-spec.md` (invariants I1–I11) >
`docs/specs/sesh-wire.md` (wire freeze, M0) >
`docs/plans/2026-07-09-001-feat-sesh-session-service-plan.md`.

## Build

Standalone Go module (Go ≥ 1.26); no imports from the host repository — this
tool is expected to move repos. Plain `go build`, no wrappers:

```sh
cd tools/sesh
go build ./cmd/sesh
```

Cross-compiles: `GOOS=darwin GOARCH=arm64` and `GOOS=linux GOARCH=amd64`.

## Layout

```
cmd/sesh/        entry point
internal/wire/   frozen types transcribing docs/specs/sesh-wire.md 1:1
internal/ship/   watcher, tailer, cursor registry, correlation
internal/store/  ingest handler, mirror, generations, recovery
internal/index/  parser, logical-session resolution, dedup, quarantine
internal/surface/ recency + transcript pages
internal/cli/    cobra command tree
tests/fixtures/  real captured session JSONL (see tests/fixtures/README.md)
tests/check-*.sh per-scenario gate harnesses (S1..S11)
etc/             systemd unit, launchd template, install-ship.sh
```

## Operator Surface

Implemented M2 commands:

```sh
sesh ship --store-url http://127.0.0.1:8765
sesh serve --addr 127.0.0.1:8765 --surface-addr 127.0.0.1:8766
sesh serve --tsnet
sesh reindex
sesh status
sesh admin drop-file <tool> <session_id> <file_uuid> --yes
```

`sesh status` reports cursor offsets, poisoned files, last ACK age, and store
reachability. It exits nonzero when the configured store is unreachable or any
cursor is poisoned.

`sesh admin drop-file` is an irreversible operator repair. It refuses to run
without `--yes`, removes exactly one mirrored file identity plus its index rows,
leaves sibling files in the same logical session intact, and records the action
in `drop_log`. Hard precondition: stop `sesh serve` before running `drop-file`;
the admin command is a separate process and does not quiesce live ingest or
queued append-index events.

`internal/surface` (U7) reads the frozen index schema through its `Store`
seam; `surface.SQLStore` satisfies it from the live store DB + mirror, and
`sesh serve` runs the surface on its own loopback read listener
(`--surface-addr`, default 127.0.0.1:8766 — the port the M2 Tailscale Serve
exposure proxies; ingest stays on `--addr`). The surface includes `/` recency,
`/s/{tool}/{id}` transcript pages, `/s/{tool}/{id}/raw` raw mirror fallback,
and `/nodes` last-PUT status. Gates: `tests/check-surface-fixtures.sh`
(fixture-backed renders) and `tests/check-surface-live.sh` (real serve + ship,
S2 renders once).

## M2 Exposure Runbook

Before M4 auth, keep ingest private to the local machine. The ingest listener
rejects non-loopback binds:

```sh
sesh serve --addr 127.0.0.1:8765 --surface-addr 127.0.0.1:8766
```

Expose only the read-only surface port with Tailscale Serve:

```sh
tailscale serve --bg --http=443 http://127.0.0.1:8766
```

Do not expose `127.0.0.1:8765`; it accepts transcript bytes. The read listener
serves the browser surface only, while ingest remains loopback-only until M4
auth and rollout gates land.

M2 exposure owner sign-off: PENDING (`@bigboss`).

## M4 tsnet Grant Runbook

M4 mode embeds tsnet in `sesh serve`. The store joins the tailnet as its own
node, authenticates each caller with WhoIs, stamps the authenticated user or
node identity into the fact log, and denies callers without the matching
Tailscale app-capability verb before ingest bytes or read handlers run.

```sh
TS_AUTHKEY=tskey-auth-... \
sesh serve --tsnet \
  --tsnet-hostname sesh-store \
  --addr :8765 \
  --surface-addr :8766
```

`--tsnet-dir` defaults to `<data-dir>/tsnet`; `--tsnet-auth-key` can be used
instead of `TS_AUTHKEY`. The hard grant is the Tailscale app capability
`sesh.dev/cap/store`; values grant one or both verbs:

- `{"verb":"ship"}` permits PUT ingest.
- `{"verb":"read"}` permits the read-only surface.

Tailscale ACLs should still limit which principals can reach the store node, but
the app capability is the application gate that can change without restarting
the store:

```json
{
  "tagOwners": {
    "tag:sesh-store": ["autogroup:admin"]
  },
  "grants": [
    {
      "src": ["group:sesh-shippers", "tag:sesh-ci"],
      "dst": ["tag:sesh-store"],
      "ip": ["tcp:8765"],
      "app": {
        "sesh.dev/cap/store": [{"verb": "ship"}]
      }
    },
    {
      "src": ["group:sesh-readers"],
      "dst": ["tag:sesh-store"],
      "ip": ["tcp:8766"],
      "app": {
        "sesh.dev/cap/store": [{"verb": "read"}]
      }
    }
  ]
}
```

WhoIs stamps user login names for user-owned nodes. Tagged or otherwise
login-less nodes stamp as the node name reported by WhoIs, usually the node's
MagicDNS name; grant CI nodes by tag in Tailscale, but expect the store fact log
to show node names rather than user emails for those clients.

## Rollout Runbook

The store URL is the only coupling between a node and the store (owner
ruling); everything below preserves that. Nothing in units or scripts assumes
where this repo lives.

### Order: store first, then nodes in any order

**1. Store up.** On the store host (any convenient host; it can move later):

```sh
GOOS=linux GOARCH=amd64 go build ./cmd/sesh   # or the matching platform
TS_AUTHKEY=tskey-auth-... sesh serve --tsnet --tsnet-hostname sesh-store \
  --addr :8765 --surface-addr :8766 --data-dir /var/lib/sesh
```

The store URL in tsnet mode is plain `http://` on purpose: the tailnet
itself WireGuard-encrypts and peer-authenticates every connection, so TLS
here would add certificate lifecycle without any confidentiality gain —
TLS termination lives in the M2 interim `tailscale serve` path, not in
tsnet mode. Using `https://` against the tsnet listener fails at transport.

**2. Grant applied.** Push the ACL/grant policy (M4 tsnet Grant Runbook
above) so `group:sesh-shippers` has `{"verb":"ship"}` and readers have
`{"verb":"read"}`.

**3. DENY VERIFIED — before any real transcript flows** (Lane-4 binding:
grant scope before content). The probe path must use syntactically valid
UUIDs: non-UUID segments answer 400 `malformed_request` before the grant
check runs, which proves nothing. From a tailnet device OUTSIDE the grant:

```sh
SID=$(uuidgen); FID=$(uuidgen)
curl -si -H 'X-Sesh-Wire-Version: 1' \
  "http://sesh-store.<tailnet>.ts.net:8765/v1/files/claude/$SID/$FID"
# REQUIRED: 403 out_of_grant (or connection refused by ACL). Anything else
# stops the rollout here.
```

From an in-grant device, the same GET must answer 404 `not_found` — that
pair (403 outside, 404 inside) is the deny evidence. Record both outputs in
the rollout log; the owner ratifies M4 against them.

**4. Nodes, any order.** Late nodes need no special handling: the shipper's
first pass is the same authoritative rescan as every later one, so a node
onboarded a week late backfills its full local history (up to Claude's
30-day retention) unaided. Per node, per shipping user:

```sh
# binary: place a matching-platform build at an absolute path
sudo install -m 0755 sesh /usr/local/bin/sesh
# service (Linux and macOS take the same command):
tools/sesh/etc/install-ship.sh --store-url http://sesh-store.<tailnet>.ts.net:8765
```

Linux reboot survival on nodes nobody logs into additionally requires
lingering — the installer warns when it is off:

```sh
loginctl enable-linger $USER
```

Shared multi-user node: run `install-ship.sh` once per OS user (each gets
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
   state dir carries the node key: the new host comes up AS `sesh-store`,
   same MagicDNS name, same grant.
4. Start `sesh serve --tsnet ...` on the new host with the same flags.
   Decommission the old host's copy (never run two stores on one identity).
5. Verify, touching NO node: shippers resume on their own (next pass or
   backoff retry); recovery GETs return the recorded high_waters; `/nodes`
   repopulates; one node's live session keeps appending end-to-end.
6. Loss check: byte-compare one mirrored file against its source on a node
   (`cmp`), and confirm `sesh status` is 0 fleet-wide.

If step 3 cannot carry the tsnet dir, re-auth the new host with the same
`--tsnet-hostname sesh-store`; the MagicDNS URL is preserved and nodes still
change nothing.

### Field failure signature: stale binary vs newer registry (R23)

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
