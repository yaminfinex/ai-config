# Fleet web API — pinned contract (v1)

Ratified by owner red-pen 2026-08-25 (fleet-refit web lane; drafting
history in the missions repo, artifacts/conductor). This file IS the
version: implementation tests quote it, and any shape change lands
here first. Numbers marked "proposed" (port, poll cadence) are
implementation-review adjustable; everything else changes only by
owner ruling.

## Ground rules (contract law)

- Served by `herder serve`; binds tailscale interface + loopback
  only; port from a flag (default proposed: 4400). Ships dark —
  nothing starts it until the owner turns it on.
- The API wraps live substrate (herdr socket, hcom CLI, tools/fleet
  spawn path). It holds NO state of its own — restart loses nothing.
- Every substrate failure is a loud structured error, never partial
  truth. Refusal body shape everywhere:
  `{"error": "<short>", "detail": "<substrate text>"}` with an
  honest HTTP status (502 substrate unreachable, 400 bad request,
  404 unknown agent/pane, 409 refused by substrate).
- Unversioned paths in v1; this contract file is the version.

### AMENDMENT (conductor, 2026-08-26) — opt-in serve auto-reload

The owner asked on 2026-08-26 for relief from manually restarting
`herder serve` after every merge. `herder serve --watch` therefore watches the
actual deployment plane and re-execs the same command-line arguments and
environment after a change remains stable across two consecutive lazy polls.
The normal `herder serve` behavior is unchanged.

The repository launcher deploys content-addressed cache binaries, so a serve
started through `bin/herder` watches that launcher's source-hash inputs and
re-enters the launcher; a directly invoked binary instead watches its resolved
executable target for literal replacement. Re-entering the launcher preserves
its last-good contract: if changed sources do not build, the serve comes back
on the checkout's last successfully built binary rather than dying. Re-exec
drops in-flight SSE connections; clients already reconnect and re-window from
the endpoint as source of truth.

## Visibility and scope (ruled)

- **Total transparency within the served scope.** Every attributed
  viewer sees the ENTIRE bus (all messages, all threads) and every
  agent's transcript. No per-viewer visibility filters, no private
  threads, no hidden agents. Joining the tailnet means joining on
  those terms.
- **The served scope is fixed in v1**: the default herdr session's
  socket plus the one hcom bus, full stop. (Herdr isolates per named
  session — one socket each, this box runs exactly one, "default";
  the hcom bus is scoped separately by its directory-isolation knob
  — one bus today.)
- **The herdr session is the named future unit of sharing**, should
  narrower sharing ever be needed: per-session sockets herdr-side
  and bus directory isolation hcom-side line up as the cheap
  boundary. Named here so the seam exists; NOT built in v1.
- **Latent join caveat** (recorded so it never surprises): if a
  second herdr session ever runs on this box, its agents still
  appear on the shared bus, so the board's gap logic would list them
  as `unplaced` from the served socket's view — technically true but
  misleading. Any future multi-session serve must revisit the join's
  scope before trusting `unplaced`.

## Reads

GET `/api/fleet`
  The board. Herdr structure AS-IS: workspaces → tabs → panes, each
  pane carrying the exact-pane-ID join (agent name, tool, herdr
  status, bus status, gap). Bus agents with no visible pane appear in
  a top-level `unplaced` list — the same two-way gap honesty as
  `herder list`, structured. This endpoint IS the list join; one
  shared Go package serves both.
  A workspace that herdr explicitly reports as an open linked worktree
  additionally carries `worktree_of: "<root-workspace-id>"`. The field is
  absent when herdr does not provide a live parent relationship; clients must
  never infer it from labels or paths.

  ### AMENDMENT (owner-ruled, 2026-08-26) — session identity is placement evidence

  The join accepts a second key: when a roster row carries no live pane claim
  (its spawn-time pane binding is absent or no longer matches any pane) and
  exactly one visible pane reports a herdr-detected agent session whose tool
  and session ID equal that row's, the row is placed in that pane. Session
  identity is derived from the live process on both sides, so this is
  evidence, not inference. An agent resumed in its pane therefore re-places
  itself without a respawn. Name matching remains display-only and never
  places; ambiguity (the same session detected in more than one pane, or two
  rows claiming one session) keeps the gap, honestly. Applies identically to
  `herder list` and this endpoint — one shared join package serves both.

