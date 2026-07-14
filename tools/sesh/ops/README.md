# sesh store ops — quick-host deployment

Deployment artifacts for the standing store host (the quick-host VM), shaped
after quick's `ops/` on the same machine. This directory is `ops/` rather
than `etc/` deliberately: in this module `etc/` means field-node installer
artifacts, while everything here is store-host machinery — and the layout
mirrors quick's on the VM the two services share.

Design authority: `docs/design/2026-07-12-sesh-store-served-distribution.md`
(§7 publishing, §8 exposure) and `backlog/docs/doc-002` §2b (hosting shape).

| File | Role |
|---|---|
| `bootstrap.sh` | idempotent VM bring-up: `sesh` user, `/var/lib/sesh` (+ `tsnet/`, `releases/`, `backup/db/`), unit install, TS_AUTHKEY first-start handoff |
| `systemd/sesh-serve.service` | system unit: `sesh serve --tsnet --tsnet-hostname sesh`, dedicated user, data dir `/var/lib/sesh` |
| `systemd/sesh-backup.{service,timer}` | 15-minute backup schedule (RPO 15 min), runs as `sesh` |
| `backup.sh` | sqlite snapshot + GCS rsync in recoverable ordering (below) |
| `deploy-remote.sh` | VM half of `just deploy-store`: crash-safe binary swap, restart, running-version print |

Coexistence rule (hosting shape §2b): **quickd, Caddy, and the VM's
tailscaled are untouched by construction.** The store embeds tsnet — its own
tailnet node (`sesh`), own identity state under `/var/lib/sesh/tsnet`, no
Caddyfile changes, no port negotiation. quickd restarts and sesh restarts are
independent. The backup is the same GCS machinery as quick's but a separate
script, timer, and bucket prefix — zero quick-owned files.

One deliberate divergence from quick's bootstrap: **no automatic restore
from GCS on an empty data dir.** An empty `/var/lib/sesh` is also the normal
first-boot state; restoring the store is the drill below, run on purpose.

## Owner execution runbook (end-to-end)

Prerequisites: `gcloud` authenticated with IAP access to the VM; the
tailnet-admin ask from the README ("The tailnet-admin ask") answered — one
`tag:sesh` grant plus one **reusable** tagged auth key.

1. **Admin key arrives.** Confirm it is reusable and carries `tag:sesh`.

2. **Bootstrap** (idempotent; safe to re-run any time):

   ```sh
   cd tools/sesh
   gcloud compute scp --recurse --tunnel-through-iap \
     --zone northamerica-south1-c --project prod-infinex-687a \
     ops quick-host:/tmp/sesh-ops
   gcloud compute ssh quick-host --tunnel-through-iap \
     --zone northamerica-south1-c --project prod-infinex-687a \
     --command 'sudo TS_AUTHKEY=tskey-auth-... sh /tmp/sesh-ops/bootstrap.sh'
   ```

   The key lands in `/etc/sesh/serve.env` (root-only) for the first service
   start; sesh-serve stays stopped until a binary is deployed.

3. **Deploy the store**: `just deploy-store`. Builds linux/amd64, ships via
   IAP, swaps the binary crash-safely, restarts, and prints the version of
   the **running** image. On the first start the store joins the tailnet as
   node `sesh`. Then:
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
   just release                       # defaults to sesh-host:/var/lib/sesh/releases
   ```

   Publishing needs the ssh alias and group membership below.

6. **Slack announcement.** Post the one-liner from the README install
   section: `curl http://sesh.<tailnet>.ts.net:8765/install.sh | sh`
   (plus the enable-linger note for headless Linux nodes).

7. **Restore drill, once,** soon after the first real transcripts land
   (below). Backup is not real until this has been performed.

## Publishing access (`just release` default dest)

`just release` defaults to `sesh-host:/var/lib/sesh/releases`. `sesh-host`
is an ssh alias for the VM over IAP — `scripts/release.sh` needs plain
`ssh`/`rsync`, not `gcloud compute ssh`:

```
# ~/.ssh/config
Host sesh-host
    HostName quick-host
    User <your VM login>
    IdentityFile ~/.ssh/google_compute_engine
    ProxyCommand gcloud compute start-iap-tunnel quick-host %p --listen-on-stdin --zone northamerica-south1-c --project prod-infinex-687a
    StrictHostKeyChecking accept-new
```

`accept-new` matters on the FIRST publish (first-rollout lesson): the VM's
host key is not in `known_hosts` yet, and release.sh drives ssh/rsync
non-interactively, so a default TOFU prompt stalls or aborts the publish.
`accept-new` pins the key on first contact and still refuses a later key
change (unlike `no`, which would accept silently).

(`gcloud compute config-ssh` generates the key pair if you have never
ssh'd to the VM.) On the VM, one-time, add your login to the `sesh` group so
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

`backup.sh` runs every 15 minutes as user `sesh` (RPO = 15 min). Bucket
layout, all under the sesh-only prefix:

```
gs://infinex-quick-backup/sesh/
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
2. Pull the data back:

   ```sh
   sudo gcloud storage rsync -r gs://infinex-quick-backup/sesh/mirror   /var/lib/sesh/mirror
   sudo gcloud storage rsync -r gs://infinex-quick-backup/sesh/tsnet    /var/lib/sesh/tsnet
   sudo gcloud storage rsync -r gs://infinex-quick-backup/sesh/releases /var/lib/sesh/releases
   sudo gcloud storage cp gs://infinex-quick-backup/sesh/db/store.sqlite /var/lib/sesh/store.sqlite
   sudo chown -R sesh:sesh /var/lib/sesh
   sudo chmod 700 /var/lib/sesh/tsnet
   ```

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

## Escape triggers (pre-agreed thresholds)

Hosting on quick-host is a threshold decision, not a permanent one
(`backlog/docs/doc-002` §2d). Any ONE of these fires the store host
migration drill (README, "Store host migration drill") to a dedicated node —
no re-litigating:

- **Disk/mirror growth**: mirror + db exceed 20 GB, or the shared disk
  passes 80% (the backup script warns at 80% on every run; disk-full 5xx's
  sesh ingest — safe, shippers hold — but could break quickd, which is
  worse).
- **quickd incident**: any security incident on quickd. The store shares its
  kernel with the most-exposed service on the tailnet; a VM compromise is a
  store compromise regardless of app-layer grants.
- **Team growth / availability**: the shipping fleet passes ~10 users, or
  sesh availability starts competing with quick deploys (store downtime
  blocks nothing — shippers hold — but repeated collisions signal the shared
  host is outgrown).

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
The **support window is current + previous release**: node version
visibility plus one-command `sesh update` keep the fleet within one release
of latest, and anything older gets upgraded, not accommodated.
