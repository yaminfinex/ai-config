# Herdr socket subscriptions versus herder polling

> **Date:** 2026-07-12
>
> **Question:** Which polling and waiting mechanics in `tools/herder` could move to Herdr's socket subscription or plugin surface, and would that improve correctness or cost?
>
> **Method:** Static inventory of production Go under `tools/herder`; review of the existing observer socket client and contract tests; read-only probes of the installed Herdr server, schema, waits, and plugin inventory; and comparison with Herdr's official socket documentation. No panes, plugins, registry rows, or other live state were created or changed. Probe subscriptions were bounded by sub-second/one-second client timeouts and closed before the probe exited.

## Executive finding

The observer is the one migration that pays, and it has already happened: lifecycle events wake a level-triggered `session.snapshot` reconciliation, while a 30-second sweep and a fresh snapshot after reconnect cover dropped events, connection gaps, and non-Herdr evidence. Keep that hybrid.

No additional polling loop should move wholesale to Herdr subscriptions now. The remaining loops either observe hcom/registry facts that Herdr never emits, require current screen or identity state rather than an edge, already use Herdr's push-style wait CLI, or are infrequent command-time probes whose saved process launches do not justify another raw-protocol consumer. Plugin packaging is **not worth it** for any of them: a plugin adds installation, lifecycle, and state ownership without adding events unavailable to an ordinary socket client.

One small hardening task does pay independently of further migration: extend the existing live-contract check to pin subscription acknowledgement and schema shapes. That is the only filed-ready task in the appendix.

## 1. Inventory

Scope: executable production code under `tools/herder/internal`. Parser loops, collection iteration, test-harness retry loops, and blocking on ordinary child-process completion are excluded. Intentional fixed sleeps and server-side waits are included because they are part of the same latency/correctness decision surface.

