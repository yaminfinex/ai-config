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
  The narrow file/folder watch exception below is ephemeral request state:
  each watch set belongs solely to one `/api/events` connection, is derived
  from that request, and is discarded in full when the connection closes or
  the server restarts. It is never persisted or shared as authority.
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

  ### AMENDMENT (owner-approved, conductor-acked, 2026-08-28) — pane-session tool evidence and unattributed terminals

  Herdr's live `agent_session` object is a single placement evidence record:
  its `agent` member is the detected tool and its `value` member is the
  immutable session ID. The shared join preserves both members and applies the
  exact unique tool-plus-session rule above. A pane title, label, command line,
  environment name, or bus display name remains display-only and never places
  an agent. When neither an exact live pane claim nor a unique live
  tool-plus-session match exists, the bus row stays `unplaced`.

  Conversely, a visible Herdr pane without join evidence is not asserted to be
  agent-free. The client labels it **Terminal** and states plainly
  that Herdr cannot attribute the terminal and that it may belong to an
  unplaced agent. This warning accompanies its standalone read-only screen.

  ### AMENDMENT (conductor-acked, 2026-08-27) — explicit Claude subagent families

  A Claude Task subagent has no pane or independent session ID. hcom instead
  exposes `agent_id` and an explicit `parent_name`, while the parent row exposes
  its `base_name`. When `parent_name` equals exactly one live roster
  `base_name`, the board nests the child in the parent's optional `subagents`
  array and omits that proven child from top-level `unplaced`. Each nested row
  retains its honest `pane_id:"-"`, `gap:"no visible pane"`, and adds
  `parent_agent:"<full bus name>"`. The parent may itself be placed or
  unplaced. Missing or ambiguous parent evidence leaves the child top-level
  unplaced. Display-name patterns and tags are never parent evidence, and the
  regular exact-pane/session placement join above is unchanged.

  The sidebar and board render that same nested payload beneath the parent;
  the child remains an independently addressable bus agent. No second fleet
  projection or client-side parent inference exists.