GET `/api/agents/{bus-name}`
  One agent: pane coordinate, tool, statuses, launch context, gap
  state. 404 for names not on the bus.

  ### AMENDMENT (owner priority ruling, 2026-08-27) — agent model and context vitals

  The detail payload additionally carries the latest session-observed `model`
  and `context_usage`; either field is absent when its fact is unavailable.
  `context_usage` contains `used_tokens`, `input_tokens`, and only the raw
  source fields actually reported: `cached_input_tokens`,
  `cache_creation_input_tokens`, `cache_read_input_tokens`, `output_tokens`,
  `window_tokens`, and `used_percent`.

  Claude facts come from the latest complete `assistant` JSONL record carrying
  `message.model` or `message.usage`. Its `used_tokens` is the current input
  context (`input_tokens + cache_creation_input_tokens +
  cache_read_input_tokens`). Claude session records do not carry a context
  window size, so `window_tokens` and `used_percent` are absent. Codex model
  comes from the latest complete `turn_context`; usage comes from the latest
  `event_msg/token_count.info`, with `used_tokens` equal to
  `last_token_usage.input_tokens`. Codex `window_tokens` is the emitted
  `model_context_window`, and only that explicit denominator authorizes
  `used_percent`. No model-to-window lookup or guessed denominator is allowed.

  Example known-window fragment:
  `{"model":"invented-codex-model","context_usage":{"used_tokens":112600,"input_tokens":112600,"cached_input_tokens":101120,"output_tokens":266,"window_tokens":258400,"used_percent":43.57585139318885}}`.
  The multiplexed entry frame remains a wake signal and the detail endpoint
  remains truth: clients invalidate both entry and agent-detail TanStack cache
  rows on the existing `entry:{bus-name}` cadence. No additional stream exists.

GET `/api/viewer`
  Resolves the current connection through the same tailscale identity and
  sender-collision checks used by every write, without performing a write or
  retaining state. Success is `{"viewer":"<web-sender>"}`. This lets the web
  shell show its attribution on initial load rather than waiting for a message
  submission. An identity that genuinely cannot be attributed receives the
  normal 409 `attribution required` refusal; a derived sender collision is a
  409 `sender refused`; tailscale or hcom infrastructure failure is a 502
  `substrate unreachable`. All refusals use the pinned refusal body.

### AMENDMENT (conductor, 2026-08-26) — viewer identity is readable on load

The read-only `/api/viewer` endpoint above exposes the exact attribution result
that write paths already require. It has no server-side state and grants no new
authority: attributed and unattributed viewers retain the same v1 privileges.
The web shell calls it on load and keeps an honest unresolved state after a
semantic attribution refusal.

GET `/api/agents/{bus-name}/entries?from={byteOffset}&limit=N&sessionId={id}`
  The classified, immutable Claude session entry stream. The server
  resolves the active file from the immutable session ID. The roster's live
  directory is a fast path only; if the agent has changed cwd, the resolver
  locates that exact session ID across known Claude project directories.
  It never parses JSONL outside the `claudesession` resolver. Pairing (`tool_use` with `tool_result`, hcom
  stub with attachment) is consumer-side and does not change this stream.

  `limit` defaults to and is capped at 500 entries. With `from`, entries
  begin at that non-negative byte offset and `window.mode` is `from`.
  Without `from`, the server chooses the last N complete classified entries
  and reports their first byte offset with `window.mode` `tail`. A partial
  trailing JSONL line is held back. Resolver-owned tool-output truncation
  fields, including the 16 KiB cap's `truncated` and `total_bytes`, pass
  through unchanged.

  Successful read shape (fields absent in the source entry stay absent):
  `{"sessionId":"<current>","window":{"mode":"from|tail","from":0,"limit":500},"entries":[{"uuid":"...","line":0,"byteOffset":0,"timestamp":"...","kind":"human_prompt","payload":{...}}],"nextOffset":123,"stats":{"sidechainSkipped":0}}`.
  A quarantined entry additionally carries its resolver-provided
  `quarantine` object.

  Offsets are meaningful only within one session. Every response returns
  the current `sessionId`; clients pin it on subsequent `from` reads using
  the optional `sessionId` query parameter. A first `from` read without
  `sessionId` uses the current session and returns its ID. A mismatched ID
  returns the resolver's typed `session_changed` reset without a blind
  window; an offset beyond the current complete file returns its typed
  `truncated` reset. Reset responses contain `sessionId`, `window`, and
  `reset`, but no entries or fabricated next offset. `sessionId` without
  `from` is a 400. Refusals: 404 unknown bus agent; 409 no resolvable Claude
  session (wrong tool, missing/invalid ID, or absent derived file); 502 bus,
  filesystem, or other substrate failure.

