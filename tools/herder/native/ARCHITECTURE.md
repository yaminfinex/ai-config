# herder native — architecture

The macOS client for herder serve, on GPUI through `gpui-kit = "=0.7.0"`. What to build is the ladder in the
mission's `v0-spec.md`; the settled decisions in `playbook.md` are binding. This file fixes the shape so the
Rung 1–3 units can be built without the code ballooning. The spike (`prototypes/native-gpui`) proved the
budgets; this is its lessons, rewritten as rules.

## 1. One crate, seven modules, one-way dependencies

One crate, `herder-native` (a lib plus a two-line bin). Not a workspace: Rung 1 is a few thousand lines, and
a workspace would add manifests and feature plumbing for a boundary that review can check by eye. Split into
crates only if a module needs to be built without GPUI, which nothing does today.

```
shell ──▶ views ──▶ store ──▶ api::types        (data flows up; dependencies point down)
  │         │
  │         └──▶ views::theme (type scale, palette)
  ├──▶ api::client, api::sse   (background threads only)
  ├──▶ local                   (disk)
  ├──▶ platform_mac            (AppKit via objc2)
  ├──▶ terminal                (Rung 2)
  └──▶ harness                 (scripted runs)
```

| Module | Responsibility (one sentence) | Depends on |
|---|---|---|
| `api` | Typed wire models, blocking HTTP, the SSE connection and frame reader, `before=` paging and sends; called from background threads. | serde, ureq |
| `store` | Pure, deterministic domain state: `Store::apply(Event) -> Vec<Effect>`; no GPUI, no I/O, no clocks. `store::sync` is the per-namespace `/api/state` pull cursor and outbox (§6), shared by spaces, members and notes. | `api::types` |
| `views` | GPUI views that render from `&Store`, own their widget entities, and dispatch `Event`s; sizes only from `views::theme`. | `store`, gpui-kit |
| `shell` | Owns the store, the threads, the one channel, the window and the keymap; runs effects (`shell/io`: the REST reads and the save-then-send, off the foreground). | everything |
| `local` | `prefs.json`, `outbox.json` and `snapshot.json` under `~/Library/Application Support/herder-native/`. | `store` types |
| `platform_mac` | The AppKit calls GPUI lacks: window ordering, dock badge, activation policy, the hotkey bridge. | objc2 |
| `terminal` | A local PTY running `et`/`ssh -t`, emulated by `alacritty_terminal`, painted by a view (Rung 2). | alacritty_terminal |
| `harness` | `HERDER_NATIVE_SCRIPT` steps: keys, screenshots and metrics, never taking focus. | gpui-kit |

`tests/layering.rs` is a cheap tripwire, not proof: it fails when `store` or `api` mention `gpui`, when
`store` does I/O or reads a clock, or when `views` reach the network. Grouped imports and indirect calls slip
past it, so the reviewer still reads the `use` lines of every changed module.

## 2. Data flow and threads

- **Threads.** One `std::thread` blocks on the SSE socket (idle costs nothing). REST calls run on GPUI's
  background executor. Nothing else spawns threads.
- **One channel.** Every background result is an `Event` sent on one `futures::mpsc::UnboundedSender`. One
  foreground task (`cx.spawn`) drains it and calls `Shell::dispatch`.
- **The mutation rule.** Domain state changes in exactly one place: `Store::apply`, called by
  `Shell::dispatch` on the foreground thread. Views take `&Store` and dispatch `Event`s; they never hold
  `&mut Store`. Each view owns the GPUI widget entities it renders with (`ListState`, `TextareaState`,
  `EditorState`) as plain GPUI state; those are not domain state and never go through the store.
- **Effects.** `apply` returns what must happen next. Network and disk effects (`Fetch`, `Post`, `Message`, `Persist`)
  run off the main thread and their results come back as events. `Notify` and `Badge` are AppKit calls and
  run on the foreground inside `dispatch`. A fixture test checks both the state and the effects a reduction
  produces.
- **Boot.** Seed theme fonts, `gpui_kit::init`, open the window. The `local` snapshot is read and applied
  **synchronously before** the SSE thread or any REST call starts, so nothing stale can land on top of live
  data (`Event::Snapshot` is also refused by the store once anything live has arrived). Then in parallel:
  the SSE thread (`hello` + `fleet`), `GET /api/viewer`, and `GET /api/state/{spaces,spaces.members,notes}?since=0`.
  The viewer is `Unknown` until the server answers. Only a 409 is `Refused` (never asked again); a
  transport failure or a 5xx stays `Unknown` and is asked again on a coalesced timer (500 ms doubling to
  10 s, one timer and one request at a time) and on every `hello`, so a healthy stream that never sends
  another `hello` still recovers.