GET `/api/agents/{bus-name}`
  One agent: pane coordinate, tool, statuses, launch context, gap
  state. 404 for names not on the bus.
  A proven Claude Task subagent additionally carries
  `parent_agent:"<full bus name>"`; the field is absent when the exact roster
  parent relationship is unavailable or ambiguous.

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

  ### AMENDMENT (owner ask, 2026-08-27) — queued hcom delivery visibility

  The detail payload additionally carries `queued` only when the server can
  prove which recent hcom messages addressed to this agent are absent from the
  complete current session transcript. Each item is shaped as
  `{"id":731,"sender":"web-owner","intent":"request","preview":"operator question","sent_at":"2026-08-27T04:00:00Z","operator":true}`.
  `operator` is present only when the server recognizes the exact fenced web
  operator envelope; a sender name beginning with `web-` is not proof.
  Message IDs are the shared proof key: an item remains queued while its bus ID
  is absent from normalized `hcom_delivery` entries and disappears once that ID
  occurs in the transcript. For Claude, a candidate sent before the most recent
  normalized `compact_divider` is unprovable because compaction may have erased
  its delivery entry, so it is also omitted rather than shown as queued. This
  boundary does not count as delivery. Codex rollout files are append-only and
  retain earlier delivery records across compaction, so Codex compact markers
  do not suppress candidates. If the bus history or current Claude/Codex
  session cannot be read, `queued` is absent rather than guessed.
  The existing multiplexed `message`
  frame invalidates addressed agent-detail rows, and the existing
  `entry:{bus-name}` frame invalidates both detail and entries; no stream is
  added. This amendment answers the owner's 2026-08-27 request to make web
  operator sends visible while they wait for the agent's next injection turn.

  ### AMENDMENT (owner-asked, conductor-acked, 2026-08-27) — queued truth is operator-only and PostToolUse-aware

  `queued` is intentionally scoped to web-operator messages: the server admits
  only candidates whose text carries the exact fenced operator envelope. Other
  bus traffic remains available in the transcript and bus stream, but is never
  presented by this endpoint as operator input awaiting delivery.

  Claude delivery proof additionally recognizes the model-visible
  `hook_additional_context` attachment emitted by `PostToolUse`. Recognition
  requires `hookEvent:"PostToolUse"`, a matching `PostToolUse` hook name, and
  exactly one string containing an exact `<hcom>...</hcom>` envelope. Only its
  anchored leading delivery header, or an exactly counted hcom batch, supplies
  normalized delivery metadata. The duplicate `hook_system_message` record and
  nested `hook_success.stdout` text are not delivery sources. A normalized ID
  excludes a queued candidate only when ID, sender, recipient, intent, and
  thread also match that real bus candidate addressed to the agent. Free-form
  body text therefore cannot forge delivery proof. When hcom event rows carry
  base names but transcript headers carry full tagged names, the server uses an
  exact unique roster base-name match to compare the same sender identity;
  missing or ambiguous roster evidence remains unmatched. UserPromptSubmit deliveries
  retain the same exact additional-context envelope path, producing one
  normalized card rather than duplicate attachment cards.

  ### AMENDMENT (owner bug, conductor-acked, 2026-08-30) — bus delivery cursors clear queued input

  The hcom bus's per-recipient delivery cursor is first-class queue-clearing
  evidence; normalized transcript deliveries remain independent corroboration.
  A candidate is first proven addressed to the resolved recipient instance or
  its exact roster base name by the message event's `delivered_to` list. It is
  not queued when that concrete recipient's newest `status` event with a
  `deliver:*` context carries `position` greater than or equal to the candidate
  message ID. The position is the recipient's consumed bus watermark, so this
  rule is independent of whether Claude recorded delivery through
  UserPromptSubmit, PostToolUse, Stop feedback, or another transcript shape.

  A delivery status row's `msg_ts` identifies the final message in its delivery
  batch and is corroboration only; it is not a per-message join key. One batch
  may advance past several addressed messages. A message addressed to multiple
  recipients clears independently from each recipient's own status watermark;
  `delivered_to` alone remains addressing evidence, not consumption evidence.
  Failure to read delivery statuses omits the optional `queued` fact rather
  than guessing. A successful query with no delivery watermark retains the
  transcript-only rules above. The existing message subscription, multiplexed
  invalidations, client rendering, and operator-only candidate admission are
  unchanged; no delivery stream is added.

  The web pins the operator-only queued block immediately above the composer,
  outside the scrolling transcript, so it remains visible during live follow.
  In clean view, consecutive machinery entries collapse into one expandable
  horizontal pill strip. Consecutive uses of the same tool aggregate their
  count; non-operator conversation deliveries join the strip as individually
  sender-labeled message pills, while exact web-operator deliveries remain full
  cards and launcher/ack traffic remains hidden. Expanding a strip renders its
  source entries with the normal entry renderer. Clean-view and show-system
  preferences are both stored per agent in browser localStorage under separate
  `herder.web.*.v1:` keys; this is client-only state.

  ### AMENDMENT (owner bug, conductor-acked, 2026-08-27) — retired agents retain read-only transcripts

  Agent identity resolution is live-first: an exact live roster row always
  wins when a name has been reused. When no live row exists, an exact retained
  `hcom list --stopped <name>` record is server-authoritative retirement
  evidence. Detail remains 200, carries `bus_status:"retired"`, no pane, and
  uses only the tool, directory, immutable session identity, transcript path,
  and session-derived facts that remain provable. `queued` is always omitted
  for a retired agent: input not delivered before retirement can never become
  deliverable, so an advancing queued age would be a false promise.

  The canonical entries endpoint resolves a proven retired row through the
  same immutable Claude/Codex resolver and continues serving the transcript
  read-only. The existing single multiplexed EventSource likewise continues
  checking that retained file without reporting retirement itself as a
  substrate failure; no stream is added. `POST .../message` never targets the
  stopped row and returns 409 `retired agent` with an honest read-only detail.

  Retention is explicitly bounded by evidence, not promised forever: retired
  resolution lasts only while both hcom's stopped-event history and the
  immutable session file remain available. A session ID remembered by a
  browser is not server-authoritative when the live and stopped records are
  both absent and therefore never authorizes a read. In that nothing-provable
  case detail and entries retain the structured 404, while the client renders
  an honest tombstone inside the still-closable tab.

  Every client error banner has an independent dismiss control. Closing an
  agent tab also removes any `transcript:<name>` problem for the dropped
  subscription. Agent panels, including tombstones, never replace or cover the
  shell-owned close control, so no server response can wedge the page.

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
  `from` is a 400. Refusals: 404 unknown bus agent; 409 `no independent transcript` for a proven subagent whose dedicated transcript cannot be resolved; 409 no resolvable Claude
  session (wrong tool, missing/invalid ID, or absent derived file); 502 bus,
  filesystem, or other substrate failure.

  For a proven Claude Task subagent, the immutable transcript identity is
  `subagent:<agent_id>` and is returned in the existing `sessionId` field for
  the same cursor/reset semantics. The server validates `agent_id` against the
  admitted strict hexadecimal Task-ID shapes before any path construction. It
  resolves the dedicated
  `<parent-session>/subagents/agent-<agent_id>.jsonl` file from the exact parent
  roster/session relationship and explicitly renders that file's
  `isSidechain:true` records. Ordinary Claude session reads continue to skip
  sidechain records. If hcom later supplies `transcript_path`, it is preferred
  only when the target exists, names the exact validated agent, and resolves
  inside `$HOME/.claude/projects`; otherwise the server falls back to the
  parent-derived path. An arbitrary or escaping roster path is never read.

### AMENDMENT (owner-ratified, conductor-acked, 2026-08-28) — opaque-root file reads and resolution

The server's readable file universe consists only of opaque absolute roots.
It derives live roots from non-empty working directories in the current hcom
roster and accepts additional operator roots through repeatable
`herder serve --root PATH` flags. Flag order is configuration order. Relative
flag values are made absolute from the serve invocation directory; every
configured value is cleaned, symlink-resolved, and required to name an
existing directory at startup. Invalid configured roots prevent startup.
Exact duplicates collapse first-wins, but configured roots remain independently
addressable when path-nested.

Agent working directories are cleaned, symlink-resolved, and de-duplicated into
an enclosing agent root when path-nested. A working directory proven by Git to
be inside a linked worktree is never folded into another root, even when
path-nested; exact identical working directories still coalesce. The server has
no mission-, plan-, or repository-name-specific root knowledge. Root IDs are
the resulting canonical absolute directory strings; a client-supplied absolute
path that is not an exact current root ID never authorizes a read.