| Site | What it polls or waits for | Cadence and bound | Failure it papers over |
|---|---|---|---|
| `internal/send/hcom.go:194-216` | A new hcom delivery-receipt event after a pre-send event-id watermark | 250 ms; default 3 s, with callers able to pass a larger window | Receipts have no message correlate and second-granular timestamps, so the loop serializes and looks for a strictly newer event before admitting `delivered`; timeout degrades honestly to `queued` |
| `internal/spawncmd/spawn.go:1130-1168` | Pane text, visible screen, and agent status until idle/done plus two identical screen samples; optionally a ready substring | 500 ms; default 15 s; 800 ms after accepting a trust modal | TUI boot/redraw lag, alternate-screen trust prompts, unknown blocking overlays, and false readiness from one transient frame |
| `internal/spawncmd/spawn.go:1206-1229` | Child-specific hcom bind via registry enrichment or roster launch-context, while also reading the pane for trust modal/ready text | 500 ms; default 60 s; 800 ms after accepting a trust modal | Async sidecar/roster publication, missing pane correlation for some agent families, and a modal that blocks boot before bus bind |
| `internal/spawncmd/spawn.go:1723-1729`, then `:1050-1083` | First the registry for sidecar-written `hcom_name`, then the hcom roster for the launch pane | Six 700 ms attempts in each phase | A child can bind after spawn returns from the start RPC; guessing by tag/cwd is intentionally forbidden because it can bind the wrong session |
| `internal/spawncmd/spawn.go:959-960` | Fixed post-readiness settling, no probe | Default 1.5 s | Lets a just-ready TUI finish redraw/input setup before the legacy paste path runs |
| `internal/spawncmd/bootpaste.go:129-169` | Message landing via agent-status transition, visible payload text, or pasted-blob count | 250 ms up to 2.5 s per send, at most two sends; 400 ms after a failed send RPC | Agent-send can succeed before the TUI reflects the paste; guarded retry avoids duplicating text already in an unreadable composer |
| `internal/spawncmd/bootpaste.go:309-325` | Submission via status transition, transcript echo, or a positively empty composer | 250 ms for 3 s per Enter attempt | Herdr status can lag and transcript redraw can erase the echo; the composer condition is accepted only with immediately-prior payload evidence |
| `internal/spawncmd/bootpaste.go:335-343` | Collapsed paste blob disappearing or the composer becoming empty | Eight reads at 300 ms; the caller can make up to three guarded Enter attempts at `:198-209` | Claude-style pasted blobs can keep a transcript marker after submission, making blob count alone a false negative |
| `internal/spawncmd/bootpaste.go:101-110`, `:176-190` | Two fixed UI-propagation pauses, then a pane read | 200 ms each | Allows composer clear and pre-Enter screen state to become observable before making an irreversible input decision |
| `internal/lifecyclecmd/lifecycle.go:785-818` | Fixed survival window, then `pane.get` | Default 7 s | A fork/resume start RPC can succeed even though the child exits before lifecycle bind; the delayed lookup records a failed launch instead of a live seat |
| `internal/lifecyclecmd/lifecycle.go:849-872` | Registry row gaining a bus name before delivering the resume doctrine | 500 ms; default 60 s | Sidecar bus enrichment is asynchronous and cannot travel in the resumed agent's stripped context |
| `internal/sidecarcmd/sidecar.go:234-245` | hcom roster until this sidecar can correlate its pane to a row | 700 ms, up to 90 attempts (about 63 s) | The hook sidecar may start before hcom exposes launch context or before herder has written the registry row |
| `internal/sidecarcmd/sidecar.go:136-180` | Parent PID, hcom roster/status/session id, registry correlation, and statusline snapshots | Every 2 s indefinitely; exits after five consecutive missing-row samples | Bridges hcom lifecycle into registry enrichment and status reporting, detects rebind/session turnover, and cleans up after the owning hook process disappears |
| `internal/spawncmd/compactthen.go:154-171` | A trustworthy hcom event-id watermark | Default three attempts at 1 s | A transient hcom failure must disable history proof rather than let a pre-arm idle event authorize mid-turn continuation |
| `internal/spawncmd/compactthen.go:110-151` | hcom status plus post-watermark hcom status history for a proven working-to-listening transition | Default 1 s; default total deadline 15 min | The detached waiter may arm after the active sample, while stale idle state must never inject a continuation before compaction ends |
| `internal/spawncmd/compactthen.go:182-210` | Retry of hcom delivery after turn end | Default 2 s backoff, within the same 15-minute deadline; each send embeds the receipt loop above | The bus identity can be briefly unavailable while compaction is still replacing the session |
| `internal/observercmd/observer.go:838-939` | Herdr lifecycle events plus an authoritative registry × socket snapshot × hcom reconciliation | Event wakeups, a 30 s ticker, and 250 ms bounded event-channel waits; failed connects/reconnects back off 30 s | Events have gaps and the observer also needs bus/process evidence; periodic snapshots recover missed edges, dropped buffered events, restarts, and quiet state drift |
| `internal/observercmd/socket.go:290-328` | One socket RPC response | Passive wait up to 3 s | A dead or behaviorally incompatible socket must not hang a sweep forever |
| `internal/waitcmd/wait.go:48-76` | Requested semantic agent status through `herdr wait agent-status` | Server-side one-shot wait; default 60 s | This is already push-style. On timeout, one-shot `agent.get`/`pane.list` diagnostics distinguish codex `done`, a gone pane, and lost agent detection (`:202-255`) |
| `internal/observercmd/observer.go:1051-1065` | Fixed grace after signalling a stale observer, no probe | 100 ms | Gives a stale process a short TERM window before forced replacement |

### One-shot live-state sweeps and probes

These are not loops, but they are relevant to the proposed replacements:

- `listcmd/list.go:239-291` and `reconcilecmd/reconcile.go:177-210` each query `agent.list` and `pane.list` separately. The second query distinguishes a live-but-undetected pane; the split can observe two adjacent server states.
- `cullcmd/cull.go:167-183`, `:257-290`, and `:352-361` take an agent-list snapshot, verify a pane's terminal identity with `pane.get`, then trust the synchronous `pane.close` result.
- `panecleanup/panecleanup.go:23-65` is stronger: `pane.get`, identity check, `pane.close`, then another `pane.get` proving absence. Spawn/lifecycle rollback uses it.
- `waitcmd/wait.go:48-58` resolves the current pane with `pane.list` before entering the server-side wait; `enrollcmd/enroll.go:41-48` uses one `pane.get` to anchor the current seat.

