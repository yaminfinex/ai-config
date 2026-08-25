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

GET `/api/agents/{bus-name}`
  One agent: pane coordinate, tool, statuses, launch context, gap
  state. 404 for names not on the bus.

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
  The tail. Opened when an agent page is open, closed on navigate
  away — a PER-AGENT subscription so the main `/api/events` stream
  never becomes an all-agents transcript firehose. Emits `exchange`
  events (same shape as the windowed read) as new transcript content
  lands, incrementally from the connection's start cursor; supports
  `Last-Event-ID` reconnect resume so a dropped tab picks up where
  it left off instead of rehydrating. Takes the same `detail` query
  knob as the windowed read (`exchanges` default | `full`; added at
  implementation review 2026-08-25 — without it, full-detail pages
  received plain-exchange tails). Resume cursors bind detail exactly
  like page cursors: cross-detail replay refuses 400.

GET `/api/events` (SSE)
  One stream, three event types:
  - `fleet`: a full board snapshot (sent on connect, then on any
    placement/status change — snapshot-not-delta keeps clients
    stateless; revisit only if size ever bites);
  - `message`: one bus message involving any agent (id, from, to,
    thread, text);
  - `substrate`: a named source went unreachable/recovered (the UI
    must render this as a banner, not an empty board — the honesty
    rule in stream form).
  Sourced from hcom's event subscription plus herdr snapshot polling
  (proposed: 2s) diffed server-side.

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