GET `/api/resolve?q={mention}&agent={optional-live-bus-name}`
or `/api/resolve?q={mention}&root={opaque-root}&path={viewed-file-relative-path}`
  Resolves a required, non-empty path-like query against the complete current
  root universe. Success is
  `{"candidates":[{"root":"/opaque/root","path":"relative/file.md","kind":"file|dir","tier":"exact|prefix|suffix|fuzzy","score":731}],"roots":[{"root":"/opaque/root","status":"complete|degraded|failed","detail":"honest bounded diagnostic when not complete"}]}`.
  Candidates are already ranked by the resolver and the server never applies
  the client auto-open rule or re-ranks them. The tier is therefore preserved
  exactly: the client may auto-open only when exactly one candidate is
  `exact` or `suffix`; it never auto-opens a fuzzy candidate. No current match,
  including a formerly mentioned path that has vanished, is an honest 200 with
  `candidates:[]`.

  A file context is the exact current opaque root plus the viewed file's
  root-relative path. It adds hard bands ahead of global ranking: a mention
  beside the viewed file, then the same mention beside each ancestor directory
  through the root, nearest first. A candidate appears only in its earliest
  band. After those bands, the ordinary global ranking below is unchanged.
  An explicit `./` or `../` mention is stricter: it is cleaned exactly against
  the viewed file's containing directory and never falls back to ancestors or
  global matches. If that exact target is absent, or cleaning would escape the
  root, success is an honest response with `candidates:[]`.

  Hard global ranking bands are exact, then prefix/suffix, then fuzzy. Within a
  band, an optional agent context prefers that agent's mapped live root, then
  configured roots in flag order, then remaining live agent roots. Without an
  agent, configured roots lead. `q`, `agent`, `root`, and `path` may each appear
  at most once. `root` and `path` are a non-empty pair and cannot be combined
  with `agent`; `path` must be relative and stay lexically inside the root.
  Malformed or empty input is 400 `bad request`, an unknown agent is 404
  `unknown agent`, and a file-context root outside the current readable
  universe is 404 `unknown root`. Each root reports its index outcome:
  `complete` means its candidate set is whole; `degraded` means usable candidates are included but
  an indexing diagnostic occurred; `failed` means that root contributed no
  candidates. Non-complete outcomes carry an honest diagnostic, bounded at
  4 KiB with an explicit truncation marker. Healthy roots remain ranked and
  returned when another root degrades or fails. A whole-response 502
  `substrate unreachable` is reserved for request-level failure such as an
  unreadable live roster or unavailable root-universe construction.

  Directory candidates are the unique non-root ancestors of indexed files,
  derived during the same candidate-index rebuild. There is no second walk or
  directory source, empty directories are consequently absent, and the root
  itself is not a candidate. Files and directories use the same tier ladder,
  root preference, and raw fzf score. Inside one tier and root, ranking applies
  a one-point directory deduction without changing the raw `score` returned on
  the wire; existing deterministic candidate order breaks a remaining tie.
  Absolute queries strip each containing root exactly as file queries already
  do, then match the relative remainder symmetrically against both kinds. A
  query equal to the root remains an honest miss.

GET `/api/backlog?root={root-id}&path={root-relative-directory}`
  Reads the board facts for any Backlog.md directory inside the opaque root
  universe. `root` and `path` are each required exactly once; `path` may be
  empty to address the root directory itself. A directory is a board only when
  it contains a parseable `config.yml` with non-empty `statuses` and a readable
  `tasks/` directory. Success is
  `{"root":"/opaque/root","path":"backlog","statuses":["To Do","In Progress","Done"],"tasks":[{"id":"TASK-49","title":"Folder panel","status":"To Do","ordinal":49000,"labels":[],"priority":"low","assignee":[],"created_date":"2026-08-28 08:03","updated_date":"2026-08-28 09:00","file":"tasks/task-49 - Folder-panel.md"}],"unparsed":[{"file":"tasks/broken.md","reason":"parse task frontmatter: ..."}],"truncated":false,"fetched_at":"2026-08-28T09:00:00Z"}`.

  Only `statuses` is read from `config.yml`. Only `id`, `title`, `status`,
  `ordinal`, `labels`, `priority`, `assignee`, `created_date`, and
  `updated_date` are read from task YAML frontmatter; a field absent in the
  source task is absent in its wire task. Unknown YAML keys are ignored. Task
  bodies are never read into the response: `file` is relative to the board
  directory and combines with `path` to open through `/api/files`. Explicit
  empty list fields remain empty lists.

  Task files are regular `.md` files directly inside `tasks/`, selected in
  deterministic filename order. At most 2,000 are selected; additional task
  files set `truncated:true`. Selected tasks are returned by ascending ordinal
  (missing ordinal last), then ID and file. Duplicate IDs remain distinct
  records. Each selected file has a 64 KiB frontmatter cap. An unclosed,
  oversized, syntactically invalid, or projected-type-invalid frontmatter
  quarantines that whole file in `unparsed` with its filename and honest reason;
  it is never silently dropped and still consumes one selected-file slot.
  `tasks` and `unparsed` are explicit arrays, including on an empty board.

  A readable directory that does not prove this format is an honest 200 rather
  than an error:
  `{"root":"/opaque/root","path":"ordinary-dir","backlog":{"status":"unavailable","reason":"directory does not contain config.yml"},"fetched_at":"2026-08-28T09:00:00Z"}`.
  Missing `config.yml`, missing `tasks/`, malformed or oversized config, and an
  empty configured status vocabulary use this unavailable shape with the
  concrete reason. No partial task facts accompany it.

  The endpoint inherits the file endpoints' containment and refusal law:
  unknown roots and missing requested paths are 404; traversal, `.git`
  internals, symlink escapes, and wrong file/directory kinds are 409 `refused by
  substrate`; missing or duplicate query parameters are 400 `bad request`; and
  other filesystem failures are 502 `substrate unreachable`. Every refusal uses
  the pinned `{"error":"<short>","detail":"<honest>"}` serializer.

GET `/api/files?root={root-id}&path={root-relative-file}`
  Reads one regular file exactly as it exists at request time. A text response
  is
  `{"root":"...","path":"relative/file.md","content":"...","binary":false,"size":123,"truncated":false,"fetched_at":"2026-08-28T02:30:00.000000731Z"}`.
  `size` is the complete current byte size. Text content has a 256 KiB soft cap,
  is shortened only on a UTF-8 boundary, and reports `truncated:true` when
  shortened. A binary response is
  `{"root":"...","path":"relative/file.bin","binary":true,"size":123,"fetched_at":"..."}`;
  `content` and `truncated` are absent rather than fabricated. Files above the
  4 MiB hard cap are never served.