### AMENDMENT (owner-ruled, 2026-08-27) — legacy exchange endpoints removed

The exchange-based `/api/agents/{bus-name}/transcript` and
`/api/agents/{bus-name}/transcript/stream` endpoints are removed. They were no
longer used by the web shell and duplicated the canonical immutable entries
endpoint plus multiplexed entry wake frames. Both legacy paths now receive the
standard structured 404 unknown-endpoint refusal. This amendment records the
conductor-authorized TASK-12 owner ruling of 2026-08-27.

GET `/api/events?agents={comma-separated-bus-names}` (SSE)
  The web shell's ONE multiplexed stream per page. `agents` is the
  de-duplicated set of open agent tabs (at most 100); changing the set
  rebuilds this one stream. An absent set subscribes only to fleet and
  bus events. Event types:
  - `hello`: the first frame on every connection, shaped as
    `{"buildIdentity":"<opaque>"}`. The identity names the running server
    build. The shell records the first identity loaded by the page; when a
    reconnect reports a different value it persistently offers a manual
    refresh and never reloads automatically;
  - `fleet`: a full board snapshot (sent on connect, then on any
    placement/status change — snapshot-not-delta keeps clients
    stateless; revisit only if size ever bites);
  - `message`: one bus message involving any agent (id, from, to,
    thread, text);
  - `entry:{bus-name}`: one immutable classified entry for a subscribed
    agent. Its payload is byte-for-byte the same JSON projection as one
    item in the entries endpoint's `entries` array (including quarantine
    metadata when present). The frame wakes a `nextOffset` catch-up read;
    the entries endpoint remains the source of truth;
  - `rewindow`: `{"agent":"<bus-name>"}` when that subscribed agent's
    session or transcript position resets; the shell discards the old
    window and fetches the current one;
  - `substrate`: a named source went unreachable/recovered (the UI
    must render this as a banner, not an empty board — the honesty
    rule in stream form);
  - `ping`: an empty client-visible heartbeat paired with the required
    SSE comment heartbeat (`: ping`) every 15s.
  Sourced from hcom's event subscription plus herdr snapshot polling
  (proposed: 2s) and one Claude-session entry tailer set per subscribed
  page-set. The shell owns reconnect: it closes any errored
  EventSource, rebuilds with bounded backoff and no stale cursor, and
  rebuilds if neither data nor heartbeat arrives for 45s. Its
  `reconnecting` indicator is present only while a rebuild is scheduled
  or in flight.

### AMENDMENT (owner-asked, 2026-08-27) — build identity handshake and manual refresh

Every multiplexed stream begins with the `hello` frame above. A server restart
can therefore tell an already-loaded page that its embedded client differs from
the running build. The response is a persistent “Server updated — refresh to
load the new version” banner with an explicit refresh button. Automatic reload
is forbidden because it can discard in-progress composer input.

The production launcher identity is its content-addressed source cache key,
which covers Go sources and embedded web distribution assets. Directly invoked
binaries use a SHA-256 of the executable contents.

### AMENDMENT (conductor, 2026-08-26) — multiplexed transcript frames are entries

The original v1 text specified `exchange:{bus-name}` frames on the
multiplexed stream. That is struck and replaced by the `entry:{bus-name}`
shape above. Exchange positions advance only when a new exchange begins;
assistant text, thinking, tool calls, and tool results can append to the
same position and therefore produced no wake-up. That made correct live
follow impossible even after the entries endpoint became canonical.

The stream now tails the same immutable `claudesession` entries served by
the windowed endpoint. It emits only content appended after subscription;
reconnect catch-up always reads from the endpoint using `sessionId` plus
`nextOffset`. Session replacement or truncation still emits `rewindow`,
and the client discards its window and refetches. There is still exactly
one multiplexed EventSource per page. The legacy per-agent exchange stream was
subsequently removed by the 2026-08-27 owner ruling above.

### AMENDMENT (conductor, 2026-08-26) — entries are tool-dispatched

The entries endpoint and multiplexed `entry:{bus-name}` tail are no longer
Claude-only. They dispatch from the hcom roster row's `tool` while preserving
one wire projection and all existing window, byte-offset, complete-line,
reset, rewindow, and endpoint-as-truth laws. The client renders `kind` and
never receives or branches on a source-tool discriminator in an entry.

