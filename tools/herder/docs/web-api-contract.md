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

GET `/api/agents/{bus-name}`
  One agent: pane coordinate, tool, statuses, launch context, gap
  state. 404 for names not on the bus.

GET `/api/agents/{bus-name}/entries?from={byteOffset}&limit=N&sessionId={id}`
  The classified, immutable Claude session entry stream. The server
  resolves the active file from the bus roster's directory and session ID;
  it never searches for session files and never parses JSONL outside the
  `claudesession` resolver. Pairing (`tool_use` with `tool_result`, hcom
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

GET `/api/agents/{bus-name}/transcript?before={cursor}&limit=N`
  The agent page shows the FULL SESSION TRANSCRIPT (what hcom
  transcript actually serves), not bus correspondence. Windowed BY
  EXCHANGE, newest-last, paging backward from an opaque `cursor`
  (returned with every window). Never rehydrates the whole session.
  `detail` query knob: `exchanges` (prompts + replies) vs `full`
  (tool-level). Default resolved by delegation: pure UX call owned
  by implementation, changeable freely without a ruling; starting
  default `exchanges`. Both shapes exist from day one so the knob is
  a query param, not a rebuild.

GET `/api/agents/{bus-name}/transcript/stream` (SSE, per-agent)
  The legacy single-agent tail for curl and other direct API users.
  The web shell does not open these streams. Emits `exchange` events
  (same shape as the windowed read) as new transcript content lands,
  incrementally from the connection's start cursor; supports
  `Last-Event-ID` reconnect resume. A cursor from an older agent
  session is routine after resume/fork, so it is treated as absent:
  the request stays 200 and starts at the current tail. Malformed,
  cross-agent, or cross-detail cursors still refuse 400. Takes the
  same `detail` query knob as the windowed read (`exchanges` default |
  `full`). Emits an SSE comment heartbeat (`: ping`) every 15s while
  otherwise idle.

GET `/api/events?agents={comma-separated-bus-names}` (SSE)
  The web shell's ONE multiplexed stream per page. `agents` is the
  de-duplicated set of open agent tabs (at most 100); changing the set
  rebuilds this one stream. An absent set subscribes only to fleet and
  bus events. Event types:
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
one multiplexed EventSource per page. The legacy per-agent
`/transcript/stream` endpoint remains exchange-shaped for direct API
consumers and is not used by the web shell.

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
  no looser prose matching is permitted.

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

POST `/api/agents/{bus-name}/fork`
  Body: `{"prompt": "..."}` (optional). Wraps hcom fork; response =
  new agent name + placement.

Both lifecycle responses (pinned at implementation review
2026-08-25): HTTP 200 `{"name": "<bus-name>", "pane": "<pane-id>"}`.
Substrate success IS success: when the fork/spawn succeeded but
placement is not yet visible to the board poll, `pane` is empty and
the agent appears on `/api/fleet` within a poll tick — never a 5xx
for a session that exists (a false 502 invites double-forks).
Attribution, statuses, and infrastructure-vs-refusal classification
follow the message write's rules exactly.

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

No WebSocket. No pane injection. No cull/kill. No resume, no dead
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
2. `/api/agents/*` + transcript view (windowed + per-agent tail) +
   message write.
3. Contextual spawn + fork (may slip without killing v1 value).
Each step merges green behind the dark `serve` flag; UI ships
embedded (React + Vite, go:embed) per the web-lane plan.