GET `/api/files/tree?root={root-id}&path={optional-root-relative-directory}`
  Lists exactly one directory level; an absent or empty path means the root.
  Success is
  `{"root":"...","path":"relative/directory","entries":[{"name":"file.md","kind":"file","size":123},{"name":"child","kind":"directory"},{"name":"alias","kind":"symlink"}]}`.
  Entries are name-sorted. `kind` is `file`, `directory`, or `symlink`; `size`
  appears only for regular files. Symlinks are identified without following
  them merely to construct a listing. A requested directory symlink is resolved
  and containment-checked before it is read.

Both file endpoints require one exact current root ID and root-relative paths.
They resolve the root and requested target through the shared containment
primitive before reading. A symlink escape is 409 `refused by substrate` and
its detail quotes the requested and resolved paths. Traversal, explicit or
symlink-aliased `.git` internals, the 4 MiB hard cap, and wrong file/directory
kind are likewise 409 refusals. Unknown roots and missing paths are 404;
malformed or duplicate parameters are 400; other filesystem failures are 502.
Every refusal retains the pinned `{"error":"<short>","detail":"<honest>"}`
serializer. Dotfiles other than `.git` internals are readable. Attributed and
unattributed viewers have the same read access, and no file write exists.

### AMENDMENT (owner-ratified, conductor-acked, 2026-08-28) — bounded Git reads

Four read-only Git endpoints extend the opaque-root file universe. They compute
facts on demand and hold no persistent state. Each path is root-relative and
inherits the file endpoints' traversal, `.git`-internal, and symlink-escape
refusals. A root nested inside a repository remains its own readable universe:
status and diff pathspecs are scoped to that root and wire paths remain relative
to it. Git output is hand-parsed from its documented byte-oriented machine
formats; an unrecognized record is unavailable or a 502 substrate failure,
never guessed. Mutable successes carry `fetched_at`.

"Read-only" here means the API never changes source content, refs, commits, or
the staged set. Git status may still refresh and write the index stat cache as
benign repository metadata; `--no-optional-locks` does not prevent that write,
so the record does not claim byte-for-byte repository immutability.

GET `/api/git/status?root={root-id}&base={optional-uncommitted|branch}`
  Returns the root-scoped changed-file list from
  `git status --porcelain=v2 -z --branch`. Success is:
  `{"root":"/opaque/root","repo":{"branch":"feature/x","head":"<full-sha>","upstream":"origin/feature/x","ahead":3,"behind":1,"branch_base":{"status":"available","default_ref":"origin/main","default_sha":"<full-sha>","merge_base":"<full-sha>","commits_ahead_of_base":5}},"entries":[{"path":"new/name.md","kind":"renamed","old_path":"old/name.md","staged":true,"unstaged":false,"index_kind":"renamed","additions":12,"deletions":3,"binary":false}],"fetched_at":"2026-08-28T15:00:00.000000731Z"}`.

  `kind` is `modified`, `added`, `deleted`, `renamed`, `copied`,
  `untracked`, `conflicted`, or `type_changed`; conflicted takes precedence.
  `index_kind` and `worktree_kind` independently use the tracked kinds and are
  omitted when that side is clean, so a staged-plus-unstaged file is never
  collapsed. `entries` is an explicit array. Optional repository and entry
  facts are omitted when Git cannot prove them.

  Omitting `base` preserves the original response byte shape and the original
  uncommitted-only `entries`. When `base` is explicit, success also carries an
  `entries_base` object. `base=uncommitted` returns
  `{"kind":"uncommitted","sha":"<HEAD-full-sha>","label":"HEAD"}`.
  `base=branch` returns
  `{"kind":"branch","sha":"<merge-base-full-sha>","default_ref":"origin/main","label":"merge-base with origin/main"}`
  and enumerates the working tree against that merge-base. If `branch_base` is
  unavailable, the response stays HTTP 200, preserves its explicit unavailable
  reason, and falls back to uncommitted entries with an uncommitted
  `entries_base`. Missing-base compatibility is deliberate; an empty, duplicate,
  or unrecognized explicit base is 400.

  Branch entries union tracked `git diff --raw -z --find-renames
  --find-copies-harder <merge-base>` facts with porcelain's untracked paths.
  Their `kind` describes the whole working-tree change relative to the
  merge-base. For a committed-only row, `staged` and `unstaged` are both false
  and `index_kind`/`worktree_kind` are omitted: those fields describe only the
  checkout's current porcelain state and are never overloaded to mean
  "committed." Renames and copies retain an honest `old_path`. A dirty tracked
  row combines its merge-base-relative `kind` with its actual staged/unstaged
  flags, so branch entries cover committed, staged, unstaged, and untracked
  work without conflating those states.

  Per-entry `additions`, `deletions`, and `binary` use the effective entries
  base: **HEAD** for uncommitted entries and the proved merge-base for branch
  entries. Both probes are root-scoped `git diff --numstat -z --find-renames
  --find-copies-harder <base>`. Untracked files have no fabricated counts.
  Failure of the optional HEAD numstat probe omits only those three facts and
  does not discard porcelain status truth; a failed branch enumeration is an
  honest Git-unavailable status rather than a partial branch list.

  `branch_base` is proved only from the symbolic `origin/HEAD`, its resolved
  commit, and `merge-base HEAD refs/remotes/origin/HEAD`. Missing or
  unprovable evidence is explicit, for example
  `{"status":"unavailable","reason":"origin/HEAD is not configured"}`.
  When that proof is available, optional `commits_ahead_of_base` is the
  non-negative result of `git rev-list --count <merge_base>..HEAD`. It is the
  commit count since the proved default-branch merge-base and is deliberately
  distinct from top-level `ahead`, which remains porcelain's count against the
  configured upstream. Either may exist without the other and clients must not
  conflate them. If the count cannot be proved, it is omitted.
  Detached HEAD omits branch/upstream/ahead/behind but may carry `head`. A root
  that is not a Git repository, or whose essential porcelain status cannot be
  read, is an honest HTTP 200 rather than a page-breaking error:
  `{"root":"/opaque/root","git":{"status":"unavailable","reason":"not a git repository"},"fetched_at":"..."}`.

