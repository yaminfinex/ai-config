---
id: doc-002
title: sesh distribution and runtime options memo
type: other
created_date: '2026-07-12 01:30'
---

# sesh — distribution and runtime options memo (TASK-155)

Research memo, not an exercised decision. Two independent researchers (one codex,
one fable) surveyed the space without reading each other; this synthesis preserves
their disagreements. Frozen constraints treated as binding throughout: wire v1
(docs/specs/sesh-wire.md), ACK durability, R23 stale-registry refusal, drop-in
preservation, user-owned per-user shipper (no sudo), I1–I11, store-URL-only
node→store coupling.

Owner steers taken as recommendation constraints (not survey constraints):
internal-only distribution; the desired UX is "announce on Slack → visit a URL →
grab binary → install"; "store URL changed → install again" as the recovery story;
version upgrades like quick's (`quick update` against a latest pointer); store
probably on the quick-host VM short-term.

## Facts that shape everything

- **sesh is pure Go** (modernc sqlite, tsnet): `CGO_ENABLED=0` cross-compilation of
  the darwin/linux × arm64/amd64 matrix works exactly like quick's
  `scripts/build.sh`. No build obstacle to prebuilt distribution.
- **sesh is a service-carrying binary; quick is a plain CLI.** quick's install.sh
  ends at "binary on PATH". A sesh node is not onboarded until a unit is rendered
  with an absolute binary path, the store URL sits in a preserved drop-in, the
  service is enabled/started, and (headless Linux) linger is on. Every distribution
  option is really *binary delivery* + *service bootstrap*, decidable
  semi-independently. quick's installer is therefore reusable as a pattern, not as
  a script.
- **The binary is large** (~48 MB stripped; 4-target matrix ~190 MB raw). This only
  bites the quick-*site* option (128 MiB gzipped deploy cap — workable if each
  deploy carries one release).
- **Store restarts are cheap by contract**: shippers hold position on an
  unreachable store (jittered backoff, cursor untouched, source file is the
  buffer). Store deploy risk is low; the mirror data dir is the only precious
  thing.
- **The store's tailnet identity IS its URL** in tsnet mode (the tsnet state dir
  carries the node key). The README migration drill (copy data dir incl. tsnet/,
  start elsewhere) makes every hosting decision below reversible with zero node
  changes — the strongest single fact in this survey.
- **The durable dataset is mirror + bookkeeping, not just SQLite.** The mirror is
  the evidence (I2/I7), grows unboundedly by design; the index is rebuildable but
  mirror generations and ACK high-waters are not. Transcripts contain pasted
  secrets (spec §4.3) — the store is the secret-densest service in the fleet.

---

## Axis 1 — Client (shipper) distribution

### 1a. Status quo: repo + Go toolchain + just on every node

