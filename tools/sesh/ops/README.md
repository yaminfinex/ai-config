# sesh store ops — store host deployment

Deployment artifacts for the standing store host. Since 2026-10-14 that is
`yamen-superset-f4-med-syd-1`, a Latitude bare-metal box in Sydney (SYD2;
32 cores, 187 GB RAM, data on the RAID1 NVMe root). It previously rode the
shared quick-host GCE VM (`prod-infinex-687a`, northamerica-south1); see
"Why the store left quick-host" below. This directory is `ops/` rather than
`etc/` deliberately: in this module `etc/` means field-node installer
artifacts, while everything here is store-host machinery.

Design authority: `docs/design/2026-07-12-sesh-store-served-distribution.md`
(§7 publishing, §8 exposure) and `backlog/docs/doc-002` §2b (hosting shape).

| File | Role |
|---|---|
| `bootstrap.sh` | idempotent host bring-up: `sesh` user, `/var/lib/sesh` (+ `tsnet/`, `releases/`, `backup/db/`), unit install, TS_AUTHKEY first-start handoff |
| `systemd/sesh-serve.service` | system unit: `sesh serve --tsnet --tsnet-hostname sesh`, dedicated user, data dir `/var/lib/sesh` |
| `systemd/sesh-backup.{service,timer}` | 15-minute backup schedule (RPO 15 min), runs as `sesh`; target + credentials from `/etc/sesh/backup.env` |
| `backup.sh` | sqlite snapshot + bucket upload (gs:// via gcloud, s3:// via rclone) in recoverable ordering (below) |
| `deploy-remote.sh` | host half of `just deploy-store`: crash-safe binary swap, restart, running-version print |

The store embeds tsnet: its own tailnet node (`sesh`), own identity state
under `/var/lib/sesh/tsnet`, independent of the host's tailscaled. Restarts
of anything else on the host and sesh restarts are independent.

Per-host settings live outside the repo, in two root-only places:

- `/etc/systemd/system/sesh-serve.service.d/host.conf` — memory caps sized to
  the host (the base unit's caps were sized for the 4 GB VM). On the
  Latitude host: `GOMEMLIMIT=4GiB`, `MemoryHigh=6G`, `MemoryMax=8G`.
- `/etc/sesh/backup.env` (0600) — backup target and credentials:

  ```
  SESH_BACKUP_BUCKET=s3://sesh-backup/sesh
  SESH_S3_ENDPOINT=https://s3.ap-southeast-2.storage.sh
  AWS_ACCESS_KEY_ID=...
  AWS_SECRET_ACCESS_KEY=...
  ```

  The bucket is Latitude object storage `sesh-backup` (site SYD, versioning
  on; SYD2 has no object storage, so backups sit in a different facility
  from the host). The key is a Limited, read-write key on that bucket only,
  created in the Latitude dashboard (Object storage → Access keys); its
  secret is shown once. Never paste it into an agent chat: transcripts are
  mirrored into this very store. Without the file, backup.sh falls back to
  the GCS default, which only works on GCE.

One deliberate divergence from quick's bootstrap: **no automatic restore
from a bucket on an empty data dir.** An empty `/var/lib/sesh` is also the
normal first-boot state; restoring the store is the drill below, run on
purpose.

## Owner execution runbook (end-to-end)

Prerequisites: ssh + sudo on the store host; for a brand-new tailnet
identity, the tailnet-admin ask from the README ("The tailnet-admin ask")
answered — one `tag:sesh` grant plus one **reusable** tagged auth key. A
host taking over an existing identity needs no key (migration drill, README).

1. **Admin key arrives** (new identity only). Confirm it is reusable and
   carries `tag:sesh`.

2. **Bootstrap** (idempotent; safe to re-run any time). Write
   `/etc/sesh/backup.env` first so bootstrap picks the right upload tool
   (rclone for s3://, gcloud otherwise). On the store host:

   ```sh
   sudo TS_AUTHKEY=tskey-auth-... sh tools/sesh/ops/bootstrap.sh   # key: new identity only
   ```

   The key lands in `/etc/sesh/serve.env` (root-only) for the first service
   start; sesh-serve stays stopped until a binary is deployed.

3. **Deploy the store**: `just deploy-store`. Builds linux/amd64 from
   `./cmd/sesh-store` (the FULL build — the release channel publishes only
   the slim client, which cannot serve), ships it with
   `scripts/deploy-store.sh` (in place when run on the store host, else
   scp + ssh to `SESH_STORE_HOST`, default
   `ubuntu@yamen-superset-f4-med-syd-1`), swaps the binary crash-safely,
   restarts, and prints the version of the **running** image. On a first
   start with a new key the store joins the tailnet as node `sesh`. Then:
   - ask the admin to disable key expiry on the node (item 3 of the ask);
   - scrub the now-inert key:
     `sudo sed -i '/^TS_AUTHKEY=/d' /etc/sesh/serve.env`.

4. **Deny-verify pair — before any real transcript flows.** Run the README's
   Rollout Runbook step 3 exactly as written there (403/refusal from an
   out-of-grant tagged device, 404 from an in-grant device, both with
   syntactically valid UUID probes) and record both outputs in the rollout
   log. Anything else stops the rollout.

5. **First tagged release**:

   ```sh
   just tag 0.1.0                     # creates monorepo-prefixed tag sesh-v0.1.0
   git push origin sesh-v0.1.0
   just release                       # defaults to the store host's /var/lib/sesh/releases
   ```

   Publishing needs the group membership below.

6. **Slack announcement.** Post the one-liner from the README install
   section: `curl http://sesh.<tailnet>.ts.net:8765/install.sh | sh`
   (plus the enable-linger note for headless Linux nodes).

7. **Restore drill, once,** soon after the first real transcripts land
   (below). Backup is not real until this has been performed.

## Publishing access (`just release` default dest)

`just release` publishes to `/var/lib/sesh/releases` on the store host: a
local path when run there, `$SESH_STORE_HOST:/var/lib/sesh/releases` over
plain ssh/rsync from anywhere else (`SESH_RELEASE_DEST` overrides both).
From another machine, make sure `ssh $SESH_STORE_HOST` works
non-interactively first — release.sh drives ssh/rsync without a terminal,
so an unanswered host-key prompt stalls or aborts the publish
(`StrictHostKeyChecking accept-new` in `~/.ssh/config` pins the key on
first contact and still refuses a later change).

On the store host, one-time, add the publishing login to the `sesh` group so
the publish path needs no sudo — `releases/` is group-writable with setgid:

```sh
sudo usermod -aG sesh $USER
```

**Quoting hazard (field bug, first live publish).** Every command
`scripts/release.sh` sends over ssh crosses one extra shell parse on the
remote side. An earlier `sh -c '...'` wrapper added a second quoting layer;
the command's own single quotes then split the wrapper's, an embedded
`printf '%s\n'` lost its backslash, and the store served `latest` as
`sesh-v0.1.0n` — installers built a 404 URL from it. The hazard is a class,
not a one-off: any backslash escape or nested quoting in a remotely executed
command string is at the mercy of however many shell parses sit between
writer and executor. The script now sends command strings through exactly
one remote parse, keeps them free of backslash sequences, and moves byte
payloads (the `latest` contents) over stdin; the release gate
(`tests/check-release-publish.sh`) replays the ssh path through a shim, and
both consumers (`install.sh`, `sesh update`) refuse any version string that
fails the release shape instead of 404ing. Keep all four in lockstep when
touching the publish path.

## Backup and restore

`backup.sh` runs every 15 minutes as user `sesh` (RPO = 15 min), uploading
copy-only (never deleting remote objects) to `SESH_BACKUP_BUCKET` from
`/etc/sesh/backup.env`. Bucket layout, all under the sesh-only prefix:

```
s3://sesh-backup/sesh/
  db/store.sqlite   VACUUM INTO snapshot — the live file is never copied
  mirror/           append-only transcript mirror (source of truth)
  tsnet/            tailnet identity — restoring it brings the node back AS
                    `sesh`, same URL, zero shipper changes
  releases/         release channel (rebuildable by re-publish; rides along)
```

Ordering is load-bearing and enforced by the script: snapshot the db first,
upload the mirror **before** the snapshot. Because mirror files are
append-only, the uploaded mirror is always at least as new as the snapshot's
high-water claims, so the bucket can never hold a db that claims bytes the
mirror lacks — the one combination that would make a restore lie to shippers
(ACK'd bytes silently gone, which the wire never re-sends). The benign
inverse (older db, newer mirror) restores cleanly: surplus mirror bytes heal
via `sesh reindex` and the wire's PUT overlap comparison.

The script also warns (journal, every run) when the disk holding the data
dir passes 80% — see the escape triggers.

### Restore drill (step-by-step)

Run once after first real data, then for any real recovery. On the target
host (fresh host: run `bootstrap.sh` first; sesh-serve stays stopped until a
binary is deployed):

1. Stop the store: `sudo systemctl stop sesh-serve`. Shippers hold position
   by design — cursors untouched, source files remain the buffer; nothing is
   lost while the store is dark.
2. Pull the data back, with the same credentials the backup uses (put
   `/etc/sesh/backup.env` on the target host first):

   ```sh
   sudo sh -ac '. /etc/sesh/backup.env
     r() { rclone copy --s3-provider Other --s3-env-auth --s3-no-check-bucket \
             --s3-endpoint "$SESH_S3_ENDPOINT" ":s3:${SESH_BACKUP_BUCKET#s3://}/$1" "/var/lib/sesh/$2"; }
     r mirror mirror; r tsnet tsnet; r releases releases; r db .'
   sudo chown -R sesh:sesh /var/lib/sesh
   sudo chmod 700 /var/lib/sesh/tsnet
   ```

   (`db/` holds only `store.sqlite`, so `r db .` lands it at
   `/var/lib/sesh/store.sqlite`.) For a GCS target the equivalent is
   `gcloud storage rsync -r gs://.../<dir> /var/lib/sesh/<dir>` per
   directory and `gcloud storage cp` for the db.

   Restore order does not matter: the backup-time invariant (db never newer
   than mirror) is what makes the restored pair safe.
3. Reindex before serving, so index rows match the restored mirror exactly
   (the snapshot may trail the mirror by up to one backup interval):
   `sudo -u sesh /usr/local/bin/sesh reindex --data-dir /var/lib/sesh`
4. Start: `sudo systemctl start sesh-serve`. The tsnet state brings the node
   up as `sesh` — same MagicDNS name, same grant, no node changes.
5. Verify, touching NO fleet node:
   - recovery GETs for a few known identities return high_waters at or below
     the matching mirror file sizes (same verification shape as the README's
     store host migration drill);
   - byte-compare one mirrored file against its source on a node (`cmp`);
   - shippers resume on their own: the nodes view at `/` shows fresh last-PUT times and
     `sesh status` exits 0 on a spot-checked node;
   - one live session keeps appending end-to-end.
6. Record the drill (date, identities verified, any gaps) in the rollout log.

Note the RPO honesty: transcripts PUT within the last backup interval may be
absent from the restored store. That is not loss — shippers hold the source
bytes and re-converge via recovery GETs + resumed PUTs as soon as the store
is back.

## Memory ceiling and profiling

`sesh-serve.service` caps the store at `MemoryMax=1800M` (with
`MemoryHigh=1500M` and `GOMEMLIMIT=1200MiB` under it), sized for the old
4 GB VM; the Latitude host raises them in its `host.conf` drop-in (above).
Past the cap the kernel kills only sesh and `Restart=always` brings it back,
instead of a global OOM taking the host's networking with it (2026-10-02:
an uncapped sesh at 3.4 GB left quick-host off the network for six days).
`journalctl -u sesh-serve | grep -i oom` shows whether the cap has fired.

To see what the heap holds, profile on the store host; pprof listens on loopback
only, never the tailnet:

```sh
curl -s localhost:8767/debug/pprof/heap > heap.pb   # on the store host
go tool pprof -top heap.pb                          # anywhere with the binary's source
```

## Why the store left quick-host

Hosting on quick-host was always a threshold decision (`backlog/docs/doc-002`
§2d). On 2026-10-14 it moved to the Latitude host via the README's store
host migration drill (tsnet identity carried, URL unchanged, no shipper
touched), for three measured reasons:

- **Distance.** Every node shipping to the store is in or near Sydney; the
  VM was in Mexico (~180 ms round trip), which was about 0.4 s of every
  0.6 s page load.
- **Disk.** pd-standard on a 4 GB VM: pages not already in the small page
  cache took 1–3 s on first view.
- **Backups never ran.** The VM's target `gs://infinex-quick-backup` did not
  exist, so every run since 2026-07-13 had failed and there was no off-site
  copy. The new host backs up to Latitude object storage (above).

The old VM's `/var/lib/sesh` was left in place, stopped and disabled, as a
rollback copy. Never start it while the new host runs: two stores on one
tsnet identity fight over the node.

## Escape triggers (pre-agreed thresholds)

Any ONE of these fires the store host migration drill (README, "Store host
migration drill") to another host — no re-litigating:

- **Disk/mirror growth**: the disk holding `/var/lib/sesh` passes 80% (the
  backup script warns at 80% on every run; disk-full 5xx's ingest — safe,
  shippers hold — but the host is shared with other work).
- **Host incident**: any security incident on the host. The store shares
  its kernel with everything else running there; a host compromise is a
  store compromise regardless of app-layer grants.
- **Team growth / availability**: the shipping fleet passes ~10 users, or
  store availability starts competing with other uses of the host (store
  downtime blocks nothing — shippers hold — but repeated collisions signal
  the shared host is outgrown).

Moving is cheap by design: the tsnet state dir IS the identity; the drill
carries it, the URL survives, and no shipper changes anything.

## Version-skew policy

Binding, in one paragraph: Upgrades are **store-first** — deploy the store
(`just deploy-store`) before publishing the release fleet nodes will
converge to (`just release`), so no shipper ever runs ahead of the store it
ships to. **Wire v2 is a fleet event**: wire v1 is frozen, and any
wire-version bump is planned, announced, and rolled out store-first as its
own migration — never an incidental side effect of a deploy. **Schema moves
forward-only**: the store's index schema and the node-local cursor-registry
generation never downgrade; an older binary against newer state refuses
cleanly (the README's "Field failure signature") instead of touching data.
The **support window is current + previous release**: one-command
`sesh update` keeps the fleet within one release of latest, and anything
older gets upgraded, not accommodated. The store binary itself is NOT
updated this way: the channel serves the slim client artifacts, so the
store build's `sesh update` fails closed before any download (one line
naming `just deploy-store`; `update --check` stays available as a read-only
skew probe). Belt-and-braces if that guard ever regresses: `sesh.prev` is
retained on the host and `just deploy-store` restores the full build. The
store converges by `just deploy-store` only.

Per-node version visibility (the User-Agent census): each shipper
self-reports its build version on every PUT (`User-Agent:
sesh-ship/<version>`, informational only — never required, never load-bearing),
the store records the last-seen version per node in `last_seen` bookkeeping,
and the nodes view renders it as a **Version** column. The support window is
anchored to the running store's own build version — current plus the previous
patch release; after a minor/major bump only current is in-window until the
fleet converges.

What the flags mean, and what to do:

- **`out of window`** — the node's shipper is older than the previous
  release. Run `sesh update` on that node (or wait for its self-update timer
  if one is installed); the flag clears on its next PUT.
- **`unknown`** — the node has never reported a parseable version: a shipper
  predating the census, a dev/untagged build, or a row written before the
  census column existed. Treat it like out-of-window: `sesh update` the node,
  after which it self-reports. A store running a dev/untagged build cannot
  anchor the window and flags nobody out-of-window (versions still display).

The census is bookkeeping, not contract: absence of the header never blocks
shipping, and version facts never appear in journal logs (identifier-free
logging holds — versions are aggregate-safe, node identities are not).