GET `/api/git/diff?root={root-id}&path={relative-file}&base={uncommitted|branch|commit}&sha={required-for-commit}`
  Returns one capped raw Git patch plus separately parsed header facts. Success
  is:
  `{"root":"/opaque/root","path":"new/name.md","base":{"kind":"branch","sha":"<merge-base-full-sha>","default_ref":"origin/main","label":"merge-base with origin/main; includes committed and uncommitted work"},"facts":{"kind":"renamed","old_path":"old/name.md","binary":false,"old_mode":"100644","new_mode":"100755"},"stats":{"additions":12,"deletions":3},"patch":"diff --git ...\n","patch_bytes":731,"truncated":false,"fetched_at":"..."}`.

  `uncommitted` compares the index and working tree with this checkout's own
  HEAD and identifies its base as
  `{"kind":"uncommitted","sha":"<HEAD-full-sha>","label":"HEAD"}`.
  `branch` compares the same checkout, including its committed and uncommitted
  work, with the proved merge-base of its own HEAD and symbolic `origin/HEAD`.
  Every proof and diff command runs with `git -C` set to that linked checkout;
  the main checkout and guessed branch names are never consulted.

  `commit` compares one exact commit with what immediately preceded it: an
  ordinary commit with its sole parent, a merge commit with its first parent,
  and a root commit with Git's empty tree. It requires `sha` to be a full SHA
  that peels to a commit; invalid, abbreviated, unknown, and non-commit values
  are 404s exactly as for `/api/git/file`. The response base is respectively
  `{"kind":"commit","sha":"<peeled-full-commit-sha>","label":"commit vs parent"}`,
  `{"kind":"commit","sha":"<peeled-full-commit-sha>","label":"merge commit vs first parent"}`,
  or `{"kind":"commit","sha":"<peeled-full-commit-sha>","label":"root commit vs empty tree"}`.
  Commit mode uses the same `--find-renames --find-copies-harder` copy
  detection, structured facts, patch caps, and refusal behavior as mutable
  diff. A successful commit response omits `fetched_at` and sets
  `Cache-Control: public, max-age=31536000, immutable`; the `sha` parameter is
  refused on mutable `uncommitted` and `branch` requests.

  `facts.kind` is `unchanged`, `modified`, `added`, `deleted`, `renamed`,
  `copied`, or `type_changed`. `old_path` appears for a proved rename/copy;
  modes appear only when changed; `binary` is explicit; `stats` is absent when
  Git reports a binary change. `patch` is the Git-produced raw patch text for
  `PatchDiff`; the server does not parse or normalize its hunks or line
  endings. An unchanged file has an empty patch and zero stats.
  Repository-configured external diff and text-conversion helpers are disabled;
  serving a diff never executes a repository-provided formatter.

  Every Git subprocess also forces an empty `core.hooksPath` and
  `core.fsmonitor=false`, preventing status refresh hooks and configured
  filesystem-monitor commands from executing. **KNOWN defense-in-depth
  limitation:** `filter.<driver>.clean` cannot be closed by an enumerable set of
  `-c` overrides because the driver name is repository-controlled. Under the
  current deployment this remains in the same unreachable tier as the other
  hostile `.git/config` cases: changing that file already requires the serving
  OS user's authority.

  Fleet currently runs agent seats and the serve process under the same OS
  identity. If fleet ever runs agent seats under an identity less privileged
  than the serve process, repository-configured execution becomes a blocking
  privilege-boundary issue; this entire class, including
  `filter.<driver>.clean`, must be revisited before those roots are served.

  Patch stdout has a 256 KiB soft cap and 4 MiB hard cap. The server retains
  at most the soft cap while draining and counting through the hard cap, so
  memory is O(soft cap). A soft-capped response backs up only to a valid UTF-8
  boundary, sets `truncated:true`, and reports the complete byte count in
  `patch_bytes`; separately parsed facts and totals remain honest. Crossing
  the hard cap kills Git, discards the partial body, and returns a 409 pinned
  refusal. An unavailable requested branch base is a 409
  `{"error":"base unavailable","detail":"..."}`; malformed parameters are
  400, unknown roots/paths are 404, containment refusals are 409, and
  unexpected Git/parser failures are 502.

