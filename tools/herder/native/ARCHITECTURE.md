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
| `shell` | Owns the store, the threads, the one channel, the window and the keymap; runs effects. | everything |
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
- **Effects.** `apply` returns what must happen next. Network and disk effects (`Fetch`, `Send`, `Persist`)
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
- **Transcript wakes.** One stream, subscribed with `agents=` to the agents of the zoomed space. An `entry:`
  frame only means "read forward from `next_offset`", coalesced over 25 ms.
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
- **`transcript::Item`** — what compact mode renders: `Prompt`, `Delivery{sender, text, operator}`,
  `TaskNotification`, `SystemChip` (`injected_system`, `command_stdout`, `system_chip`, `turn_duration` fold
  here or are dropped), `CompactDivider`, `Assistant{markdown}`, `Thinking` (a collapsed pill),
  `Tool{name, summary, result}`, `Error`. Assistant text has `<internal>…</internal>` removed and `<status>`
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
- **`Prefs`** — local owner preferences: `text_scale` now; rows, visible, seen, drafts, hotkey next.

## 4. Keys

The kit's keymap beats raw key listeners: its Root binds `tab`, its Input binds `cmd-enter` and `escape`,
so a `capture_key_down` handler never sees them. Every binding is therefore an `action!` with a key-context
predicate. GPUI evaluates a predicate against the whole focus stack, so a binding on `Lens` also fires while
the composer inside it has focus; single-letter navigation must exclude text surfaces explicitly.

Contexts (identifiers on elements): `Lens` (the root), `Space` (the zoom shell), `Input` (any kit text
input), `Terminal` (a terminal panel). Predicates:

| Predicate | Used for |
|---|---|
| `Lens` | app-wide chords only: `cmd-q`, text scale `cmd-=` `cmd-shift-=` `cmd--` `cmd-0` |
| `Lens && !Input && !Terminal` | home navigation letters |
| `Space && !Input && !Terminal` | in-space navigation letters and scrolling |
| `Input` | the composer's own chords (`cmd-enter`, `cmd-shift-enter`, `alt-enter`, `escape`) |
| `Terminal` | keys the terminal consumes (Rung 2); `cmd-w` `cmd-t` `cmd-1…9` stay on `Space` |

| Keys | Predicate | Action | Unit |
|---|---|---|---|
| `cmd-q`; `cmd-=` `cmd-shift-=` / `cmd--` / `cmd-0` | `Lens` | Quit; TextBigger / TextSmaller / TextReset | A0 |
| `left right h l j k` `1 2 3` `v` `m u` `t s` `?` `enter` | `Lens && !Input && !Terminal` | move, set row, cycle visible, seen/unseen, card text/size, help, zoom in | U2 |
| `n` / `N` | both navigation predicates | next needing you / and zoom in | U2 |
| `escape` `[` `]` `tab` `shift-tab` | `Space && !Input && !Terminal` | zoom out, prev/next space, prev/next agent | U2 |
| `j k space shift-space g G` | `Space && !Input && !Terminal` | scroll the transcript | U3 |
| `/` `r` | `Space && !Input && !Terminal` | focus the composer | U4 |
| `cmd-enter` / `cmd-shift-enter` / `escape` | `Input` | send / send and file back / leave the box | U4 |
| `alt-enter` | `Input` | queue as note | U5 |
| `cmd-w` `cmd-t` `cmd-1…9` | `Space` | close panel, terminal, switch panel | Rung 2 |
| `ctrl-alt-cmd-h` | global (`global-hotkey`) | summon | U6 |

Harness scenarios guard this: `just check-keys` (A0) dispatches `cmd-=` and checks the persisted scale; U4
adds a scenario that focuses the composer and types `n`, `j`, `[`, `]`, then asserts the text arrived and
no navigation happened.

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
  for one (the window pops up over their work); units add `type:`,
  `cpuscroll:`, `keycpu:` from the spike as they need them. Screenshots and presented-frame timings need an
  unlocked screen; CPU frame cost (`Window::draw` timed directly) does not.
- **Perf** is acceptance at each rung, measured with the screen on: cold start < 300 ms, idle ≈ 0 % CPU,
  RSS < 150 MB with the 88 MB transcript and a terminal, keystroke to paint < 16 ms, smooth scrolling on
  the 88 MB transcript. `harness::metric` lines on stderr carry the numbers.
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
| `store/notes.rs` | 250 | `local.rs`, `platform_mac.rs`, `harness.rs` | 120, 150, 200 |

About 4,000 lines for Rung 1, tests excluded. Going over a budget needs a stated reason in the unit's DONE
report and the reviewer's agreement; the usual answer is a move into the right module, not a bigger number,
and never a new module invented to satisfy a cap.

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