## 2. Surface available today

### Protocol and transport

The installed server is Herdr 0.7.3 and reports protocol 16, `compatible: yes`, on the default Unix socket. The installed `herdr api schema --json` reports schema version 1; the observer pins protocol 16 at `observercmd/socket.go:20,103-110` and discovers the socket through `herdr status server` at `:113-176`.

The documented wire format is newline-delimited JSON with request ids, and a subscription keeps its connection open after the acknowledgement. Official guidance says to prefer CLI wrappers for simple orchestration and use raw sockets for protocol clients or long-lived subscribers; it also says to take a new `session.snapshot` after reconnect or possible cache staleness. [Herdr socket API](https://herdr.dev/docs/socket-api/)

Read-only live probes confirmed:

- `session.snapshot` returns `result.type = "session_snapshot"` and nests the actual state under `result.snapshot`; the observer unwraps this through `herdrcli.ParseSessionSnapshotResult` at `observercmd/socket.go:369-375`.
- A query connection returned the first response and then EOF before a second read-only request on the same connection. This is why the observer reserves one persistent connection for `events.subscribe` but opens a fresh connection per other RPC (`observercmd/socket.go:290-366`).
- `events.subscribe` acknowledged with `result.type = "subscription_started"` and held the connection open. A pushed global lifecycle event used a top-level snake-case `event` plus `data`, matching the schema's `event` shape; subscription request names use dotted spelling.
- A quiescent follow-up subscription produced only its acknowledgement during the bounded probe. There is no documented cursor/resume parameter or event sequence id, so this is not evidence of replay; clients must treat reconnect as a gap and resnapshot.
- `herdr plugin list` reported no installed plugins and `herdr plugin action list` reported no actions.

### Subscription vocabulary and semantics

The installed request schema permits these subscription types:

- Workspace: `workspace.created`, `.updated`, `.renamed`, `.moved`, `.closed`, `.focused`.
- Worktree: `worktree.created`, `.opened`, `.removed`.
- Tab: `tab.created`, `.closed`, `.focused`, `.renamed`, `.moved`.
- Pane lifecycle: `pane.created`, `.closed`, `.focused`, `.moved`, `.exited`, `.agent_detected`.
- Pane-specific: `pane.output_matched` (pane, read source, substring/regex match), `pane.agent_status_changed` (pane, optional target status), and `pane.scroll_changed` (pane).
- Layout: `layout.updated`.

The three pane-specific types have a dedicated `subscription_event` schema; general lifecycle pushes use the broader `event` schema. The official documentation confirms that the first response is an acknowledgement and later lines are pushed events, and directs callers to dedicated wait helpers where a one-shot wait exists. [Herdr event subscriptions](https://herdr.dev/docs/socket-api/#event-subscriptions)

Two assumptions in the earlier problem statement do **not** survive the installed schema check. `agent_started` is a success-response type for `agent.start`, not a subscribable event; boot observation is `pane.agent_detected`. `session.snapshot` is a query/bootstrap response, not an event, and official guidance explicitly requires another snapshot after reconnect.

No subscription carries a durable cursor, generation, replay window, or acknowledgement high-water in the installed schema. The observer therefore treats events only as latency hints, drops an event when its in-process buffer of 32 is full (`observercmd/socket.go:81,243-246`), and makes correctness level-triggered through a full snapshot.

### Reconnect and generation behavior

The observer's generation policy is the reusable part of its client, not merely its JSON codec:

1. It stats the socket before and after dial and refuses an incarnation change (`observercmd/socket.go:58-74`).
2. It keeps only the subscription on the persistent connection and uses a fresh connection for every query (`:290-353`).
3. It rechecks the socket inode around each query (`:355-366`).
4. On reconnect it creates a fresh `seenTerms` set and marks `connectionGap: true`; only absence observed after a prior sighting on the same uninterrupted connection can be decisive (`observercmd/observer.go:258-317,425-447,848-939`).

This caution is evidence-based. The live server has previously changed from multi-request connections to closing query connections after the first request while still reporting protocol 16; the repository diagnosis records the resulting reconnect spin and the split-transport repair. The present live two-request probe reproduced that connection behavior. Separately, live response wrapping under `result.snapshot` once differed from the mock and made mocked success misleading. Protocol 16 is therefore a useful compatibility gate, not proof that connection lifetime or payload wrapping is unchanged.

The official stability statement is narrower: protocol changes are reviewed with release compatibility in mind, clients should check the reported protocol, and clients should tolerate unknown fields. It does not promise semantic versioning, replay, or that every connection/payload change bumps the protocol. [Herdr protocol stability](https://herdr.dev/docs/socket-api/#protocol-stability)

### Direct client versus plugin packaging

Plugins are an early host surface for manifest-declared actions, event hooks, panes, and link handlers. They persist in Herdr's plugin registry, run with Herdr-provided environment paths, and own their own state; there is no managed plugin storage API in the current host surface. [Herdr plugin API](https://herdr.dev/docs/socket-api/#plugin-apis)

None of the candidate mechanics needs an in-terminal action, a plugin pane, or server-managed invocation. An ordinary process can already open the same socket, and the observer must outlive/recover independently from Herdr restarts. Packaging these mechanics as a plugin would couple their lifecycle to the server and introduce install/enable/log/state behavior without improving event coverage. Prefer the existing direct client only for a genuinely long-lived subscriber; prefer the Herdr CLI for one-shot waits and queries.

## 3. Mapping

| Current site/mechanic | Candidate primitive | Fit and gap | Migration risk | Verdict |
|---|---|---|---|---|
| Observer daemon periodic reconciliation | Existing `events.subscribe` wakeups + `session.snapshot` | Exact fit already in place; events reduce dead-pane latency while snapshots recover gaps and incorporate hcom/process evidence | Removing the sweep would make buffer overflow, reconnect, quiet drift, and non-Herdr evidence correctness bugs | **Keep hybrid as-is** |
| `herder wait` semantic status wait | `pane.agent_status_changed` / existing wait helper | Exact event, and the CLI already wraps it as a one-shot server wait | A new raw client would duplicate timeout, target resolution, response, and reconnect handling for no user-visible gain | **Keep the CLI wait; migration not worth it** |
| Spawn `awaitReady` and post-ready settle | `pane.agent_status_changed`, `pane.output_matched`, `pane.scroll_changed` | Status/substring events can wake checks, but cannot prove two stable frames, an empty composer, or visible alternate-screen modal state; quiet stability is absence of events | Must subscribe before start/read to avoid a missed edge, still needs pane reads, and expands raw-socket use in a short-lived command | **Not worth it** |
| Legacy boot-paste landing/submission | `pane.output_matched` + `pane.agent_status_changed` | Good positive signals for payload echo and working transition; no event represents composer-empty, blocked modal, or guarded duplicate avoidance | A match can occur in scrollback rather than the composer, event setup can race the send, and this path now serves only bash prompts and self-compaction | **Not worth it; retain evidence-gated polling** |
| Fork/resume 7-second survival settle | `pane.exited`/`pane.closed` plus timeout | Can fail faster on an exit edge, but success still requires waiting the full survival window and a final level check | Missed event or reconnect cannot prove survival; saves latency only on failing launches | **Not worth it** |
| Cull identity and close confirmation | `pane.closed`/`pane.exited` | Can acknowledge an edge only if subscribed before close; it does not replace the pre-close terminal identity check or prove absence after a connection gap | Synchronous close plus authoritative `pane.get` is simpler and replay-free; event-only confirmation is weaker | **Do not migrate. If strengthened, reuse `CloseConfirmed`, not a subscription** |
| `list`/`reconcile` agent + pane sweeps | `session.snapshot`, ideally via CLI | A single snapshot is more coherent than adjacent `agent.list` and `pane.list` calls | Direct raw use requires extracting the observer client and inheriting wrapping/connection drift for commands run only on demand | **Raw socket not worth it; consider CLI snapshot only if incoherence is observed** |
| Spawn bind/name capture and resume addendum bind | None | Facts are hcom membership and registry enrichment; `pane.agent_detected` neither supplies nor proves the bus name | Substituting a pane event would recreate wrong-session delivery hazards | **Keep hcom/registry polling** |
| hcom delivery receipt loop | None | Herdr output/status events cannot correlate a bus receipt to the sent message | Cross-substrate inference would weaken the current strictly-newer receipt proof | **Keep** |
| Compact-then watermark, turn-end, and delivery retry | None | Herdr status overlaps superficially, but the proof is tied to hcom identity/history and a post-arm event watermark | Pane ids can move across compaction; Herdr events have no durable cursor to replace the watermark | **Keep hcom proof; not a Herdr migration** |
| Sidecar discovery/steady status and statusline snapshots | `pane.agent_status_changed` | Herdr status is downstream/overlapping information; the loop needs hcom name, session id, process binding, registry row, and whole-roster snapshots | Moving to Herdr would be circular for status and still leave nearly every required fact polled | **Keep** |
| One-shot `pane.get` probes in enroll/spawn/cleanup | `pane.get` or `session.snapshot` over raw socket | Technically exact query mapping, no subscription advantage | CLI launch overhead is negligible at this frequency; direct client adds protocol and platform/socket-path obligations | **Keep CLI queries** |

## 4. Recommendation

Ranked by value:

1. **Keep the observer's current subscription-plus-snapshot hybrid.** It delivers the proven latency win without pretending an edge stream is durable truth. No migration task is needed.
2. **Add a small live-contract guard for the subscription acknowledgement/schema.** This pays because a production raw-socket consumer already exists and the known failures were mock-versus-live/unchanged-protocol drift. Filed-ready text is in Appendix A.
3. **Keep `herder wait` on the upstream one-shot wait helper.** It already obtains the push benefit at the supported CLI layer.
4. **Do not migrate the other loops.** In particular, boot-paste, lifecycle settle, cull verification, list/reconcile, hcom receipts, sidecar status, bus bind, and compact-then are **not worth it** on today's surface.

Revisit only if one of these triggers appears: measured CLI process-launch cost becomes material; a second long-lived Herdr subscriber needs the same transport; Herdr adds cursor/replay semantics; or a concrete correctness incident shows adjacent query incoherence. At that point, extract the observer client into a shared internal package rather than copying it, keep subscription and RPC connections split, and require both hermetic adversarial servers and live-shape checks.

## Appendix A — filed-ready task capture

### Title: Pin live Herdr subscription acknowledgement and schema contracts

Herder already depends on a long-lived Herdr subscription, but the live-contract tier currently pins the schema and nested snapshot response more strongly than the subscription handshake. Extend the read-only live check to verify protocol compatibility, the `subscription_started` acknowledgement, and the required observer event subscription variants, with a hard timeout and guaranteed connection close. Keep hermetic coverage for both one-request-per-query and multi-request servers so the check detects live-shape drift without making either query connection policy an accidental requirement.

**Acceptance criteria**

- The live-contract script opens only a read-only `events.subscribe` request, asserts `result.type == "subscription_started"`, times out deterministically, and leaves no connection or process behind.
- The live schema assertion verifies protocol 16 and the presence/parameter shapes of `pane.created`, `pane.closed`, `pane.exited`, and `pane.agent_detected` subscriptions.
- Hermetic socket tests continue to cover a persistent subscription connection, fresh-per-query operation against a close-after-first-request server, nested `result.snapshot`, reconnect backoff, and socket-incarnation change.
- The negative path demonstrates that a mock-only acknowledgement shape such as `{"ok":true}` fails the same parser/assertion used by the live check.
- The check is documented as optional live-environment coverage and performs no pane, plugin, registry, or server mutation.