GET `/api/git/log?root={root-id}&path={relative-file}&cursor={optional}`
  Returns single-file history, newest first, following renames:
  `{"root":"/opaque/root","path":"new/name.md","entries":[{"sha":"<full-sha>","author":"Fixture","date":"2026-08-28T12:34:56Z","subject":"rename file","path_then":"new/name.md"}],"next_cursor":"<opaque>","history_truncated":true,"fetched_at":"..."}`.
  The page size is fixed at 50; `entries` is explicit and each entry contains
  exactly sha/author/date/subject/path_then. `path_then` is the root-relative
  historical path reported by the same rename-following log traversal for that
  commit (the rename/copy destination at its commit); clients use it when
  requesting `/api/git/file` or the immutable commit diff across a rename
  boundary. `next_cursor` is absent at end, and an
  untracked/no-history file is an honest 200 with `entries:[]`.

  The cursor is an opaque, versioned, base64url token containing the first
  page's fixed HEAD anchor, next skip offset, and a SHA-256 binding to the
  repository, opaque root, and root-relative path. Its fields, version, SHA,
  page-aligned bounded skip, and binding are validated; offsets the server
  could not have issued for that anchored history are refused. Each page reruns
  one anchored `git log --follow --find-renames --name-status -z --no-ext-diff
  --no-textconv --max-count=1001` traversal **without `--skip`**, parses the
  continuous rename-following stream, and slices it in memory at the cursor
  offset. Git therefore carries rename state across page boundaries, and the
  same machine name-status record supplies `path_then` before pagination
  without changing cursor anchoring or binding. Using `cursor^` is forbidden
  because it can lose the rename boundary and historical path. The server has
  no cursor store.

  At most 1000 history entries are served for one anchored path. The 1001st
  record is a probe proving that older history exists; it is never returned.
  When the probe exists, every page includes `history_truncated:true`, the
  cursor advances normally through the page beginning at offset 950, and that
  terminal capped page deliberately omits `next_cursor`. Thus a cursor never
  claims that entries beyond the cap are available. `history_truncated` is
  omitted for histories of 1000 entries or fewer, preserving the existing wire
  shape for ordinary and short first-page consumers.

  The complete machine-format log stream also has a 4 MiB hard cap. Crossing
  it kills Git, discards the partial stream, and returns a 409 refusal; partial
  entries and cursors are never fabricated. This bounds repository-controlled
  author, subject, and historical-path metadata independently of the 1001-entry
  walk limit.

GET `/api/git/file?root={root-id}&path={relative-file}&sha={full-commit-sha}`
  Reads a blob at an exact commit without changing `/api/files`' as-of-now
  meaning. Text success is
  `{"root":"/opaque/root","path":"old/name.md","sha":"<full-commit-sha>","content":"...","binary":false,"size":123,"truncated":false}`;
  binary success is
  `{"root":"/opaque/root","path":"img.bin","sha":"<full-commit-sha>","binary":true,"size":123}`.
  Text, binary, UTF-8-safe truncation, and 256 KiB/4 MiB caps exactly match
  `/api/files`. Invalid/unknown SHAs and paths absent at that commit are honest
  404s. Successful responses omit `fetched_at` and set
  `Cache-Control: public, max-age=31536000, immutable` because the SHA-addressed
  representation cannot change.

Agent detail additionally carries top-level `cwd` and optional
`git:{branch,remote_url,worktree_of}` derived read-only from its current roster
working directory; the existing `directory` field remains compatible. Every
fleet workspace carries the same fields only when Herdr supplies a live
`worktree.checkout_path`. `worktree_of` inside `git` is a proven main-checkout
path for a linked Git worktree and is distinct from the existing workspace-level
`worktree_of` parent workspace ID. Detached heads, missing origins, non-Git
directories, and unavailable facts are honestly omitted rather than inferred
from labels, paths, or branch-looking text.

### AMENDMENT (owner-ruled, 2026-08-27) — legacy exchange endpoints removed

The exchange-based `/api/agents/{bus-name}/transcript` and
`/api/agents/{bus-name}/transcript/stream` endpoints are removed. They were no
longer used by the web shell and duplicated the canonical immutable entries
endpoint plus multiplexed entry wake frames. Both legacy paths now receive the
standard structured 404 unknown-endpoint refusal. This amendment records the
conductor-authorized TASK-12 owner ruling of 2026-08-27.