- **Stopping and reconfiguring the stream.** `api::sse::Reader` is the stream's thread: plain HTTP/1.0
  over a `TcpStream`, reconnecting with backoff and the watchdog below, reporting each frame and each drop.
  `close()` (or dropping the `Reader`) ends it promptly wherever it is: the socket is published as soon as it
  connects, so shutting it down interrupts the header read as well as a frame read, and the backoff sleep
  waits on a condvar. Not interruptible: DNS resolution of the host, which sits outside the 5 s connect
  timeout (as long as the system resolver takes), and the connect itself (5 s). The shell holds the
  current `Reader`; changing the `agents=` subscription drops it and spawns a new one under a new
  **generation** number, and the store drops anything from an older generation. Reading never relies on
  the 45 s timeout to notice anything: a healthy stream pings every 15 s and would never time out.
- **Stale results.** Every transcript request carries the transcript's `(session_id, generation)`;
  `generation` bumps on `rewindow`, on a `reset`, and when the window is thrown away. A response whose tag no
  longer matches is dropped by the store.
- **Reconnect.** Backoff 500 ms → 10 s, watchdog 45 s. On every `hello`, the first included (a change made
  between a boot pull and the stream's subscription sends no nudge this client can see): re-read each open
  transcript forward from its `next_offset`, re-pull the state namespaces from their in-memory cursors
  (pulls in flight coalesce), ask for the viewer again while it is `Unknown`, and expect a fresh `fleet`. A changed `hello.buildIdentity` shows "server updated" and never reloads by itself.
- **Transcript wakes.** One stream, subscribed with `agents=` to the agents of the zoomed space (plus a
  previewed outsider), and to none on the lens. An `entry:` frame only means "read forward from
  `next_offset`". Wakes coalesce without a timer: one forward read in flight, and a wake meanwhile sets
  `again`, which reads once more when it lands. A `message` frame addressed to the open agent refreshes its
  detail (coalesced the same way), so a queued message shows. A failed tail, forward or detail read retries
  after 1, 2 and 4 s (it and any queued wake), then waits for a wake or `hello`. Each of the two keeps its
  own budget and one pending timer, tagged with a token, so a sibling's success resets nothing and a
  superseded timer reads nothing. A failed read's notice holds paging back until that read succeeds, the
  notice is dismissed or a `hello` arrives; a failed `before=` page is not retried. An unmatched path's
  notice holds nothing.
- **Server cost.** Debounce detail refetches; skip the web client's habit of invalidating every open
  transcript on each `fleet` frame.

## 3. Core types

Wire shapes are in `api::types` and decode tolerantly (`#[serde(default)]`; new kinds become `Unknown`).
Derived shapes are in `store`:

- **`fleet::Agent`** — one row of the board keyed by bus name: tool, `herdr_status`, `bus_status`, title,
  group, parent, workspace label, pane id, context used. **`Status`** is derived, never stored:
  `Blocked` (either status says so) > `Working` (herdr `working`) > `Done` (herdr `done`) > `Idle`.
  An agent absent from the board is gone; retired detail (`bus_status: retired`) makes a transcript read-only.
- **`spaces::Space`** — `{id, name, order}` from the `spaces` namespace, tombstones dropped. **`Member`** is
  `Agent{name}` or `File{root, path}` from `spaces.members`, in dock order. Local: **`Row`** (`Focus`,
  `Watch`, `Background`) per space, the **visible agent** per space, and **seen** per agent: the board's
  `turn_end_id` (the hcom event id of the agent's latest completed turn, monotonic) the owner has seen; the
  board carries no activity timestamp. An agent seen for the first time takes its current turn as the
  baseline (web's policy: an unknown baseline is not a new turn), and marks are pruned to agents on the
  board or in a space. **Needs you** = the agent is not `Working`, not `retired` or `stopped`, and its
  `turn_end_id` is above its seen mark; `Blocked` without a new turn does not count (making it always count
  is an owner policy call for U2). The card count is the number of such agents in the space.
- **`transcript::Item`** — what compact mode renders (`store::condense` projects entries; `transcript`
  orders and pairs them): `Prompt`, `Delivery{sender, text, operator, quiet}` (`quiet`: an ack or the
  launcher, a one-line chip),
  `TaskNotification`, `SystemChip` (`injected_system`, `command_stdout`, `system_chip`, `turn_duration` fold
  here or are dropped), `CompactDivider`, `Assistant{markdown}`, `Thinking` (a collapsed pill),
  `Tool{name, summary, result: Option<ToolResult{error, text}>}`, `Error`. Assistant text has `<internal>…</internal>` removed and `<status>`
  unwrapped; the operator envelope (`[HERDER_WEB_OPERATOR_NOTE_BEGIN]…END]`) is stripped from deliveries.
- **`transcript::Transcript`** — pages arrive in both directions, so nothing is an append-only fold:
  - One wire entry can yield several items (an `hcom_delivery` entry carries every delivery of that
    injection; `grill-confirm-lubo` has three at one offset), so the row key is `(byte_offset, sub)` with
    `sub` the item's index within its entry. `items: BTreeMap<(u64, u16), Item>`; row order is key order,
    so a `before=` page is an insertion, not a prepend, and no stored index ever moves. Across sessions
    rows are `(session_id, byte_offset, sub)`.
  - `calls: HashMap<tool_use_id, (u64, u16)>` and `orphans: HashMap<tool_use_id, ToolResult>`. A
    `tool_result` whose call is already known fills that `Tool`; otherwise it waits in `orphans`, and a
    `tool_use` arriving later (from an older page) claims it. An `is_error` result marks the line ✗.
  - Cursors: `next_offset` (forward) is set only by tail and `from=` responses. `prev_offset` (backward) is
    seeded from `tail.window.from` (tail responses carry no `prevOffset`) and then from each `before=` page's
    `prevOffset`; `0` means the start of the file. A `before=` response never touches `next_offset`.
  - `generation` (see §2). `rewindow`/`reset` clears everything and re-reads the tail.
- **`notes::Note`** — the web record: `{id, group (agent or general), text, quote?, source?, created}`,
  `updated` on the row. **`Draft`** is one string per agent, local only.
- **`Prefs`** — local owner preferences: `text_scale`, rows, visible, seen, drafts, `hotkey` (U6);
  `vscode_host`, the Remote-SSH alias file links open on (default `superset`; web asks).

## 4. Keys

The kit's keymap beats raw key listeners: its Root binds `tab`, its Input binds `cmd-enter` and `escape`,
so a `capture_key_down` handler never sees them. Every binding is therefore an `action!` with a key-context
predicate. GPUI evaluates a predicate against the whole focus stack, so a binding on `Lens` also fires while
the composer inside it has focus; single-letter navigation must exclude text surfaces explicitly.

Contexts (identifiers on elements): `Lens` (the root), `Space` (the zoom shell), `Composer` (around the
composer's box, U4), `Input` (any kit text input), `Terminal` (a terminal panel). Predicates:

| Predicate | Used for |
|---|---|
| `Lens` | app-wide chords only: `cmd-q`, text scale `cmd-=` `cmd-shift-=` `cmd--` `cmd-0` |
| `Lens && !Input && !Terminal` | home navigation letters |
| `Space && !Input && !Terminal` | in-space navigation letters and scrolling |
| `Composer > Input` | the composer's own chords (`cmd-enter`, `cmd-shift-enter`, `alt-enter`, `escape`); no other input (U5's notes) gets them |
| `Notes > Input` | the notes editor's own (`enter` and `cmd-enter` save, `escape` cancels, U5); `shift-enter` stays a new line |
| `Terminal` | keys the terminal consumes (Rung 2); `cmd-w` `cmd-t` `cmd-1…9` stay on `Space` |

| Keys | Predicate | Action | Unit |
|---|---|---|---|
| `cmd-q`; `cmd-=` `cmd-shift-=` / `cmd--` / `cmd-0` | `Lens` | Quit; TextBigger / TextSmaller / TextReset | A0 |
| `left right h l j k` `1 2 3` `v` `m u` `t s` `?` `enter` | `Lens && !Input && !Terminal` | move, set row, cycle visible, seen/unseen, card text/size, help, zoom in | U2 |
| `n` / `N` | both navigation predicates | next needing you / and zoom in | U2 |
| `escape` `[` `]` `tab` `shift-tab` | `Space && !Input && !Terminal` | zoom out, prev/next space, prev/next agent | U2 |
| `j k space shift-space g G` | `Space && !Input && !Terminal` | scroll the transcript | U3 |
| `/` `r` | `Space && !Input && !Terminal` | focus the composer | U4 |
| `cmd-enter` / `cmd-shift-enter` / `escape` | `Composer > Input` | send / send and file back / leave the box | U4 |
| `alt-enter` | `Composer > Input` | queue as note | U5 |
| `a` / `c` / `p` | `Space && !Input && !Terminal` | add a note / capture the transcript selection / notes into the composer | U5 |
| `enter` `cmd-enter` / `escape` | `Notes > Input` | save the note / cancel | U5 |
| `cmd-w` `cmd-t` `cmd-1…9` | `Space` | close panel, terminal, switch panel | Rung 2 |
| `ctrl-alt-cmd-h` | global (`global-hotkey`) | summon | U6 |

Harness scenarios guard this: `just check-keys` (A0) dispatches `cmd-=` and checks the persisted scale; U4
adds a scenario that focuses the composer and types `n`, `j`, `[`, `]`, then asserts the text arrived and
no navigation happened.

Focus follows the zoom after every action (`views::on`), with one exception (U4): an input inside the
zoom that holds focus (the composer, U5's notes) keeps it while the zoom stays on the same agent (a clicked
path or mention of that agent); the composer takes it on `/` `r` (only when the agent can be written to)
and gives it back to the zoom on `escape`. A mention that opens another agent moves focus to the zoom, so
the box never types into an agent the owner did not pick. The composer's chords are handled on its own
element, so they act only on the focused box. `cmd-shift-enter` leaves the zoom only once the send lands
(`Effect::FiledBack`), marking seen only the agent as it stood when sent (a later turn still needs you, and
an unread mark set meanwhile stays; a block that ends during the flight is not acknowledged again); a failure stays on that agent, preview included, saying why. The
landing hands focus to the lens at the next render wherever it was in the departing zoom
(`composer::sync`), as does a focused box left under another agent (to the zoom). A send refused for
attribution outlasts any viewer answer already in flight.

## 5. Type scale and theme

One app-wide **text scale** (`Prefs::text_scale`, range 0.7–1.8 in ×1.1 steps; 1.0 is the spike's sizes × 0.9,
the owner's ruling: body 10.8 px, code 11.7 px, meta 9.9 px) is the only source of text size.
`views::theme::type_scale(scale)` gives the tokens `small`, `body`, `title`, `code`, `line`; views use those
and never a literal `px()` for text. `theme::apply` pushes `body` into the kit theme's `font_size` and `code`
into `mono_font_size`, calls `Theme::sync_base` (the kit rebuilds its Base defaults only then) and refreshes
every window, so inputs, lists, markdown and the code editor follow. The scale persists in `prefs.json` and is
restored at boot. The palette is dark only in v0; U2 owns colours. Font families are explicit (Menlo /
Monaco) so the kit never enumerates installed fonts.

## 6. Persistence

| Where | What | Owner |
|---|---|---|
| Server, `spaces` | space definitions (`{id,name,order,created}`, tombstones 30 days) | shared with web |
| Server, `spaces.members` | per space: `{members:[{kind:agent,name}|{kind:file,root,path}], updated}` | shared with web |
| Server, `notes` | notes, per agent | shared with web |
| `prefs.json` | text scale, rows, visible agent per space, seen marks, drafts, hotkey | this Mac |
| `outbox.json` | unsent state rows (notes, spaces, members), written before each send attempt | this Mac |
| `snapshot.json` | the last board, spaces, members and notes, for the first paint | this Mac |

`store::sync` holds it, one `Sync` per namespace. State sync is simpler than web's in one way: **no persisted revision cursor.** Every boot pulls each
namespace with `since=0` (tens of kilobytes, one round trip each) into the store; the pull cursor lives in
memory for the session and a `state-changed` frame above it pulls again. Rows resolve last-write-wins on
`(updated, writeID)`.

The outbox is durable and its cleanup is **version-aware**, copied from web's `stateSync.ts`, because the
server's `accepted` list omits idempotent and losing rows and so cannot be used as the acknowledgement:

1. A local edit queues its row (a full version, `(updated, writeID)` set) and saves `outbox.json`. Every
   POST waits for a successful save of the outbox as it stood when the send was decided, whether or not
   that step changed it, so a send can never overtake an edit's own save still pending on another task. A
   failed save posts nothing and comes back as a transport failure, which backs off and tries again.
2. A POST sends a copy of the whole outbox. When the POST returns any 2xx, every queued row whose version is
   **equal to or older than** the version that was sent is retired; a newer edit of the same key made while
   the POST was in flight stays queued. `accepted` is not consulted, so a lost-then-retried write and a
   write that web beat both settle.
3. Then pull. After every pull, a queued row is discarded when the pulled row for its key compares equal or
   newer; the store keeps the winner.
4. 409 means local-only; 413 holds the outbox until the next edit; anything else retries with backoff
   500 ms → 10 s.

The snapshot is written on every change of what it holds, coalesced.

Local writes go through one writer in `local`: each save carries a sequence number, an older save never
overwrites a newer one, and every file is written to a temporary name and renamed into place, so a crash
mid-write leaves the previous file intact. The shell coalesces bursts (a held ⌘+) into one write.

## 7. Testing

- **Fixtures** (`testdata/`, recorded by `testdata/record.sh` from the tailnet serve into a staging
  directory, scrubbed by `scrub.py` on decoded string values, and only then published): the board, the first
  two SSE frames, `viewer`, the three state namespaces, and for three agents (`mupu` claude with every kind
  but one, `conductor-line` with `task_notification`, `grill-confirm-lubo` codex) detail, a tail page and a
  `before=` page; plus a tail and a `before=` page from `riko`, the ~90 MB transcript. `just fetch-big riko`
  pages the whole thing into `testdata/big/` (gitignored) for perf runs.
- **`api` tests** (`tests/fixtures.rs`, `api::sse` unit tests) decode every fixture into the types, check the
  paging invariants (`prevOffset` is the first entry's offset; a `before=` page ends before the tail's
  `from`), and run the `Reader` against loopback servers: `close` interrupts a frame read, a stalled header
  read and the backoff, and a silent stream trips the watchdog and reconnects. `tests/fake_server.rs` runs
  the write path, refusals, the reconnect and the outbox save-before-send rule against an in-process fake.
- **`store` tests** (in-module) feed fixture-built events to `Store::apply` and assert state and effects.
  No network, no clock, milliseconds to run.
- **Layering** (`tests/layering.rs`), see §1.
- **UI harness** (`harness`, `just harness "<steps>"`): a scripted run opens its window with `focus: false`,
  orders it behind every other app's windows (`platform_mac::order_windows`) and never calls
  `activate`, so the owner keeps focus. It always quits when the script ends, and any failed step (a bad
  keystroke, a failed screenshot, an unknown step) exits non-zero. Steps: `wait:`, `key:` (through
  `Window::dispatch_keystroke`, the real input path), `shot:` (draws a fresh frame first, then
  `render_to_image`; needs the `shots` feature = GPUI `test-support`), `rss`, `cpu:<ms>` (CPU share with
  pulse paints, shell renders, pointer moves and whether the window was on screen), `move:<ms>` (the same
  while a synthetic pointer sweeps the window), `quit`; `HERDER_NATIVE_WINDOW=WxH` sizes the window and
  `HERDER_NATIVE_VISIBLE=1` orders it in front, still unfocused, for CPU runs, only when the owner asks
  for one (the window pops up over their work); units add `type:` and `keycpu:` from the spike as they
  need them. U3 added `draw` (one frame: a window behind others gets none, and transcript paging is
  driven by render), `link:<url>` (dispatches what a click on a transcript link dispatches),
  `expect:<agent>` / `expect:<agent>+preview` (what the zoom shows; the shell answers through
  `harness::Probe`, so the harness knows no views), `cpuscroll:<key>x<n>` (`n` keystrokes, each with a
  timed `Window::draw`) and `start:<ms>` (draws until the open transcript has paged back to its start,
  logging its rows; also a `Probe` query). U4 added `box:<focused|idle>:<text>` (the composer's focus
  and text), `says:<text>` (the line under the box contains it) and `testdata/fake_serve.py`, a loopback
  serve over the fixtures whose `POST …/message` answers ok, slowly ok, 409 (sender collision), 502 or
  holds: any scenario that presses `cmd-enter` points `HERDER_URL` at it, never at the real serve
  (`just check-composer`, six runs, each failing unless the app exits 0 at `quit` with the POSTs it
  expects). U5 added `notes:<n>:<closed|focused:text>`, `has:` (the composer contains), `select:` (stands in for a
  pointer selection, which a script cannot drag), `tap:` (a key that may be bound to nothing), and `POST
  /api/state/<ns>` on the fake serve, held in memory (`--notes` seeds web's two notes from
  `testdata/notes-web.json`), and `click:<capture|handoff|edit:i|delete:i>` (what a click on the strip
  dispatches, through the focused element as the click does). `just check-notes` runs six scenarios (one
  relaunches on the same HOME after a hand-off), each failing unless it made exactly the notes POSTs it
  expects and no message. U6: a scripted run is test mode (`platform_mac::quiet`): notifications, the
  dock badge and the summon chord are logged no-ops (`platform: would notify …`, `platform: badge N`), and
  `HERDER_NATIVE_FRONT=1` makes it count as frontmost with its window still behind; the fake serve's
  `--turn` sends fleet frames that end agents' turns, `summon:<tag>` dispatches what a notification's
  click does (without activating the app), and `just check-alerts` runs four scenarios (a turn on the
  lens, the agent in view, a burst, an agent in no space opened alone, shot `u6-no-space`). Screenshots and presented-frame timings need an
  unlocked screen; CPU frame cost (`Window::draw` timed directly) does not.
- **Perf** is acceptance at each rung, measured with the screen on: cold start < 300 ms, idle ≈ 0 % CPU,
  RSS < 150 MB with the 88 MB transcript and a terminal, keystroke to paint < 16 ms, smooth scrolling on
  the 88 MB transcript. `harness::metric` lines on stderr carry the numbers.
- **Transcript page reads (ratified at U3 review).** Pages are 100 entries. The "< 200 ms per request"
  target is not reachable over the tailnet: a bare GET costs 150–275 ms, and riko's `before=` pages
  measured p50 210 ms, max 363 ms. Reads stay off the main thread and prefetch ahead of the viewport, so
  this is a network floor, not a stall; gzip on the serve is a separate ask.
- **Idle CPU exception (Rung 1, agreed at U2 review).** A window on screen costs about 1 % of one core
  with nothing to draw: GPUI 0.3.7 runs a CVDisplayLink while the window is visible and calls its frame
  step every vsync (`stop_display_link` is private), so the app renders nothing and still pays the tick.
  Occluded, the same run is 0.1 %. No fork. Working dots add their pulse: a separate `Pulse` view over the
  cached shell repaints only the dots on screen, in 4 steps over 1.6 s (2.5 Hz). At 5 Hz, three working
  dots measured about 0.9–1.0 points above the floor; 2.5 Hz was ratified at U2 on that estimate, without
  a fresh visible run.
- **Live smoke** once per unit against the tailnet serve, by hand.

## 8. Line budgets (Rung 1)

| File | Budget | File | Budget |
|---|---|---|---|
| `api/types.rs` | 300 | `views/lens.rs` | 350 |
| `api/client.rs` | 200 | `views/space.rs` | 300 |
| `api/sse.rs` | 150 | `views/transcript.rs` | 400 |
| `store/mod.rs` | 250 | `views/composer.rs` | 150 |
| `store/sync.rs` | 320 | | |
| `store/fleet.rs` | 120 | `views/notes.rs` | 200 |
| `store/spaces.rs` | 250 | `views/theme.rs` | 100 |
| `store/transcript.rs` | 400 | `shell.rs` | 300 |
| `store/condense.rs` | 220 | `views/markdown.rs` | 250 |
| `store/notes.rs` | 250 | `local.rs`, `platform_mac.rs`, `harness.rs` | 120, 150, 200 |
| `store/composer.rs` | 175 | | |
| | | `shell/io.rs` | 100 |

About 4,000 lines for Rung 1, tests excluded. Going over a budget needs a stated reason in the unit's DONE
report and the reviewer's agreement; the usual answer is a move into the right module, not a bigger number,
and never a new module invented to satisfy a cap.

U6 (asked of review; finding 4 grew these): `store/spaces.rs` 405, where needs-you lives (the brief's placement): `Alerts`, the
transition rule (an agent's turn or block moving on into needing you), the one-second burst and its
summary, the badge (the lens total plus agents in no space that need you; they alert too, owner ruling, and open
alone in a zoom of no space, `Zoom::alone`); `shell.rs` 446 (the effects, the frontmost/zoom sync into the store, the chord and
notification-click summon); `store/mod.rs` 427 (`Looking`, `BurstEnded`, `Summon`, `Notify`, `Badge`,
`Burst`); `views/space.rs` 357 (`Summon`, `summon`, the zoom of no space); `harness.rs` 338 (`summon:`); `platform_mac.rs` 96, under its 150.

U5 exceptions (agreed at review; the U5 fixes grew the first two): `views/notes.rs` 426, one cohesive view
(the strip, its count collapse, the editor with its own key context for add, capture and edit and its
refusal kept on screen, the two-click delete, the capture chip and the selection bound to its agent, the
problem lines, the harness's click map; rustfmt lays the GPUI builder chains out a call per line);
`store/notes.rs` 427 (each edit's row in web's record shape, web's 8 KiB refusals and edit-after-delete
fallback, the hand-off and queue as transfers that save their destination first, and web's
`noteTransferText` and `noteSourceLabel`, so a hand-off reads as web's); `harness.rs` 314 and `shell.rs`
387 (the U5 probes and steps, the transfer's save), `views/mod.rs` 375 (the notes bindings and help, and
the editor in the focus rule), `views/transcript.rs` 401 (the selection taken at mouse-up),
`api/types.rs` 360 (quote and source omitted when unset, as web writes them), `store/mod.rs` 403
(`Event::Note`, `Effect::Transfer` and `sync_step`, shared by the network and local edits).

U4 exceptions (agreed at review): `store/composer.rs` is new (drafts, `can_send`, the send lifecycle) at
174; `views/composer.rs` 217 (the box, its keys, file-back and the wording of every read-only state and
failure, moved from the store at review); `shell/io.rs` 119 (`save_then_message`, the prefs-before-POST
barrier for a message, beside `save_then_send`); `store/spaces.rs` 273 (`looking` / `acknowledge`, the
seen mark a file-back bounds to send time); `store/mod.rs` 388, `shell.rs` 358, `views/lens.rs` 353,
`views/mod.rs` 356 and `harness.rs` 279 carry the composer's event, effects, widget, focus rule and probes.

Documented U3 exceptions (agreed at the U3 reviews): `store/transcript.rs` 539 (one cohesive paging and
request lifecycle: cursors, pairing, wake coalescing and each read's own retries; the condenser moved to
`condense.rs`), `api/types.rs` 357 (message recipients and the 11-field payload projection that cut the
decode high-water), `api/client.rs` 219 (`resolve`'s optional agent scope), `store/mod.rs` 364 (reducer
vocabulary) and `harness.rs` 267 (the U3 steps and `Probe`). `shell.rs` is back to 338, under its U1
exception, since its background I/O (`run_fetch`, `save_then_send`) moved to `shell/io.rs`.

Documented U1 exceptions (agreed at U1 review): `store/mod.rs` 349 (the Event/Effect vocabulary,
`Prefs`, the reducer), `shell.rs` 347 (the effect runner with the save-before-send barrier), `api/sse.rs`
195 (the reconnecting `Reader` with its cancellation).

## 9. From the spike: lifted, rewritten, dropped

**Lifted** (rewritten to fit, credit where it came from): the SSE frame parser and the blocking thread with
backoff; the `<internal>` stripping and the tool/result pairing idea (now offset-keyed, see §3); seeding
theme fonts before `gpui_kit::init` (150 ms of the cold-start budget); binding actions under key contexts;
the harness steps and `rss_mb`; the card-to-window morph and the sideways swipe (U2); notification `tag` =
agent name, click opens it (U6); the `global-hotkey` install (U6); the disk snapshot for first paint
(`local`, U1); `bundle.sh`.

**Rewritten:** the 1,700-line `main.rs` becomes `shell` + `views`; the layouts import becomes
`spaces.members`; 4 s forward polling becomes `entry:` wakes; loading the whole history forward from byte 0
becomes `before=` paging on scroll; the append-only condenser becomes the offset-keyed transcript.

**Dropped:** guessing membership from groups; the `layouts.json` import; `LENS_*` env knobs other than the
script; the fleet-sidebar and command-palette ideas (settled decision 5).
