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
| `store` | Pure, deterministic domain state: `Store::apply(Event) -> Vec<Effect>`; no GPUI, no I/O, no clocks. `store::sync` is the per-namespace `/api/state` pull cursor and outbox (§6), shared by spaces, members, notes and read markers. `store::markers` is the read markers shared with web (RM): the row, its merge, reading through, unread, the dwell, seeding. `store::attention` owns attention: needs-you, the local block marks, the alerts and the dock badge (U2, U6). `store::cards` holds the lens cards' text (F4). | `api::types` |
| `views` | GPUI views that render from `&Store`, own their widget entities, and dispatch `Event`s; sizes only from `views::theme`. `views::notes` is the notes strip (header, editor, transfers) and `views::notes_list` its keyboard list (selection, keys, cards; F6); `views::capture` notes a transcript selection where it was made (F7); `views::paths` offers where a clicked path lives when that is not obvious (G3). `views::panel` is one agent's set of them (DK1). | `store`, gpui-kit |
| `shell` | Owns the store, the threads, the one channel, the window and the keymap; runs effects (`shell/io`: the REST reads and the save-then-send, off the foreground). | everything |
| `local` | `prefs.json`, `layouts.json` (DK2), `outbox.json` and `snapshot.json` under `~/Library/Application Support/herder-native/`. | `store` types |
| `platform_mac` | The AppKit calls GPUI lacks: window ordering, dock badge, activation policy, the hotkey bridge; opening a URL, and a file by VS Code's CLI (G3). | objc2 |
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
  previewed outsider), and to none on the lens. A transcript is open for each agent panel on screen
  (`Move::View`'s focused agent and each other dock group's shown tab `beside` it, DK2), each its own generation,
  and a panel off screen lets its rows go. An `entry:` frame only means "read forward from
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
  `Watch`, `Background`) per space and the **visible agent** per space. **`markers::Marker`** per agent
  (RM; web's code is the spec: `readMarkerModel`, `readPositionModel`, `spaceAttentionModel`,
  `viewingModel`): the `read.markers` row shared with web, `{turn, pos: {session, offset, ts} | null, at,
  unread}`, `turn` being the board's `turn_end_id` read (the hcom event id of the agent's latest completed
  turn, monotonic; the board carries no activity timestamp). Rows merge newest-wins, but turn, position
  and time never go back, except that a newer mark unread stands; a pulled row merged ahead of its winner
  is republished (`Sync::repairs`), and the outbox sends a row as it merged. A weak row (version 1)
  against a real one takes no part, set aside before versions compare: the real one (a tombstone too)
  wins as written, no repair (the owner's rule, web's
  too); weak rows only fill an empty slot. **Unread** = a mark unread, or the agent is `listening` or `active` on
  the bus and its `turn_end_id` is above its marker's (web's gate: an agent already working on its next
  turn still counts for the one it ended; before RM native excluded `Working`). No marker is no baseline,
  so nothing is unread: once the first pull has answered and a live board has arrived, every board agent
  with a turn and no marker is seeded weakly (version 1, so any real row wins; web seeds only the open
  agents, native also alerts the agents in no space), the pre-RM `prefs.seen` turns first, once. **Needs
  you** = unread, or `Blocked` and this block not yet viewed (local `prefs.blocks`, kona's default beside
  the shared markers; owner ruling, U2: viewing clears it, blocking again needs you again). The card count
  is the number of such agents in the space; the header's "N need you" and the dock badge are one number
  (owner rulings, 2026-10-01): each agent that needs you once, in one space, several or none. **Reading**
  (the dwell): only the focused panel's agent, frontmost, its transcript following the bottom, for a second
  (`DWELL_MS`; a change of agent restarts it under a new token) is read through: the board's turn and the
  transcript's newest entry, a position creeping inside a turn already read at most every 5 s, a turn end
  at once. Viewing alone reads nothing; a panel beside the focused one is never read (owner ruling,
  2026-10-03; web reads every visible group, the stricter rule is safe as reading only moves forward). A
  block that lands on the watched agent is viewed at once. `m` marks the space's unread agents read and
  views their blocks, `u` marks each of its agents on the board unread, `alt-u` toggles the zoomed agent
  (web's); these marks and a file-back's are held until the first markers pull has answered, then made
  from the merged marker. A mark unread puts reading back before the latest turn's opener, over every
  entry read in (`Transcript::turn_start`, web's `lastTurnStart`), and holds until its agent has been
  left while frontmost (another panel focused, or the zoom closed; the app going to the back leaves
  nothing) and come back to. A mark read reads to the tail when
  the transcript is open and following it, else keeps the position.
  Leaving the bottom (a scroll key, the wheel) reaches the store as it happens, before any fleet frame
  behind it (the wheel's from the list's scroll handler, deferred to the end of that event's effects: the
  list calls it holding its own borrow, so asking the list from there panicked, the owner's 10-02 crash); a route that publishes nothing (the scrollbar's drag) is caught as the shell reduces its next
  event (`transcript::reduce`, which publishes the list's leaving first); the view's word on the tail names the transcript's agent and generation, and a stale one
  (after a zoom switch or a reset) is dropped.
- **`transcript::Item`** — what compact mode renders (`store::condense` projects entries; `transcript`
  orders and pairs them): `Prompt`, `Delivery{sender, text, operator, head}` (`head`: recipient, intent,
  thread and message id, for the card's header; acks and the launcher vanish,
  owner ruling F2), `Chip{tone, label, text}` (a run pill: a task notification, a slash command or an unknown
  entry; `Tone` is `Tool`, `Thinking`, `Message`,
  `Other` or `Status`, web's pill colours), `SystemChip` (model switches; `injected_system`,
  `command_stdout`, `turn_duration` and scheduled-task fires fold or are dropped), `CompactDivider` (with
  its metadata), `CompactSummary` (without: web's folded summary), `Assistant(Vec<Seg>)` (one of only `<status>`/`<internal>` fences is a run's member and pill,
  `condense::marker`), `Thinking`, `Tool{name, summary, input, result: Option<ToolResult{error, text, at, capped, images}>}`
  (the input as pretty JSON and the whole output, as the entries pages carry them, for the open member; A3),
  `Error`.
  An answer's fences parse as web's `fencingModel` (`condense::fence`) into `Seg::Text`, `Status` and
  `Internal`; an unfenced or malformed one is a single literal `Text`. The
  operator envelope (`[HERDER_WEB_OPERATOR_NOTE_BEGIN]…END]`) is stripped from deliveries.
- **Rows** (F2) — `condense::rows` groups the items, in key order, into `Row::One(key)` or
  `Row::Run(first, last)`: a run is consecutive activity (`Item::activity`: tools, thinking, chips, agents'
  deliveries), as web's `cleanRows` (`testdata/runs-web.json` is web's own grouping of the fixtures, and
  must match). `condense::pills` aggregates a run as web's `aggregateActivityPills`: adjacent tools of one
  name, and adjacent equal status or internal chips, merge as `×n`. Rows are derived, never stored: the view
  regroups when the item count changes, and `views::transcript::plan(old, new)` splices the rows grown at
  either end into the list in place (a `before=` page may join the top run, a live entry the bottom one);
  anything else resets. Each row's last laid-out bounds are recorded, and in it each mark: a member drawn
  in full or a pill (as the member keys it merges). A page that grows the head (the first member key
  changes) anchors what is read: the mark nearest the viewport's top in the topmost row, and its y. Before
  the list lays out, `hold` (a canvas ahead of it) lays the row now holding that mark out of sight, finds
  the same mark (a pill grown at either end still holds its key) and scrolls it back to that y, however
  the row grew or rewrapped. A mark gone since (a latest block replaced by an answer) falls back to the
  row's top: a stated limitation. `o` toggles the lowest run on screen among the recorded rows (the list
  keeps no top while following the tail). Open runs are a set of member keys, so a run stays open as it grows and closing it
  drops every key in it. `Transcript.times` maps an entry's offset to its UTC milliseconds, for ages
  and the members' durations.
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
  - `choices` (G3): where a clicked path lives, when the store cannot tell. A path click (or the cwd's ↗)
    asks `/api/resolve`; the answer drops weak results (web's `isConfidentResolution`: a top fuzzy match
    below 20 per character is no match) and opens (`Effect::OpenFile{root, file, line}`, the candidate's
    git root and its path; a folder opens its root alone) the serve's first strong one (exact or suffix)
    when it is in the agent's own root (the longest `roots[].root` holding its cwd), else the only strong
    one when every root was fully searched; otherwise it keeps up to eight candidates as
    `Choices{query, line, candidates, total}`, and `Step::Choose{pick}` opens one or closes them. Nothing
    matching says so, and that some roots were not fully searched. Web opens only the single strong match:
    the agent's own root is native's rule (the brief). The serve ranks the agent's canonical root first
    within a tier; the cwd here is lexical, so through a symlink the two differ. A match in the serve's
    agent root ranks first and the choices are offered; but a symlinked cwd whose lexical parent is also
    a live root may auto-open that lexical parent repo when it alone holds a strong match (an accepted
    limit: the serve does not say which root is the agent's). Each resolve is
    numbered (`What::Resolve{id}`) and only the transcript's latest unanswered one is taken: an older
    click's answer, or one after the choices were answered or closed, is dropped (web's `AbortController`).
- **`cards::Cards`** (F4) — each focus and watch card's visible agent's last assistant answer, cleaned as
  the transcript is (`condense::clean`) with `<status>` stripped too; background cards carry none. One tail
  read (`limit=12`) per agent per turn, keyed on `turn_end_id`; at most four in flight; a result for an
  agent no longer on a text card, or for any turn other than the agent's current fleet turn, is dropped
  (success or failure) and the current turn is asked; a failed read keeps its text and is not asked
  again for that turn until the next `hello`, while a newer turn reads at once. Reads start only after `Boot` and are not in the
  snapshot, so a cold start paints `status · title` until they land. The answer is kept as one plain
  paragraph (`cards::flat`: no headings, emphasis, ticks, table rules or link targets) for the card's
  line clamp.
- **`notes::Note`** — the web record: `{id, group (agent or general), text, quote?, source?, created}`,
  `updated` on the row. **`Draft`** is one string per agent, local only.
- **`Prefs`** — local owner preferences: `text_scale`, rows, visible, viewed blocks (`blocks`; the pre-RM `seen` is read once and dropped), drafts, a pending quick send per agent (F7), `hotkey` (U6);
  `vscode_host`, the Remote-SSH alias file links open on (default `superset`; web asks).

## 4. Keys

The kit's keymap beats raw key listeners: its Root binds `tab`, its Input binds `cmd-enter` and `escape`,
so a `capture_key_down` handler never sees them. Every binding is therefore an `action!` with a key-context
predicate. GPUI evaluates a predicate against the whole focus stack, so a binding on `Lens` also fires while
the composer inside it has focus; single-letter navigation must exclude text surfaces explicitly.

Contexts (identifiers on elements): `Lens` (the root), `Space` (the zoom shell), `Composer` (around the
composer's box, U4), `NotesList` (the notes strip's list, F6), `Capture` (the capture chip and popover at
a transcript selection, F7), `Paths` (a clicked path's choices, G3), `Input` (any kit text input), `Terminal` (a terminal panel). Predicates:

| Predicate | Used for |
|---|---|
| `Lens` | app-wide chords only: `cmd-q`, text scale `cmd-=` `cmd-shift-=` `cmd--` `cmd-0` |
| `Lens && !Input && !Terminal && !NotesList && !Capture && !Paths` | home navigation letters |
| `Space && !Input && !Terminal && !NotesList && !Capture && !Paths` | in-space navigation letters and scrolling |
| `Composer > Input` | the composer's own chords (`cmd-enter`, `cmd-shift-enter`, `alt-enter`, `escape`); no other input (U5's notes) gets them |
| `Notes > Input` | the notes editor's own (`enter` and `cmd-enter` save, `escape` cancels, U5); `shift-enter` stays a new line |
| `NotesList && !Input` | the notes list's keys (F6); not in the editor open in one of its cards |
| `Capture && !Input` / `Capture > Input` | the capture chip's keys / its popover's (F7): while a selection is live no lens or zoom key fires |
| `Paths` | a clicked path's choices (G3): its four keys; while it holds focus no lens or zoom key fires |
| `Terminal` | keys the terminal consumes (Rung 2); `cmd-t` stays on `Space` |

| Keys | Predicate | Action | Unit |
|---|---|---|---|
| `cmd-q`; `cmd-=` `cmd-shift-=` / `cmd--` / `cmd-0` | `Lens` | Quit; TextBigger / TextSmaller / TextReset | A0 |
| `left right h l j k` `1 2 3` `v` `m u` `t s` `?` `enter` | `Lens && !Input && !Terminal` | move, set row, cycle visible, read / unread the space's agents (RM), card text/size, help, zoom in | U2 |
| `n` / `N` | both navigation predicates | next needing you / and zoom in; after the spaces, `N` (and `n` zoomed) opens an agent in no space alone | U2, U6 |
| `escape` `[` `]` | `Space && !Input && !Terminal` | zoom out, prev/next space | U2 |
| `tab` `shift-tab`, `alt-right` `alt-left` | `Space && !Input && !Terminal` | the focused group's next / previous tab, wrapping, focused (the dock is asked at the next sync, never from inside its own handlers) | U2, DK2 |
| `cmd-1…9` | `Space && !Input && !Terminal` | the focused group's tab N | DK2 |
| `cmd-w` | `Space && !Input && !Terminal` | close the focused tab (a pinned one leaves the space) | DK2 |
| `alt-enter` | `Space && !Input && !Terminal` | maximize the focused group / put it back (not kept) | DK2 |
| `alt-u` | `Space && !Input && !Terminal` | mark the zoomed agent read if unread, else unread (web's toggle) | RM |
| `j k space shift-space g G` | `Space && !Input && !Terminal` | scroll the transcript | U3 |
| `o` | `Space && !Input && !Terminal` | open / close the lowest run on screen, as laid out (none if none shows); a click on a run's pills does the same | F2 |
| `/` `r` | `Space && !Input && !Terminal` | focus the composer | U4 |
| `cmd-enter` / `cmd-shift-enter` / `escape` | `Composer > Input` | send / send and file back / leave the box | U4 |
| `alt-enter` | `Composer > Input` | queue as note | U5 |
| `a` / `p` | `Space && !Input && !Terminal` | add a note / every note into the composer (U5's `c`, capture the selection, is F7's chip) | U5 |
| any printable key / `enter` `space` / `cmd-enter` / `escape` | `Capture && !Input` | the popover with that key typed / empty / send the quote / cancel | F7 |
| `enter` / `cmd-enter` / `escape` | `Capture > Input` | save the note / send it to the agent / cancel; `shift-enter` a new line | F7 |
| `enter` `cmd-enter` / `escape` | `Notes > Input` | save the note / cancel | U5 |
| `up` | `Composer > Input` | into the notes list, every note selected, when the box is empty or its caret at the start; else the kit's caret move (`notes_list::Up` propagates) | F6 |
| `up` `down` / `shift-up` `shift-down` / `cmd-a` | `NotesList && !Input` | move the cursor / extend the selection from its anchor / select all | F6 |
| `enter` / `backspace` `delete` / `e` / `cmd-c` / `escape` | `NotesList && !Input` | the selection into the composer / delete it (a second press; any other key disarms) / edit the cursor's note in place / copy (`Host::copy`; a scripted run logs it) / clear the selection, then back to the box | F6 |
| `up` `down` / `enter` / `escape` | `Paths` | move / open the cursor's candidate in VS Code / close | G3 |
| `cmd-t` | `Space` | terminal | Rung 2 |
| `ctrl-alt-cmd-h` | global (`global-hotkey`) | summon | U6 |

**Mouse (F4).** A card click selects its space and a double-click zooms in (`lens::Pick`); a tab click
shows that agent (`space::Tab`); `lens ›` in the breadcrumb is `Zoomed::Out`. Each click dispatches the
same action the harness's `click:card:i`, `card2:i`, `tab:i` and `crumb` do (`just check-mouse`). Hovering
a card lifts its border, which re-renders the shell on enter and leave only.

**Notes list (F6).** `views::notes_list` is web's `notesListModel` and `NotesList`: `Picked` (selection,
anchor, cursor; pure) is view state, per agent panel, with the list's focus, scroll, keys and cards;
`views::notes` keeps the strip around it (header, add, the editor, the transfers). In the list
no lens, zoom or composer key fires (the `!NotesList` predicates); the kit's Root `tab` still moves focus
out, as a browser's does. A click on a card picks it (`cmd` toggles, `shift` a range from the anchor) and
focuses the list; a double-click edits in place. A note's ✕ and the editor in a card keep their own
clicks (the ✕ stops its click; the editor its mouse-down), so neither picks the card. A delete arms on the
first press and runs on the second; any other key disarms it (a keystroke observer, since GPUI runs an
element's key listeners only for unbound keys), as do a click and focus leaving the list. `e` edits the
cursor's note, chosen or not (web's `cursor ?? selectedNotes[0]`). A hand-off of the chosen notes is the
U5 transfer (destination before source) with only their ids; once it lands, `Effect::HandedOff` names the
deleted ones and the list selects the note after them, as after a delete (a failed save changes nothing).
A note web deletes or reassigns while it is open in the editor keeps its card until save or cancel (web's
`noteEditDisplay`), and the save writes it again (U5). The strip says what each action did for 4 s
(`notes::fade_later`). `just check-notes` (`list`, `picked`, `keyed`, `armed`) and `views::tests`'
`notes_events` (real pointer and key events in a headless window) drive it.

**Type-to-capture (F7).** `views::capture` is web's `NoteCaptureChip`. A drag begun in the transcript and
let go anywhere in the window (the body's `on_mouse_up_out` too, as web's window `pointerup`) that leaves
text selected puts a focused "＋ Add note" chip 6 under the selection's last line, at its left (the kit
does not say where a selection's lines are: the line is the one under the lower end of the drag, its bottom
half a line under the pointer, and a selection over more than one line starts at the text column). While
it holds focus `Capture` is on the focus stack, so no lens or zoom key fires (one rule: no per-key
exceptions; chords keep their meaning, the kit Root's `cmd-c` copies): a printable key opens the popover
with that key typed (the chip's key listener), `enter` or `space` empty, `cmd-enter` sends the quote,
`escape` cancels, a click on it saves the quote alone. The popover (quote, box, footer; web's measured
style) saves on `enter` through `store::notes` as any note (outbox, then POST), sends on `cmd-enter` as
web's quick send (`composer::Step::Quick`: web's note text alone, the draft untouched; it is pending in
`Prefs::quick`, saved before the POST as a draft is, until answered; text that did not land, or that a
boot finds still pending, is added to the draft, never sent again), and cancels on `escape` or a click
anywhere else (typed text and all, as web's). While the agent cannot take a message now (read-only, or
a send of its in flight) `cmd-enter` saves the note instead and says so: native's rule (web sends
regardless of a send in flight, and adds the text to a read-only agent's prompt). Focus leaving the chip
or the popover any other way (the kit Root's `tab`) cancels it too (`on_blur`; GPUI reports it in an
active window), so no zoom key runs while text is selected. Closing clears the selection, so the keys
come back. `just check-notes` (`capture`, `dropped`, `sent`) and `views::tests`'
`capture_events` drive it with real drags and keys.

**Agent panels (DK1).** The zoom body is one `views::panel::Panel` per agent: its transcript view (the
list, folds, taps and where it was read), notes strip, composer and capture, and a focus handle of its
own. `lens::State::panels` keeps one for each of the zoomed space's agents opened since (and a previewed
outsider while shown); the zoomed agent's is shown and focused, the others hidden: a hidden panel's rows
go, with where it was read (`transcript::View::hide`, unless at the tail), and shown again it reads its
tail afresh, pages back as far as that and puts it where it was (`restore`, then `hold`); what it was in
the middle of (an editor, a capture, a confirmation) ends, as with one set of views. `panel::sync` runs
after every action (`views::on`, before focus is placed) and before each frame, and moves focus left in a
hidden panel to the zoom's. The panel's keys (scroll, `o`, folds, links, `/` `r`, notes, capture, list
clicks) are handled on its element, the zoom's (`escape`, `[` `]`, `tab`, tab clicks, `n`) on the
`Space` shell around it. Each panel draws through its own GPUI view, `AgentPanel`, cached and notified
whenever the shell is (it observes it): the seam the dock (DK2) lays out side by side.

**The dock (DK2).** `views::dock` puts the zoomed space's panels on the kit's `DockArea` (one per zoom,
rebuilt on entering another space; the lens stays home): tabs in groups, split side by side or stacked,
drag and drop, divider resize and maximize are the kit's. `AgentPanel` is a kit panel (`BasePanel`,
`Focusable`, `EventEmitter<PanelEvent>`, panel name `agent`, its dump `{"agent": name}`). A space's
members are its pinned tabs; a tab of an agent that is not a member is a preview (italic, a hollow ring),
at most one per group: a mention or an outsider opened there replaces it in place, and a double-click, a
send from it, or a drag to another group pins it (`Move::Pin`, a `spaces.members` write). Closing a pinned
tab (`cmd-w`, its ×, a middle-click) removes the member (`Move::Unpin`); closing a preview writes nothing.
A member another device adds opens as a tab behind the focused group's shown one (in a dock with no tab
left, as the zoom's, focused); one it removes stays open as a preview, unless its group has one: then its
tab closes, so a group keeps one preview, the one it had (saved marked `"preview": true`, which `restore`
keeps over a member removed while the app was closed). Only a send the store takes pins (`ready`, and a
capture's quick send too); a refused one writes nothing. The zoom's agent is the focused panel's: GPUI
reports focus-in only in an active window, so `dock::sync` reads which panel holds focus whenever it runs,
after every action and on every shell render (a focus change refreshes the window), with no timer of its
own (`follow`; the panel's root takes focus on any press, before the transcript's selection can swallow
it). A tab dropped anywhere, its own group too (a reorder), is the zoom's and takes focus. Focus left on
an element the dock lays out anew goes with it (the transcript's text after a click, under a maximize:
G2); the dock's `on_focus_lost` gives it back to the zoom's panel, so the keys never need a click. A
mention opens in the focused group, with `alt` beside it (the first other group in the layout, not the nearest on
screen: a declared limitation; else a new one to the right); an agent already open is shown, not
re-opened. The store is told the focused
agent and every other group's shown tab (`Move::View{agent, beside}`, only when that changes); only the
focused one is seen. Actions never touch the dock: they leave an `Ask` that the next sync carries out
(the dock's callbacks run inside its own update). The tab strip is drawn to web's measurements
(`views::tabs`: `Strip`, a `TabGroupRenderer`, as the kit's is private, always draws a menu and has no
top line; `Skin` delegates the rest of the dock's look to the kit's);
the breadcrumb is a slim line above the dock. `just check-dock` and `views::tests`' `dock_events` drive
it with real clicks, a real drag and keys.

**Paths (G3).** A path in the transcript opens in VS Code, its git root as the window's folder and the file
at its line, by two calls of its CLI, about 3 s apart on the background executor (G3b: one call with both
opens the file but not the folder): `code --folder-uri vscode-remote://ssh-remote+<vscode_host><root>`
(each segment percent-encoded), then `code -r --remote ssh-remote+<vscode_host> -g <root>/<path>[:line]`
into that window; a folder (the cwd's ↗, a folder candidate) is the first call alone
(`markdown::vscode`, run by `platform_mac::vscode`, both argvs logged in a quiet run; web's `vscode://` URL
when the CLI is missing). Which
candidate is the store's (`Transcript::choices`, §3); when it keeps choices, `views::paths` draws them
(web's `.selection-file-popover`: up to eight `root · path` rows, the root's last segment) under the press
that clicked the path (`transcript::View::pressed`), and takes focus once as they land (`paths::sync`,
before each frame: the answer comes back off the foreground). `up` `down` move, `enter` or a click opens,
`escape`, a press anywhere else or focus leaving closes; each hands focus back to the panel. No other
lens or zoom key fires under it (`!Paths`, as `!Capture`); the app-wide chords keep their meaning. The cwd's ↗ opens the cwd's git root the same way.
`just check-paths` (`paths`, `pointed`), `views::tests`' `paths_events` (a real row click, blur, choices gone
while focused) and `tests/keys.rs` drive it.

Harness scenarios guard this: `just check-keys` (A0) dispatches `cmd-=` and checks the persisted scale; U4
adds a scenario that focuses the composer and types `n`, `j`, `[`, `]`, then asserts the text arrived and
no navigation happened.

Focus follows the zoom after every action (`views::on`), with one exception (U4): an input inside the
zoom that holds focus (the composer, U5's notes) keeps it while the zoom stays on the same agent (a clicked
path or mention of that agent); the composer takes it on `/` `r` (only when the agent can be written to)
and gives it back to the zoom on `escape`. A mention that opens another agent moves focus to the zoom, so
the box never types into an agent the owner did not pick. A link (a URL, a mention, a path) shows the
pointer under the mouse, the kit's `TextView`'s own; web also brightens a hovered link and thickens its
underline (`styles.css` `.inline-link:hover`), which the kit cannot do for one link (one link colour, no
hovered-link state): a declared limitation (G2). The composer's chords are handled on its own
element, so they act only on the focused box. `cmd-shift-enter` leaves the zoom only once the send lands
(`Effect::FiledBack`), marking the agent read up to its turn when sent (a later turn still needs you; a
mark unread is cleared, as by any mark read; a block that ends during the flight is not viewed again); a failure stays on that agent, preview included, saying why. The
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
restored at boot. The palette (`theme::pal`) is dark only in v0, its neutrals herder web's dark theme (ink
on ground ~12:1). The kit's own text (markdown, inputs, lists) paints from it too: `theme::dark` sets the kit
theme's foreground, muted, border, link, selection, table, input, ring and caret colours from `pal`, and transcript markdown takes
`theme::prose`, a whole base `TextViewStyle` of its own so none of it reaches the kit's other text (paragraph gap, inline code chip, unwrapped fenced code that scrolls sideways; a row
stops a mostly sideways wheel so the list does not take its vertical part, `transcript::sideways`). Font families are explicit so the
kit never enumerates installed fonts: the lens, composer and notes are Menlo (`FONT`); the transcript is web's
`system-ui` and `ui-monospace` (A1), as CoreText's system families `SANS_T` (`.AppleSystemUIFont`) and `MONO_T`
(`.AppleSystemUIFontMonospaced`, also the kit's mono). "SF Pro" and "SF Mono" do not resolve by name: GPUI
falls back to Helvetica. The two resolve in ~3 ms and ~1.3 ms on the first transcript open; cold start is
unchanged (the lens draws neither).

The transcript is set to web's measured styles (A1, `transcript-style-spec.md`), so its lengths are web's CSS
pixels at the scale, `TypeScale::css(w) = w × scale`, while the lens, composer and notes keep `TypeScale::px`
(the spike's design pixels × 0.9). Prose is 13 on a 20.15 line; headings 26/19.5/15.2/13 bold with web's
margins under them (nothing gaps after a heading, so its bottom padding is the whole gap; above one is only
the block before's own margin, a nearest-layout limit: the renderer cannot collapse margins by neighbour, so
web's h4 top margin of 17.3 is not modelled); code blocks SF Mono
11, padding 9, radius 5; tables transparent (a card's ground shows through), cells 4 8, the header semibold on
the wash; links #a9c4ff (paths too: the kit gives every link the one colour and underline). The selection is
web's #375576 as seen on the ground: the kit paints it over the glyphs, so it is #538ecb at half opacity; the
kit theme's #31406b stays for the composer and notes. `theme::prose` gives the paragraph gap in rems of the kit
root's rem, which is the theme's `font_size` (`body`), not 16. Each row carries its own font, size and line
height, because `hold` lays a row out alone and must measure it as the list does. Rows are spaced by kind
(`transcript::Kind`: answer 10, run strip 5, card 9, divider 14, system chip 6): the gap above a row is
`gap(prev, next)`, the larger of the two kinds' margins, 10 above the first, and the last row adds its margin
plus 14 below. The frame overlays the kit's `Scrollbar` on the list (a rounded #3a3c45 thumb, 8 wide, shown
while scrolling) and, while the list is not following the tail, web's accent "Jump to bottom" pill centred 10
above the bottom; a click is `Scroll::Bottom`.

Entries are drawn as web's (A2, spec §1; `views::entries` builds them, `transcript` decides what is open).
An answer is a 3px #3a3c45 rule, padding 3 0 3 12, a header (the agent in SF Mono 11/600, the time `8h ago`
in mono 9 at the right, 6 above the text), then its markdown, or its fenced parts in a column 4 apart:
text, a status chip cut at 26 characters that a click opens in full (`‹` closes it), an internal note as a
thinking pill `› internal note · N words` that opens to a body on the code ground. One card builder serves
the three cards (`entries::Card`): another agent's message (blue edge), an operator's note (green edge on
#1b2420, indented min(6%, 54), at most 820 wide) and the owner's prompt (green edge on the right, #1d2320,
min(10%, 90), 790); radius 8, padding 9 12, a header of the sender, `web operator`, `→ recipient`, the
intent, `#id`, the thread and the time. GPUI draws one border colour, so the edge is the outer box's
ground past the inner box. Another agent's message keeps the 5-line cut, with web's `Show full message`
toggle; an operator's is whole. The compact divider carries its time and web's token counts; a summary
without metadata is a fold (`.entry-expander`, 6 apart like a system chip); a model switch is a status-tone
chip; queued messages are web's amber box under the list. What is open is `View::open`, by item key and
part (0 an item's own fold, else an answer's segment), so it survives regrouping; every fold toggles
through the `Fold` action, which the harness clicks too. Limits, nearest kept: no local clock time (the
owner's prompt and a queued message past an hour show an age), no fade on a cut message, no letter spacing
on the queued title, the owner's prompt as markdown on a 1.55 line (web: plain text, 1.5).

Runs are drawn as web's (A3, spec §1 "Activity strip", "Expanded run details", "Latest activity"). The strip
is a summary of min-height 24, padding 2 5, radius 6, ruled and on the panel under the pointer or open,
with the dimmer `›` and its pills (mono 9, padding 1 7, 14 tall; a cut status on the button's 17.5 line; a
merged failed tool keeps F2's red). Open, its members sit on a rail (padding 2 8 4 14, a rule on the left)
at their own margins (`Kind::item`): a tool or a thinking is an expander (`entries::expander`, 27 tall,
6 apart); a tool shows its status dot (green, red, blue running), name, summary cut to the row, duration
(result time less call time, web's `formatDuration`) and time, and opens to `INPUT` and `OUTPUT` sections
of mono 11 that scroll sideways; a thinking shows `thinking · 2.7s` (until the next item) and opens to its
text or web's "Thinking content unavailable."; an answer of only statuses and notes is drawn bare, as web,
with its notes held open; cards and answers as their own rows. Closed and last, a run ends in
`Latest activity · 8d` and its last member in full. Members open through `Fold(key, 0)`, the same set as
A2's parts. A chevron that web turns is swapped for `⌄`, raised to the turned glyph's centre. A capped output ends in web's
`Output capped at 16 KiB — N bytes total.` and an image result in `▧ N image results present (not served)`
(the payload's `truncated`, `total_bytes`, `image_count`). The detail's sections are observed under the
tool's id for the headless test. Limits, nearest kept: no structured-patch diff in a tool's detail; its
text is not selectable; a thinking's duration runs to the next item, not the next entry; tasks, slash commands
and unknown entries in an open run stay F2's pill and text; no letter spacing on the section heads.

## 6. Persistence

| Where | What | Owner |
|---|---|---|
| Server, `spaces` | space definitions (`{id,name,order,created}`, tombstones 30 days) | shared with web |
| Server, `spaces.members` | per space: `{members:[{kind:agent,name}|{kind:file,root,path}], updated}` | shared with web |
| Server, `notes` | notes, per agent | shared with web |
| Server, `read.markers` | per agent: `{turn, pos, at, unread, updated}`; a 404 is empty | shared with web |
| `prefs.json` | text scale, rows, visible agent per space, viewed blocks, drafts, pending quick sends (F7), hotkey | this Mac |
| `layouts.json` | each space's dock as left (`{version, spaces: {id: the kit's dump of its tree}}`, DK2); written at most every 250 ms (the latest dump, at the end of a fixed window the first change opens); a file of another `version` is ignored (the kit's own `load` never checks); reconciled with the members when the dock opens, never writing them (each member once where it was, one preview per group, the one marked `"preview": true` else the first, missing members added to the first group); maximize is not kept | this Mac |
| `outbox.json` | unsent state rows (notes, spaces, members, read markers), written before each send attempt | this Mac |
| `snapshot.json` | the last board, spaces, members, notes and read markers, for the first paint | this Mac |
| `~/Library/Logs/herder-native/panic.log` | each panic's message, thread and backtrace, appended before the default hook (and the abort) | this Mac |

`store::sync` holds it, one `Sync` per namespace. State sync is simpler than web's in one way: **no persisted revision cursor.** Every boot pulls each
namespace with `since=0` (tens of kilobytes, one round trip each) into the store; the pull cursor lives in
memory for the session and a `state-changed` frame above it pulls again. Rows resolve last-write-wins on
`(updated, writeID)`, except `read.markers`, which merges as web's (§3). A pull's 404 is an empty pull.
The store writes rows on its own (read markers' seeds and dwell) with the time and `writeID` the shell
stamps on each event (`Store::clock`); a scripted run against anything but loopback (one reading the live serve,
`coldstart`) gets a read-only client that posts nothing (`harness: would post …`): a scripted run
writes only to a loopback `HERDER_URL` over plain http (`client::loopback`, decided on ureq's own parse of
the URL: the fake serve), whatever else it names.

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
  orders it behind every other app's windows (`platform_mac::order_back`) and never calls `activate`, so
  the owner keeps focus; no automated run brings a window on screen. It always quits when the script ends,
  and any failed step (a bad keystroke, a failed screenshot, an unknown step) exits non-zero. Any
  `HERDER_NATIVE_SCRIPT` run is test mode (`platform_mac::quiet`): notifications, the dock badge, the
  summon chord, opening a URL (`platform_mac::open`) and running VS Code's CLI (`platform_mac::vscode`, its argv logged) are logged no-ops. The harness knows no views: what it asks of them goes through
  the `harness::Probe` trait, which the shell implements with `views::probe` (`ask`, `action`, and
  `find`, which scrolls the first transcript row holding a text to the top, or the member holding it in
  an open run, for shots; `run:` opens the run from there too; a selection is a real `drag:`, F7),
  the one file that spells what a script compares against. A scenario that sends anything points
  `HERDER_URL` at `testdata/fake_serve.py` on loopback, never at the real serve (`scripts/scenario.sh`,
  shared by the `check-*` recipes, does that, the throwaway HOME and the reached-`quit` check). Steps live
  in `src/harness.rs`'s module doc, and the scenarios (`just check-keys`, `check-composer`, `check-notes`,
  `check-alerts`, `check-mouse`, `check-runs`, `check-entries`, `check-dock`, `check-paths`, `check-read`) in the justfile's
  comments. `testdata/markers-web.mts` runs web's own read-marker code over fixture rows (unread, merges,
  reads, where a mark unread resumes) into `markers-web.json`, which `store` tests check native against. Screenshots and presented-frame timings need an unlocked screen; CPU frame cost
  (`Window::draw` timed directly) does not.
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

Current budgets, at each file's size after G3b (G3b grew `views/markdown` and `platform_mac` by VS Code's two calls, the folder then the file; RM added `store/markers` for the read markers shared with web and grew `store/transcript` by the read positions, `store/sync` by the markers' merge and repairs, `store/mod` by the clock and the dwell's wake, `api/client` by the read-only client; G3 added `views/paths` for a clicked path's choices, and grew `store/transcript` by the pick rules, the choices and each resolve's number, `views/markdown` by the CLI's argv, `platform_mac` by running it, and `probe` by `paths` and the picks; DK2 added `views/dock` for the dock in the zoom: its sync with the zoom, asks, previews and pins, layouts and their restore, and `views/tabs` for the tab strip drawn to web's measurements; and grew `store/spaces` by the members writes and the layouts, `views/space` lost the hand-drawn tab row; DK1 added `views/panel` for the agent panels and grew `store/transcript` by a transcript per panel on screen, `views/transcript` by a hidden panel's place and its restore, and the views that were one per zoom (`capture`, `notes`, `notes_list`, `probe`, `lens`) by reaching their panel; F7 added `views/capture` for type-to-capture, web's chip and popover, and grew `store/composer` by the quick send and its recovery, `views/transcript` by the release's anchor and the replay's frame rule, and `probe` by `capture`, while `views/notes` lost U5's selection seam; A3 grew `views/entries` by the members' looks (tool, thinking, durations, detail sections), `views/transcript` by the strip, rail and latest block and the bare fenced answer, `condense` and `store/transcript` by the tool's input, output and times, and `probe`/`harness` by `tools`, `run:` and the member clicks; A2 added `views/entries` for the entries' looks and grew `views/transcript` by the answer's parts and their folds, `condense` and `store/transcript` by the segments and the delivery's header, `theme` by the card, badge and queued tints, and `probe` and `harness` by `parts` and the part clicks; A1 grew `views/transcript` by the row kinds and their gaps, the scrollbar and the jump pill, `views/theme` by the transcript's fonts and markdown styles, and `probe`/`harness` by `jump` and `find`; F4 grew `lens`, `space` and `probe` by the card text and the mouse; F6 added web's keyboard list as `views/notes_list` (its selection model `Picked`, the list keys and the cards) and grew `views/notes` by the strip's header, confirmations and the card editor; F2 grew `condense` and `views/transcript` by the runs) (tests excluded: `store/tests.rs`, `views/tests.rs` and the
`mod tests` in `api/sse.rs` and `local.rs`). How each grew past its first budget is in the run-log.
`shell.rs` (boot and running effects) and `store/mod.rs` (the event and effect vocabulary and `apply`)
are restated rather than split: what did not belong in them has moved out (`views::probe`,
`store::attention`, `shell::io`).

| File | Budget | File | Budget |
|---|---|---|---|
| `api/types.rs` | 335 | `views/mod.rs` | 461 |
| `api/client.rs` | 234 | `views/lens.rs` | 470 |
| `api/sse.rs` | 194 | `views/space.rs` | 369 |
| `store/mod.rs` | 498 | `views/transcript.rs` | 1380 |
| `store/sync.rs` | 345 | `views/composer.rs` | 246 |
| `store/fleet.rs` | 108 | `views/notes.rs` | 414 |
| `store/spaces.rs` | 324 | `views/notes_list.rs` | 525 |
| `store/attention.rs` | 263 | `views/probe.rs` | 290 |
| `store/transcript.rs` | 822 | `views/markdown.rs` | 273 |
| `store/condense.rs` | 439 | `views/theme.rs` | 261 |
| `store/notes.rs` | 432 | `shell.rs` | 467 |
| `store/composer.rs` | 228 | `shell/io.rs` | 180 |
| `local.rs` | 101 | `harness.rs` | 390 |
| `store/cards.rs` | 182 | `platform_mac.rs` | 142 |
|  |  | `views/entries.rs` | 529 |
|  |  | `views/capture.rs` | 452 |
|  |  | `views/panel.rs` | 171 |
|  |  | `views/dock.rs` | 902 |
|  |  | `views/tabs.rs` | 361 |
|  |  | `views/paths.rs` | 198 |
|  |  | `store/markers.rs` | 440 |

About 13,667 lines for Rung 1, tests excluded (G3b: +23; RM: +584; G3: +415; G2: +8; DK2: +1,405; DK1: +366; F7: +520). F2 took `store/condense.rs` and `views/transcript.rs` past
its design's estimates (~320, ~530): the fence parser, run grouping, pills and timestamps, and the run strip,
open members, latest line and splice plan; its review added the painted bounds that `hold` and `o` read. Going over a budget needs a stated reason in the unit's DONE
report and the reviewer's agreement; the usual answer is a move into the right module, not a bigger number,
and never a new module invented to satisfy a cap.

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