GET `/api/events?agents={comma-separated-bus-names}&screens={comma-separated-herdr-pane-ids}&watches={JSON-array}&focused_screen={one-requested-pane-id}` (SSE)
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
  - `file-change`: a file or folder change fact shaped as
    `{"kind":"file|folder","root":"<opaque-root-id>","path":"<root-relative-path>"}`.
    It carries no content. The shell invalidates the exact current-file query
    for a file fact, or the exact one-level tree and Backlog queries for a
    folder fact; those read endpoints remain truth;
  - `substrate`: a named source went unreachable/recovered (the UI
    must render this as a banner, not an empty board — the honesty
    rule in stream form);
  - `ping`: an empty client-visible heartbeat paired with the required
    SSE comment heartbeat (`: ping`) every 15s.
  Sourced from hcom's event subscription plus herdr snapshot polling
  (proposed: 2s) and one filesystem-woken transcript entry tailer set per
  subscribed page-set. The shell owns reconnect: it closes any errored
  EventSource, rebuilds with bounded backoff and no stale cursor, and
  rebuilds if neither data nor heartbeat arrives for 45s. Its
  `reconnecting` indicator is present only while a rebuild is scheduled
  or in flight.

  ### AMENDMENT (owner priority, conductor-acked, 2026-08-29) — live open-file/folder freshness

  `watches` is an optional JSON array ordered oldest-to-newest. Each item is
  `{"kind":"file|folder","root":"<exact-current-root-id>","path":"<root-relative-path>"}`.
  The client derives it only from mounted mutable file reads and explicitly
  open folder listings, coalescing layout-restore registration bursts before
  rebuilding this same EventSource. Hidden dock tabs remain open; unmounted or
  replaced views are removed. Expanded folder nodes are individual opt-in
  watches and never authorize or create a recursive tree watch.

  One real fsnotify watcher is owned by each SSE request. File targets watch
  their resolved parent directory and filter to the exact filename. Folder
  targets watch the resolved directory itself for direct entry changes. Equal
  directories are de-duplicated within that connection. Two viewers have
  independent connection state and never mutate one another's watch set.

  A connection retains at most 64 distinct watched directories. Declaration
  order is eviction order: when more are requested, the newest 64 directories
  remain watched and older views retain manual refresh. The decoded declaration
  is capped at 64 KiB and 256 logical targets. Create, write, rename, remove,
  and chmod bursts are coalesced per logical target with a trailing 120ms quiet
  window before one `file-change` fact is emitted.

  Resolution, registration, runtime, cap, and teardown failures are silent UI
  degradations to the existing manual Refresh behavior: they never produce a
  watch error banner or replace an otherwise healthy fleet stream. Closing the
  request context closes its fsnotify watcher and drops the complete ephemeral
  set. Changes/Git panels, transcript panels, recursive tree freshness, editing,
  and file writes remain outside this amendment.

  ### AMENDMENT (conductor-accepted TASK-77, 2026-08-31) — transcript tails wake on session-file writes

  Each SSE request with subscribed agents owns a separate transcript fsnotify
  watcher. Claude main sessions, proven Claude Task sidechains, and Codex
  rollout files all watch the resolved session file's parent directory and
  filter events to the exact path, so in-place appends and atomic replacement
  both wake the existing entry tail. A trailing 120ms quiet window coalesces a
  write burst into one affected-agent tail pass. Session identity changes on
  the existing fleet cadence rebuild the resolved watch path and preserve the
  existing `rewindow` contract.

  The 2s fleet cadence never tails an unchanged transcript. A separate 30s
  server-side safety sweep bounds staleness after watcher setup, registration,
  or runtime failure; those failures are audited and retried by the safety
  path. No client transcript poll is added. EventSource reconnect still
  invalidates each subscribed cursor query, catching up writes missed while
  disconnected. `entry:{bus-name}` remains an immutable entry payload, but the
  client deliberately treats it only as an invalidation signal and coalesces a
  multi-entry frame burst into one cursor fetch.

  ### AMENDMENT (owner-rescoped 2026-08-27, expanded 2026-08-28) — read-only terminal screens (superseded for terminal surfaces by TASK-73 below)

  Screen view is permitted as a read-only convenience for panes currently
  reported by Herdr. Standalone screen tabs remain offered for panes without
  an agent claim and use the unattributed-terminal wording ruled above. A
  watched pane may acquire an agent without its already-open read-only screen
  being revoked. The server's readable scope is every currently visible Herdr
  pane; it does not infer ownership or restrict reads by client presentation.

  Each agent tab additionally offers an opt-in `Transcript` / `Screen` switch.
  `Transcript` is the default. `Screen` is enabled only while that exact agent
  detail has a proven live pane and mirrors that pane inside the agent tab; a
  pane move updates the selected pane. An unplaced agent says `No live pane.`
  and disables the switch. A retired agent says it has no live pane and remains
  transcript-only. Switching the viewport does not replace the queued-message
  dock or composer: the composer continues to send bus messages only. Preview,
  pinning, close, clean-view, and show-system semantics are unchanged.

  `screens` is an independent, de-duplicated set of at most 100 exact Herdr
  pane IDs whose standalone screen tabs or opted-in agent screen viewports this
  page currently has open. The client folds those IDs and the open `agents`
  set into the same one EventSource query; it never creates a second stream.
  The server validates every requested ID against each current Herdr
  snapshot and never accepts an arbitrary target. A pane absent from the live
  snapshot is not read. Its `screen:{pane-id}` frame instead becomes
  `{"pane_id":"...","status":"unavailable","text":"","truncated":false,"detail":"..."}`,
  actively clearing any prior client snapshot.

  Available frames replace the complete client snapshot:
  `{"pane_id":"...","revision":731,"status":"available","text":"...","truncated":false}`.
  They come only from Herdr's read-only `pane.read` RPC with `source=visible`,
  `format=text`, and `strip_ansi=true`; plain ANSI-stripped text is deliberate
  v1 scope. This is distinct from `hcom term`, which queries hcom's
  bidirectional per-agent PTY inject port. The web server must never connect to,
  proxy, or expose that inject port.

  Herdr exposes no general terminal-output change subscription: its
  subscribable `pane.updated` family tracks pane metadata/title changes, while
  `pane.output_matched` is one-shot pattern matching rather than a screen wake.
  Each SSE handler therefore polls only its requested-and-still-visible panes
  every 250ms, suppresses unchanged text, and emits at most four frames per
  second per pane. The existing 2s live-snapshot poll separately revalidates
  pane IDs and reconciles disappearance and recovery. Unrequested and no-longer
  visible pane IDs are filtered before any `pane.read`.
  Frames are full snapshots rather than diffs: measured real visible panes
  were 4,081 bytes median and 6,813 bytes maximum, so stateless replacement is
  cheaper and safer than maintaining a reconnect-sensitive diff base.

  The complete serialized SSE frame, including `event:`/`data:` framing, has a
  hard 16,384-byte limit. The server measures final bytes and UTF-8-safely
  truncates text when required, setting `truncated:true`. Screen frames never
  enter transcript endpoints, transcript entries, or transcript serializers.
  Opening or closing a standalone screen, or opting an agent viewport in or
  out, rebuilds the existing single EventSource with the new set; no second
  stream exists. Every screen surface is a read-only `pre` with no input, key
  handler, terminal-write endpoint, or browser path to Herdr input. Screen
  reads use only `pane.read` as pinned above. The server must not connect to or
  expose hcom's per-agent inject port. The composer/bus remains the only web
  typing path.

  ### AMENDMENT (owner-ruled, 2026-08-29) — first-class ANSI read/write terminals (TASK-73)

  This amendment supersedes the terminal-specific read-only, plain-text,
  16 KiB, and no-input constraints immediately above. It does not widen any
  other mutation surface. Terminal reads and writes use Herdr's own Unix
  socket only; the server still must never connect to, proxy, or expose hcom's
  per-agent PTY inject port.

  Available `screen:{pane-id}` frames are full ANSI snapshots from
  `pane.read` with `source=visible`, `format=ansi`, and `strip_ansi=false`.
  They additionally carry `cols` and `rows` from Herdr's current layout. The
  final serialized SSE wire frame is capped at 65,536 bytes. Oversized text is
  truncated only at an ANSI-ground-state line boundary, with
  `truncated:true`; an oversized single line may honestly become an empty
  prefix. The browser renders snapshots by resetting and repainting a
  zero-scrollback xterm. It never invents local echo, cursor position, or pane
  geometry, and it never resizes the shared Herdr pane. Fit measurement may
  adjust browser font size, but xterm's rows and columns remain the real Herdr
  grid.

  `focused_screen` is an optional scalar in the existing `/api/events` query.
  When present it must name one of the de-duplicated `screens` values or the
  request is refused with 400. The focused pane polls at 100 ms; all other
  watched panes remain at 250 ms. Agent, screen, file-watch, and focus facts
  continue to share one EventSource, rebuilt on focus transitions.

  `GET /api/panes/{pane-id}/history` validates the target against the current
  Herdr snapshot and reads at most 2,000 recent ANSI lines. It returns
  `pane_id`, `text`, `truncated`, and an RFC3339 `fetched_at`. History is a
  separate, static, refreshable xterm surface with bounded scrollback; live
  snapshot repaints never accumulate into it.

  `POST /api/panes/{pane-id}/input` accepts exactly one of `{"text":"..."}`
  or `{"keys":["..."]}` in a JSON body capped at 8 KiB. Empty input,
  undocumented fields, and both/neither forms are 400. A target absent from
  the current Herdr snapshot is 404; a pane that disappears before
  `pane.send_input` accepts the request is 409; attribution and substrate
  failures use the existing pinned 409/502 refusal serializer. The browser
  serializes requests per pane. Exact, live-proven single-key xterm chunks map
  through one encoder table: CR→`enter`, DEL→`backspace`, Tab→`tab`,
  Ctrl-C→`ctrl+c`, Ctrl-D→`ctrl+d`, Esc→`escape`, CSI A/B/C/D→the four arrows,
  and Esc-b/Esc-f→Alt-B/Alt-F. Herdr currently refuses `home`, `end`, and
  `delete` with `502 invalid_key`; those xterm chunks are intentionally left
  unmapped rather than approximated with readline-only control chords. All
  other chunks are one unchanged `text` request. In particular, content
  containing an embedded CR and bracketed/multiline paste is never split into
  key presses. Each attributed request produces one audit record containing
  only time, viewer, pane, and byte count—never terminal content. Success returns
  `{"sent":true,"pane_id":"...","viewer":"..."}`.

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
  message. This message endpoint never injects pane input; TASK-73's separately
  attributed and bounded terminal endpoint is the only pane-input exception.

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
  Body: `{"tool": "claude" | "codex", "model": "<optional>",
  "tag": "<optional, default impl>", "repo": "<optional absolute path>",
  "branch": "<optional>"}`. A blank repo means the ai-config root that
  launched this Herder. A blank branch generates
  `launch-<tool>-<yyyymmdd-hhmm>`. Every request delegates to
  `tools/fleet/spawn.sh` with `--worktree-branch` and `--repo`; the web
  server never owns the launched process and never uses `--split-from`.

  HTTP 200: `{"names":["<bus-name>"],"output_tail":"<spawn output>"}`.
  Semantic spawn refusals are HTTP 409 with `error: "launch refused"`
  and spawn.sh's stderr preserved as `detail`; wrapper/infrastructure
  failures are 502. After success, one attributed launch edge per name is
  appended to `launch-edges.jsonl` under the Herder state directory. The
  new agent then appears through the ordinary fleet SSE poll; no endpoint
  response is used as fleet state.