Exists today; git-stamped versions; `just versions` staleness check. But every
node needs the private repo (git auth), Go ≥1.26, just, and a human comfortable
with all three; upgrades are `git pull && just restart` × N by hand. Fails the
no-repo/no-toolchain/nobody-logged-in node class outright, and the Slack-announce
UX is unreachable. Keep for development and emergency diagnosis; not fleet
distribution. Trial cost: zero (it's the present).

### 1b. Prebuilt artifacts via quick's channel — two genuinely different sub-options

**1b-i. quickd's own /releases mechanism, namespaced.** Extend `/srv/releases`
with `/releases/sesh/<ver>/sesh-<os>-<arch>` and an *independent*
`/releases/sesh/latest` pointer (do not overload quick's single latest — the two
tools have independent cadences and rollback). Concretely this takes: quickd's
release router currently expects exactly two path segments and quick-named assets,
and its install.sh is an embedded, quick-specific asset (installs a file literally
named `quick`, no unit rendering) — so either quickd code changes (generic
artifact namespaces + a second installer) or a small static Caddy path on the VM.
Publishing needs checksums (SHA-256 manifest) and ideally a signing story;
unsigned tailnet delivery rests entirely on host/tailnet integrity.
*Pros:* one operated host, proven channel, natural substrate for `sesh update`.
*Cons:* couples sesh's release cadence to quickd deploys; quickd grows a generic
package-server concern; sesh nodes gain an install-time dependency on quick's
apex. *Trial:* publish one namespaced version without touching existing clients;
reversible but touches a second product's server.

**1b-ii. A quick site (e.g. `sesh.quick.infinex.xyz`) carrying static binaries +
static install.sh.** No quickd changes: `quick deploy` a directory with
`install.sh`, `latest`, and per-version binaries. Anyone on the team can publish.
*Pros:* zero new server code anywhere; the Slack-URL UX exactly; publishable this
afternoon; trivially reversible (delete the site). *Cons:* the static installer
can't interpolate its base URL (hardcode the stable site URL — fine) and doesn't
know the store URL, so onboarding stays two-step (install, then configure with the
URL from the Slack message); the 128 MiB deploy cap means one release per deploy;
no server-side latest semantics beyond a file you overwrite. *Trial:* an
afternoon, fully reversible. **Best zero-code interim.**

### 1c. Non-obvious: store-served distribution — `sesh serve` grows quick-§11.4 endpoints

The store itself serves `/install.sh`, `/releases/latest/VERSION`,
`/releases/<ver>/sesh-<os>-<arch>` from a releases dir (≈180 lines of apex.go +
a 50-line installer, copied from a working in-house reference). Unique property:
**the distribution URL and the store URL are the same URL.**
`curl http://sesh-store…:8765/install.sh | sh` ends with the installer invoking
`sesh setup --store-url $BASE` — the installer learns the store URL for free, so
onboarding is genuinely one command, and the owner's "store URL changed → install
again" recovery story becomes literally correct. URL-only coupling is not just
preserved but strengthened: install-time and runtime coupling collapse into one
URL, and `sesh update` gets its base URL from the SESH_STORE_URL already in the
drop-in — no new config anywhere.
*Cons:* adds operator surface to the service guarding transcripts (mitigate:
serve from the read surface, grant-gated in tsnet mode); distribution is down
when the store is down (acceptable — shippers hold position during outages and
nobody onboards during one); needs a one-line informational note in the wire doc
(no shipper behavior switches on these endpoints, so no amendment). *Trial:* ~1
day of Go; reversible (delete the routes); nothing frozen is touched.

### 1d. Self-installing binary: `sesh setup` subcommand absorbing install-ship.sh

Port the shell installer to Go: embed unit/plist templates, render the absolute
binary path, write the store-URL drop-in with the same refuse-unless-`--force`
preservation rule, preflight the user bus before any write, warn on missing
linger, `--dry-run`. Orthogonal to how the binary arrives; composes with 1b/1c;
pays off under every option. Makes installer behavior unit-testable (the shell
script isn't). Keep `just deploy` delegating to it so repo and field paths
exercise identical code. It cannot hide the sudo boundary: headless Linux still
needs a one-time `loginctl enable-linger` (admin/polkit on some distros), and the
installer must keep refusing honestly when the user bus is unreachable. *Trial:*
1–2 days incl. porting the preservation/preflight tests; the script can remain
during one migration release with dry-run parity checks.

### 1e. `sesh update` self-update (quick's cmd_update.go pattern)

Fetch `<base>/releases/latest/VERSION`, compare to buildinfo, download the
matching asset, atomic-rename over the running executable — then, the part quick
doesn't need: **restart the unit and verify the running image reports the new
version**. Updating bytes without restarting is not success, and a replaced
binary under a still-running old process is operator confusion waiting to happen.
`--check` mode for scripting. *Trial:* ~1 day; depends on a release channel
(1b/1c) existing.

### 1f. `go install` by module path — reject

The module is named `sesh` inside a private repo: a fetchable path, GOPRIVATE +
git auth per node, and the toolchain everywhere — and VCS stamping of a
subdirectory module is unreliable, so versions regress to `(devel)`, colliding
with the staleness story. Solves only binary delivery, not service bootstrap.
Status-quo costs without status-quo version stamping.

### 1g. mise pinning — poor fit for a service binary

mise's HTTP-backend tools could pin `sesh = "vX"` per node: versions become
diffable config, `mise upgrade` is the verb. But mise buries the binary
mise-deep, and the unit pins an absolute path — every pin bump changes the path
and forces a unit re-render, strictly worse than 1e's stable-path atomic replace.
Suited to CLIs, not service-carrying binaries. Also doesn't touch service
bootstrap.

### 1h. OS packages / nix / home-manager — defer

deb/rpm installs are privileged and can't know which OS users should ship or
preserve their drop-ins; Homebrew's versioned Cellar paths fight pinned absolute
ExecStart. nix/home-manager is the one ecosystem that declaratively unifies
binary *and* systemd user unit — genuinely elegant, but it imposes a second
toolchain on a ~4-person fleet. Revisit only if the fleet becomes cattle.

### Non-obvious hardening for whichever installer wins: immutable version dirs

Install each release at `~/.local/lib/sesh/<ver>/sesh` with a stable
`~/.local/bin/sesh` symlink; keep one previous version for instant rollback;
switch transactionally. Makes interrupted downloads and rollback much safer than
overwriting the running path. Caveat that must be tested before claiming
rollback works: a newer binary may have migrated the cursor registry, and R23
will (correctly) refuse the rolled-back binary.

---

## Axis 2 — Backend hosting (`sesh serve`)

### 2a. Dev machine (status quo)

Zero provisioning, already proven — and availability tied to one workstation,
backups are whatever the workstation does, the machine producing the most
transcripts also holds them (correlated loss), and every teammate's transcript
bytes flow to a personal machine (wrong trust shape even at this size). Suitable
only until field rollout. Migration later is the documented drill.

### 2b. quick-host VM alongside quickd (owner's short-term lean)

Concrete shape: a **system** service `sesh-serve.service` under a dedicated
`sesh` OS user, data dir `/var/lib/sesh` (mirror + store.sqlite + tsnet/),
running `sesh serve --tsnet`. sudo here is not barred: the no-sudo rule scopes to
per-user components on fleet nodes; the store is an operator-owned central
service, same class as quickd.

- **tsnet makes co-location clean**: the store joins the tailnet as its own node
  (`sesh-store`) — zero Caddyfile changes, zero port negotiation, zero shared
  identity with `quick-host`; the store URL never mentions the VM. Moving off
  later = the migration drill; shippers never notice.
- **Backup**: quick's 15-min GCS timer (`ops/backup.sh` → versioned bucket)
  extends naturally — add `/var/lib/sesh` with a separate prefix/bucket, SQLite
  via snapshot/backup API (never naive file copy of a live DB), and mirror files
  copied in an ordering that yields a recoverable point. Backup is real only
  after a restore drill: recover, verify recorded high-waters, byte-compare
  samples, reindex, resume a shipper.
- **Blast radius (the honest cons, bidirectional)**: quickd is
  whole-tailnet-reachable and executes user-supplied JS; co-locating puts the
  secret-densest service on the same kernel as the most-exposed one — a VM
  compromise is a store compromise regardless of app-layer grants. And the
  unbounded mirror now shares a disk with `/srv`: disk-full would 5xx sesh ingest
  (safe — shippers hold) but could break quickd (worse). Capacity thresholds or
  a separate disk belong in the first deployment, not later.
- **Trial cost:** small (unit + user + dir + deny-verify + backup line);
  reversible via the drill.

### 2c. Dedicated tailnet node

Clean blast-radius isolation, disk sized for the mirror alone, availability
decoupled from quick deploys. Costs one more machine to patch/monitor/back
up/pay for; at current team size the overhead buys little that grants + a later
migration don't. The clean long-term target when mirror size, an incident, or
availability competes with quick.

### 2d. Non-obvious: threshold-triggered escape — "grow into" hosting

Because the tsnet state dir is the identity, start on quick-host (2b) **with the
escape pre-agreed in writing**: trigger conditions (mirror > X GB, first security
incident on quickd, team > N people) fire the documented migration drill to a
dedicated node. Converts the 2b-vs-2c debate from a decision into a threshold —
honest about the fact that the drill exists precisely so this needn't be decided
now.

Also surveyed and rejected: **object storage as the primary store** (GCS/S3
chunks + separate index) — removes local data gravity but is an architectural
rewrite: frozen PUT overlap comparison, fsync-like durable ACK, generations, and
byte-range recovery don't map onto object semantics. Dead on arrival for v1; use
object storage for backup/archive only. **Managed container/serverless** — poor
fit for persistent mirror + SQLite + tsnet state + unbounded disk; adds layers
without reducing ownership. **NAS/home hardware** — worse availability and
backup posture than the dev machine.

### Exposure modes and the M4 tailnet-auth milestone

- **Interim Tailscale Serve** (loopback bind, expose read surface only): follows
  the current runbook but cannot expose ingest to nodes, so it cannot be the
  fleet ingest endpoint before M4. On quick-host specifically it is also
  awkward — the VM's tailscaled already serves quick's identity, so sesh would
  ride the `quick-host` node name. On that host, skip the interim mode and go
  straight to tsnet.
- **tsnet in `sesh serve`**: the best match, and exactly the M4 mechanism —
  stable store identity, WhoIs, app-capability grants separating ship/read
  verbs. Preserve the tsnet state dir in backup/migration or re-auth the same
  hostname.
- **Loopback behind Caddy/quickd**: operationally familiar, but tailnet
  reachability is not application authorization; a proxy must carry a
  connection-bound trusted identity or enforce grants itself (client-supplied
  headers are not acceptable), and it couples sesh auth to quick internals.
- **Non-obvious: host Tailscale-IP listener + firewall/grants via LocalAPI**:
  simpler than tsnet but lacks WhoIs/app-capability enforcement unless M4
  explicitly implements that mode and proves deny-before-body.

Whatever the host: gate real transcript flow on M4's deny-verification pair
(out-of-grant valid-UUID probe → 403/refusal; in-grant → 404). Hosting on
quick-host does not waive that gate.

---

## Axis 3 — Store deploy/upgrade

### 3a. quick's deploy-server shape: scp + sudo install + systemctl restart

`just deploy-store`: cross-build linux/amd64, scp to the VM, `sudo install` +
`sudo systemctl restart sesh-serve`, then probe the running version. Proven,
deliberate (a human runs it), and cheaper for sesh than for quick: shippers hold
position by contract, so restart timing is a non-decision. Graceful shutdown
(SIGTERM drains without false ACKs) is the one prerequisite quickd doesn't
share. Keep release-publishing and server-restart as separate verbs, as quick
deliberately does. Rollback = redeploy an old build, subject to the schema rule
below. *Trial:* an hour of justfile.

### 3b. Versioned install dirs + `current` symlink

`/opt/sesh/releases/<ver>/` + atomic `current` switch; instant offline rollback;
more auditable than overwriting `/usr/local/bin`. One extra concept — and it
makes rollback easy enough that someone might do it casually across a schema
migration, which is the actual trap (see policy). Worth adopting only with the
schema rule stated first.

### 3c. Non-obvious options surveyed and rejected

**Socket-activation / blue-green**: two store processes must never concurrently
own the same SQLite/mirror/tsnet state, so blue-green needs quiescing + exclusive
handover and buys little over a brief restart that clients already tolerate.
**Container image**: pins runtime, but adds registry/volume/tsnet-state/backup
complexity; only sensible if quick-host standardizes on containers generally.
**Store self-update from its own channel** (dogfooding 1c): self-referential
with no real win over scp — the operator is already on the box. **Auto-pull
timers**: save one command a month and cost the deliberateness quick's
release≠deploy split encodes.

### Version-skew policy: what's guaranteed vs what needs writing

Already guaranteed by frozen wire v1 (no policy needed): version header checked
on every call, mismatch is a non-advancing 400 (failstop, not corruption);
servers must not require fields v1 clients don't send and clients ignore unknown
response fields — so **store-newer-than-fleet is safe indefinitely** and
fleet-newer-than-store is safe within v1; `unknown_tool` holds and surfaces;
unreachable/503 holds cursors with the source as buffer; replay is idempotent.
R23 protects node-local binary-vs-registry skew only — it is not a fleet
mechanism.

Needs an actual written policy (a paragraph, not machinery):

1. **Store-first order** for any release touching both sides; canary one shipper
   before announcing fleet-wide.
2. **Wire v2, if ever, is a fleet event** — no v2 store until every shipper
   understands v2 (exactly what the version header makes checkable).
3. **Store schema is forward-only**; binary rollback is permitted only within
   the same schema, and irreversible migrations require a pre-upgrade snapshot.
4. Support window: current + immediately previous shipper release; contract
   changes require a frozen-wire amendment.

---

## Axis 4 — Fleet operations / onboarding

### New node end-to-end (fresh, no-repo, no-toolchain, nobody-logged-in Linux)

| Path | Steps |
|---|---|
| 1a status quo | install git+go+just, clone private repo (auth!), `just install`, `just deploy <url>`, enable-linger — five tool installs before sesh exists; fails this node class in practice |
| 1b-ii quick site | `curl https://sesh.quick.infinex.xyz/install.sh \| sh`, then `sesh setup --store-url <url from Slack>`, enable-linger — two URLs in play |
| 1c store-served | `curl http://sesh-store…:8765/install.sh \| sh` (installer runs `sesh setup --store-url $BASE` itself) + enable-linger — one command, one URL |

In every path the headless-Linux sudo boundary is real and cannot honestly be
hidden: `loginctl enable-linger` once per node (admin/polkit on some distros),
then an installer run in a session with the user bus available; the existing
refuse-before-write preflight must survive the port to `sesh setup`. macOS
LaunchAgents are login-session-bound by nature; a truly headless Mac would need a
root LaunchDaemon model, which conflicts with the frozen user-owned constraint —
out of scope.

### Upgrade story — who restarts N shippers

- **Announce + `sesh update`** (self-restarting): owner Slacks once, each node
  runs one command (or the owner runs it over ssh for nodes they own).
  Partial-fleet skew during the window is contractually safe, so a lazy fleet is
  an annoyance, not a hazard.
- **Auto-update** (shipper polls latest, replaces own binary, exits and lets
  Restart= bring up the new build): fully unattended convergence — and a bad
  release auto-propagates fleet-wide before anyone notices, the shipper gains
  write-to-own-binary capability (today its threat surface is read-only on
  sources), and silent upgrades fight the deliberate-release culture. Wrong
  default now; viable later for headless nodes nobody wants to touch.
- **Central push (SSH/MDM/Ansible)**: proves convergence and can manage linger,
  but adds node inventory/credentials against the spirit of a low-dependency
  shipper. Only if manual pull measurably fails.
- **R23 as upgrade signal**: it isn't one — it fires only after a newer binary
  already wrote the registry on that node. Backstop, never discovery.

### Staleness noticing (fleet-wide)

- **Non-obvious, zero wire change**: shipper sends `User-Agent:
  sesh-ship/<version>` — not a frozen `X-Sesh-*` header, no store behavior
  switches on it; the store records it per node and `/nodes` grows a version
  column. One glance = whole-fleet version census against the latest pointer.
  Hygiene: an explicitly *informational-only* note in the wire doc (never
  routed/authed/stored-on), keeping the note trivial rather than semantic.
- `sesh status --check` against the release channel: staleness visible on the
  stale node, in the command operators already run.
- Owner's rollout loop becomes: publish → Slack → watch `/nodes` converge →
  chase stragglers. `/nodes` last-PUT identifies silence; the version column
  identifies build skew; they are different failures and both stay visible.

### Keeping URL-only coupling honest

Store-served distribution (1c) collapses install-time and runtime coupling into
the single URL. A quick site (1b-ii) adds a second, install-time-only URL —
legal under the ruling (runtime coupling stays SESH_STORE_URL only) but it should
be named for what it is so nobody later threads runtime config through the
distribution site. Nothing surveyed needs any config beyond the store URL;
`sesh setup` flags are install-time inputs; the drop-in remains the single
runtime config surface with its preservation rule unchanged.

---

# Recommendations (synthesis — separated from the survey; owner decides)

Both researchers independently converged on: prebuilt binaries + a
self-installing `sesh setup` + a `sesh update` self-updater; store on quick-host
now with strict isolation and a pre-agreed escape; quick's two-verb deploy shape
with a short written skew policy; pull upgrades + a `/nodes` version column.

**The one genuine disagreement is the artifact channel.** Codex-researcher
recommends namespaced artifacts on quick-host's existing release path
(1b-i-style, needing quickd or Caddy work); fable-researcher recommends the
store serving its own releases (1c), with a quick site as zero-code interim.
Synthesis view: 1c is the stronger end state for sesh specifically — one URL for
everything, one-command onboarding, "URL changed → install again" literally true,
no second product modified — while a quick site (1b-ii) is the right interim the
moment the owner wants to announce something on Slack before 1c lands. The
sequence below assumes that resolution; if the owner prefers not to grow any
distribution surface on the store, 1b-i is the fallback and only R2 changes.

Recommended sequence (each step independently shippable and reversible):

1. **R1 — `sesh setup`** (self-installing binary; pays off under every channel).
2. **R2 — release channel + `sesh update`** (store-served; quick site interim if
   needed sooner).
3. **R3 — store on quick-host** (dedicated user/unit/data-root, tsnet from day
   one, backup rider + restore drill, deny-verify gate, written escape
   triggers, `just deploy-store`, skew-policy paragraph).
4. **R4 — fleet version visibility** (`/nodes` version column via User-Agent +
   `sesh status --check`).

## Filed-ready follow-up tasks

**T1 — sesh: `sesh setup` self-installing subcommand (absorb install-ship.sh)**
Move service installation into Go: embed the systemd unit + launchd plist
templates, render the absolute binary path and store-URL drop-in, enable/start
the service. Keep `just deploy` delegating to it so repo and field paths share
one code path.
*AC:* (1) `sesh setup --store-url URL [--force] [--dry-run]` reproduces
install-ship.sh behavior on Linux and macOS, including user-bus preflight before
any write, linger warning, and refuse-to-clobber-differing-drop-in without
`--force`; (2) unit tests cover render, preservation refusal, dry-run, and
preflight ordering; (3) `just deploy` calls `sesh setup`; install-ship.sh deleted
or reduced to a deprecation pointer after one release of dry-run parity; (4)
README rollout runbook updated; no wire/spec change.

**T2 — sesh: release channel + self-update (`/install.sh`, `/releases`, `sesh update`)**
Store serves quick-§11.4-style endpoints from a releases dir on its read surface
(grant-gated in tsnet mode); installer script ends by invoking
`sesh setup --store-url $BASE`. `sesh update [--check]` fetches latest VERSION,
verifies the SHA-256 manifest, atomically replaces its own executable retaining
the previous binary, restarts its own unit, and verifies the running image
reports the new version.
*AC:* (1) matrix build (CGO_ENABLED=0, darwin/linux × arm64/amd64, stamped
version) + `just release` publishing version dir before atomically advancing
latest, with SHA-256 manifest; (2) a fresh Linux node onboards with one curl
command + enable-linger, no repo/toolchain; (3) `sesh update` on a stale node
converges binary and running service to latest with no cursor loss; failure
before verification leaves the prior runnable installation; (4) endpoints noted
as informational operator surface in the wire doc; frozen wire untouched; (5)
endpoints deny out-of-grant callers in tsnet mode; (6) a failed publication
leaves the previous latest fully usable.

**T3 — sesh: store deployment on quick-host (unit, user, backup, deny-verify, skew policy)**
System service `sesh-serve.service` under a dedicated `sesh` user, `--tsnet`,
data dir `/var/lib/sesh`; `just deploy-store` mirroring quick's deploy-server
(graceful-shutdown prerequisite: SIGTERM drains without false ACKs); backup
rider on quick's GCS timer; written escape triggers; skew-policy paragraph.
*AC:* (1) store reachable at `http://sesh-store.<tailnet>.ts.net:8765` with the
deny-verification pair recorded (403/refusal outside grant, 404 inside) before
real transcript flow; (2) quickd, Caddy, and the VM's tailscaled untouched;
quickd restarts and sesh restarts are independent; (3) backup covers mirror +
store.sqlite (snapshot API, never live-file copy) + tsnet dir in a
recoverable ordering; restore drill performed once: recovered high-waters
verified, samples byte-compared, reindex, a shipper resumes; (4) capacity/free-
space alerting so mirror growth cannot exhaust the shared disk (separate disk
evaluated); (5) `just deploy-store` builds, ships, restarts, prints the running
version; (6) escape triggers (mirror size, quickd incident, team growth) and the
skew policy (store-first order, wire-v2-is-a-fleet-event, schema forward-only,
current+previous support window) written into ops docs.

**T4 — sesh: fleet version visibility (`/nodes` version column + status check)**
Shipper sends `User-Agent: sesh-ship/<version>`; store records last-seen version
per node; `/nodes` shows it against the latest pointer. `sesh status` gains
`--check` against the release channel.
*AC:* (1) `/nodes` shows per-node shipper version and highlights ≠ latest,
alongside (not replacing) last-PUT silence detection; (2) wire doc gains an
explicit informational-only note — no routing/auth/storage semantics may ever
attach to User-Agent; absence never blocks shipping; (3) `sesh status` reports
update-available without breaking the exit-0 contract for a reachable unpoisoned
state; (4) works against a store predating the change (header ignored) and
against clients predating it (version shown as unknown); (5) tests distinguish
stale activity, old version, R23 refusal, poison, and unreachable store.

---

*Sources: two independent researcher memos (napkins/task-155-options-{vuro,nibe}.md
in tools/sesh at time of writing; napkins are branch-local — this doc is the
durable record), sesh justfile + etc/install-ship.sh + frozen specs, quick
justfile + scripts/build.sh + internal/cli/cmd_update.go + internal/server/apex.go
+ ops/, backlog doc-001.*