For `tool: claude`, resolution and parsing remain byte-for-byte the existing
`claudesession` behavior. For `tool: codex`, the `codexsession` resolver
validates the roster session ID and requires exactly one match under
`~/.codex/sessions/YYYY/MM/DD/rollout-*-<sessionId>.jsonl`; zero or ambiguous
matches are honest 409 `no session` refusals on the endpoint and transcript
substrate failures on the stream. Unknown complete rollout lines are served
as quarantined `unknown` entries, and partial trailing lines are held back.
Tools with neither a Claude nor Codex session file keep their prior refusal
and stream-failure behavior.

### AMENDMENT (conductor, 2026-08-27) — tool-neutral developer-delivery subtype

Codex hcom developer deliveries use the tool-neutral payload subtype
`developer_message`. The former `codex_developer_message` wire name is struck;
entry `kind` remains `hcom_delivery` and the normalized deliveries shape is
unchanged.

### AMENDMENT (conductor, 2026-08-27) — Codex developer injections are visible

A Codex developer-role message that is not an hcom delivery is served as an
`injected_system` entry with the same normalized message-content shape used by
the shared renderer. Empty developer messages remain non-renderable. This
brings the Codex transcript into parity with Claude's injected-system entries.

### AMENDMENT (conductor, 2026-08-27) — Codex compaction summaries are visible

When a Codex `compacted` record has an empty `message`, the server projects the
last visible summary text from `replacement_history` into the compact divider's
normalized message shape while preserving the replacement history. A textual
compaction item takes precedence over the final retained message. Records with
no visible replacement text remain honest rather than fabricating a summary.

### AMENDMENT (conductor, 2026-08-27) — Claude batch envelopes authorize splits

A non-enveloped Claude hcom attachment always produces at most one normalized
delivery; only its initial header supplies metadata, and header-shaped body text
cannot split it. Multiple deliveries are projected only when hcom's leading
`[N new messages]` batch envelope is present and the number of candidate
delivery headers is exactly `N`. Any count mismatch preserves the complete
unwrapped body as one delivery instead of guessing which boundary was forged.

## Writes (plain HTTP POST — ruled: no WebSocket in v1)

POST `/api/agents/{bus-name}/message`
  Body: `{"text": "..."}`. Sends on the bus as an ATTRIBUTED web
  peer (below). RULED: the server marks EVERY web message as a
  reply-expected request (intent=request) — always, no knob in v1 —
  so agent response rules guarantee an answer. Web origin is
  disclosed to the agent (owner ruling 2026-08-25): the server
  prepends a bracketed context note to the delivered text — the
  sender is a web operator, hcom replies cannot reach them, answer
  in the normal chat turn. Web senders are NOT addressable bus
  peers (their identity is transient; observed live 2026-08-25 when
  agent replies to a web sender bounced): the agent's answer
  arrives on the web viewer's transcript tail, not as a bus
  message. No pane injection, ever.

  **2026-08-26 operator-note amendment.** New deliveries fence that
  agent-facing instruction block with stable, line-delimited markers; the
  operator's text follows after one blank line:
  ```text
  [HERDER_WEB_OPERATOR_NOTE_BEGIN]
  [This message came from a web operator named <web-sender> via the fleet web view. They cannot receive hcom messages; do not reply with `hcom send`. Answer in your normal chat turn; they are watching the session transcript live.]
  [HERDER_WEB_OPERATOR_NOTE_END]

  <operator text>
  ```
  The agent receives the complete fenced instruction unchanged. Transcript
  clients strip the fenced prefix for display because sender attribution is
  already rendered separately. For existing session files only, clients also
  strip the exact legacy unfenced sentence when it is the delivery-body prefix;
  no looser prose matching is permitted. (2026-08-26, post-review: clients also
  strip the short-lived pre-release `<<<HERDER_WEB_OPERATOR_NOTE>>>` /
  `<<<END_HERDER_WEB_OPERATOR_NOTE>>>` marker spelling — exact-marker prefix
  only — solely for compatibility with deliveries already persisted in existing
  session files; new deliveries never use it.)

  Success response (pinned at implementation review 2026-08-25):
  `{"sent": true, "to": "<agent>", "from": "<web-sender>",
  "intent": "request"}`. Refusal statuses on this path: 404 unknown
  agent; 400 bad body; 409 attribution required / sender collision /
  refused by substrate (semantic refusals, incl. tailscale's
  peer-not-found for an unresolvable peer); 502 substrate
  unreachable (infrastructure: timeouts, missing/unreachable hcom or
  tailscaled — never conflated with a refusal).