### AMENDMENT (owner-ruled, 2026-08-26) — web fork removed

The owner ruled that the web fork control does not work and breaks sessions.
The client control and `POST /api/agents/{bus-name}/fork` endpoint are removed;
the path now receives the standard 404 unknown-endpoint refusal. This removes
only web fork. Worktree-only spawn remains available, and lifecycle operations
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

No WebSocket. No pane injection other than the attributed, bounded Herdr
`pane.send_input` terminal endpoint defined by TASK-73 above. No cull/kill. No resume or fork, no
retired-session lifecycle controls beyond the retained read-only transcript
amendment, no sesh. No blank-form/global spawn, no new-workspace
creation. No auth beyond the tailnet boundary. No server-side state.
No agent screen is selected by default. The opt-in interactive agent terminal
viewport and unattributed-terminal surface are only the narrow exceptions
defined by the 2026-08-28 expansion and TASK-73 amendment above.
There are no file writes, content grep/search endpoint,
archive/download surface, arbitrary-path read, mission-aware root behavior,
whole-repository history browser, or persistent file state. File/folder change
facts exist only through the bounded ephemeral `/api/events` exception above. The short-lived candidate index is
derived and rebuildable; restart loses no source of truth.

## Former candidate now ratified (2026-08-27)

The former non-agent terminal-pane candidate was first ratified as the narrow
read-only screen amendment, then superseded by TASK-73's ANSI snapshot mirror,
bounded history read, and attributed Herdr input endpoint. It remains polling
snapshot transport rather than a raw PTY stream or WebSocket.

## Build order inside the lane (ruled: existing sessions first)

1. `/api/fleet` + `/api/events` + board UI (read-only heart).
2. `/api/agents/*` + transcript entries view + message write.
3. Contextual spawn (web fork removed by the 2026-08-26 owner ruling).
Each step merges green behind the dark `serve` flag; UI ships
embedded (React + Vite, go:embed) per the web-lane plan.