POST `/api/spawn`
  Contextual only. Body:
  `{"from_pane": "<pane-id>", "shape": "pane" | "tab" | "worktree",
    "tool": "claude" | "codex", "tag": "...", "prompt": "...",
    "branch": "<only for worktree shape>"}`.
  Placement derives from `from_pane`'s tab/workspace; worktree shape
  inherits the workspace's repo. Wraps the one spawn path
  (tools/fleet — which grew a same-tab split placement for the pane
  shape at implementation, 2026-08-25, keeping terminal and web on
  one path). Response: the new agent's bus name + pane. Refusals
  (unknown pane, workspace without a repo for worktree shape) are
  409s quoting the substrate.

The spawn lifecycle response (pinned at implementation review
2026-08-25): HTTP 200 `{"name": "<bus-name>", "pane": "<pane-id>"}`.
Substrate success IS success: when the spawn succeeded but
placement is not yet visible to the board poll, `pane` is empty and
the agent appears on `/api/fleet` within a poll tick — never a 5xx
for a session that exists (a false 502 invites duplicate spawns).
Attribution, statuses, and infrastructure-vs-refusal classification
follow the message write's rules exactly.

### AMENDMENT (owner-ruled, 2026-08-26) — web fork removed

The owner ruled that the web fork control does not work and breaks sessions.
The client control and `POST /api/agents/{bus-name}/fork` endpoint are removed;
the path now receives the standard 404 unknown-endpoint refusal. This removes
only web fork. Contextual spawn remains available, and lifecycle operations
outside this web API are unchanged.

## Web-peer attribution (ruled — tailscale identities, flat authority)

- The server derives the sender of EVERY request from the tailnet
  identity of the connection (whois-style lookup against the local
  tailscale daemon). No cookies, no chosen names, no login UI.
- The derived identity is presented on the bus as a visibly
  web-origin sender. Rendering (settled at implementation review
  2026-08-25): `web-<slugged tailnet login>` — lowercase ASCII
  letters/digits with separator runs collapsed to single hyphens,
  bounded at 50 chars with a stable 8-hex SHA-256 suffix when
  truncated (e.g. `Alice@Example.com` → `web-alice-example-com`).
  Accepted on record: two distinct logins can slug to the same
  sender; harmless under flat authority because identity is
  attribution, never authorization.
- Reserved and existing agent names are refused as senders — a web
  peer can never send as an agent or the conductor.
- A connection whose tailnet identity cannot be resolved (e.g. bare
  loopback with no tailscale peer) gets reads but refused writes,
  loudly — attribution is a precondition of writing, not decoration.
  (Ratified as drafted; owner note: anything local can always just
  use hcom directly.)
- Authority is FLAT (ruled: "all web users are the same level of
  authority, treat all humans the same"). Tailnet identity is
  attribution only, NEVER authorization: no identity maps to owner
  or elevated bus authority, no web peer claims an existing bus
  name, no web peer outranks another. The API has exactly two
  privilege levels: attributed (full v1 surface) and unattributed
  (reads only).

## Explicitly absent (ruled)

No WebSocket. No pane injection. No cull/kill. No resume or fork, no dead
sessions, no sesh. No blank-form/global spawn, no new-workspace
creation. No auth beyond the tailnet boundary. No server-side state.
No screen view for AGENT sessions — the tailed transcript is the
window.

## Named candidate for a later unit (dated option, not scope)

Non-agent terminal panes (plain shells — no transcript; the screen
is their only content): read-only SNAPSHOT mirror, poll-on-view
(capture-pane style, cheap), no live pty streaming. Owner sees
"strong benefit... if cheap enough" (2026-08-25); explicitly NOT a
v1 commitment.

## Build order inside the lane (ruled: existing sessions first)

1. `/api/fleet` + `/api/events` + board UI (read-only heart).
2. `/api/agents/*` + transcript entries view + message write.
3. Contextual spawn (web fork removed by the 2026-08-26 owner ruling).
Each step merges green behind the dark `serve` flag; UI ships
embedded (React + Vite, go:embed) per the web-lane plan.
