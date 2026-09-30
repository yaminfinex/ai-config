# herder native — architecture

The macOS client for herder serve, on GPUI through `gpui-kit = "=0.7.0"`. What to build is the ladder in the
mission's `v0-spec.md`; the settled decisions in `playbook.md` are binding. This file fixes the shape so the
Rung 1–3 units can be built without the code ballooning. The spike (`prototypes/native-gpui`) proved the
budgets; this is its lessons, rewritten as rules.

## 1. One crate, seven modules, one-way dependencies

One crate, `herder-native` (a lib plus a two-line bin). Not a workspace: Rung 1 is a few thousand lines, and
a workspace would add manifests and feature plumbing for a boundary that `tests/layering.rs` already enforces
by reading the sources. Split into crates only if a module needs to be built without GPUI, which nothing does
today (`cargo test` links GPUI once and reuses it).

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
| `api` | Typed wire models, blocking HTTP, the SSE frame reader, `before=` paging and sends; called from background threads. | serde, ureq |
| `store` | Pure, deterministic state: `Store::apply(Event) -> Vec<Effect>`; no GPUI, no I/O, no clocks. | `api::types` |
| `views` | Thin GPUI render functions over `&Store` that dispatch `Event`s; sizes only from `views::theme`. | `store`, gpui-kit |
| `shell` | Owns the store, the threads, the one channel, the window, the keymap and the kit widget state; runs effects. | everything |
| `local` | `prefs.json` and `snapshot.json` under `~/Library/Application Support/herder-native/`. | `store` types |
| `platform_mac` | The AppKit calls GPUI lacks: window ordering, dock badge, activation policy, the hotkey bridge. | objc2 |
| `terminal` | A local PTY running `et`/`ssh -t`, emulated by `alacritty_terminal`, painted by a view (Rung 2). | alacritty_terminal |
| `harness` | `HERDER_NATIVE_SCRIPT` steps: screenshots and metrics, never taking focus. | gpui-kit |

`tests/layering.rs` fails when `store` or `api` mention `gpui`, when `store` does I/O or reads a clock, or
when `views` reach the network. Add a needle there before adding a dependency, not after.

## 2. Data flow and threads

- **Threads.** One `std::thread` blocks on the SSE socket (idle costs nothing). REST calls run on GPUI's
  background executor. Nothing else spawns threads.
- **One channel.** Every background result is an `Event` sent on one `futures::mpsc::UnboundedSender`. One
  foreground task (`cx.spawn`) drains it and calls `Shell::dispatch`.
- **The mutation rule.** State changes in exactly one place: `Store::apply`, called by `Shell::dispatch` on
  the foreground thread. Views take `&Store` and dispatch `Event`s; they never hold `&mut Store`. The kit's
  own widget state (`ListState`, `TextareaState`) lives in the shell's `Ui` struct, not in the store.
- **Effects.** `apply` returns what must happen next (`Fetch`, `Send`, `Persist`, `Notify`, `Badge`). The
  shell runs them off the main thread; their results come back as events. A fixture test therefore checks
  both the state and the effects a reduction produces.
- **Boot.** Seed theme fonts, `gpui_kit::init`, open the window, then in parallel: `local` snapshot →
  `Event::Snapshot` (paints within 300 ms), the SSE thread (`hello` + `fleet`), `GET /api/viewer`,
  `GET /api/state/{spaces,spaces.members,notes}?since=<cursor>`.
- **Reconnect.** Backoff 500 ms → 10 s, watchdog 45 s. After every reopen: re-read each open transcript
  forward from its `next_offset`, re-pull the state namespaces, and expect a fresh `fleet`. A changed
  `hello.buildIdentity` shows "server updated" and never reloads by itself.
- **Transcript wakes.** One stream, subscribed with `agents=` to the agents of the zoomed space; changing the
  set reconnects (API gap G10). An `entry:` frame only means "read forward from `next_offset`", coalesced
  over 25 ms. `rewindow` or a `reset` throws the window away and re-reads the tail.
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
  `Watch`, `Background`) per space, the **visible agent** per space, and **seen** per agent (a timestamp).
  **Needs you** = the agent is not `Working` and its last activity is after its seen mark; the card count is
  the number of such agents in the space.
- **`transcript::Item`** — what compact mode renders, one per row with a `(session_id, byte_offset)` key:
  `Prompt`, `Delivery{sender, text, operator}`, `TaskNotification`, `SystemChip` (`injected_system`,
  `command_stdout`, `system_chip`, `turn_duration` fold here or are dropped), `CompactDivider`,
  `Assistant{markdown}`, `Thinking` (a collapsed pill), `Tool{name, summary, result}`, `Error`.
  **Condensing:** `tool_use` opens a `Tool` and records `tool_use_id → item index`; the matching
  `tool_result` fills `result` (an `is_error` result marks the line ✗). Assistant text has
  `<internal>…</internal>` removed and `<status>` unwrapped; the operator envelope
  (`[HERDER_WEB_OPERATOR_NOTE_BEGIN]…END]`) is stripped from deliveries. The pending map persists across
  pages so a result on the next page still finds its call. A **`Transcript`** holds the items, `next_offset`
  (forward, on `entry:` wakes) and `prev_offset` (backward, `before=`, `0` at the start of the file).
- **`notes::Note`** — the web record: `{id, group (agent or general), text, quote?, source?, created}`,
  `updated` on the row. **`Draft`** is one string per agent, local only.
- **`Prefs`** — local owner preferences: `text_scale` now; rows, visible, seen, drafts, sync cursors next.

## 4. Keys

The kit's keymap beats raw key listeners: its Root binds `tab`, its Input binds `cmd-enter` and `escape`,
so a `capture_key_down` handler never sees them. Every binding is therefore an `action!` bound in a key
context under the root's `Lens`; a deeper context wins. Three contexts: **`Lens`** (home), **`Lens > Space`**
(zoomed into a space), **`Lens > Input`** (the composer, wrapping the kit's Input). Single-letter keys are
bound only in `Lens` and `Lens > Space`, never where text is typed.

| Keys | Context | Action | Unit |
|---|---|---|---|
| `cmd-q` | Lens | Quit | A0 |
| `cmd-=` `cmd-shift-=` / `cmd--` / `cmd-0` | Lens | TextBigger / TextSmaller / TextReset | A0 |
| `left right h l j k` `1 2 3` `v` `m u` `t s` `?` | Lens | move cursor, set row, cycle visible, seen/unseen, card text/size, help | U2 |
| `enter` / `escape` | Lens / Lens > Space | zoom in / zoom out | U2 |
| `n` / `N` | Lens, Lens > Space | next needing you / and zoom in | U2 |
| `[` `]` | Lens > Space | previous / next space | U2 |
| `tab` `shift-tab` | Lens > Space | next / previous agent in the space | U2 |
| `j k space shift-space g G` | Lens > Space | scroll the transcript | U3 |
| `/` `r` | Lens > Space | focus the composer | U4 |
| `cmd-enter` / `cmd-shift-enter` / `escape` | Lens > Input | send / send and file back / leave the box | U4 |
| `alt-enter` | Lens > Input | queue as note | U5 |
| `cmd-w` `cmd-t` `cmd-1…9` | Lens > Space | close panel, terminal, switch panel | Rung 2 |
| `ctrl-alt-cmd-h` | global (`global-hotkey`) | summon | U6 |

## 5. Type scale and theme

One app-wide **text scale** (`Prefs::text_scale`, default 1.0 = 11 px body, range 0.7–1.8 in ×1.1 steps)
is the only source of text size. `views::theme::type_scale(scale)` gives the tokens `small`, `body`,
`title`, `code`, `line`; views use those and never a literal `px()` for text. The shell pushes `body` into
the kit theme so its inputs, lists and markdown follow, and the code view and terminal read `code`. The
scale persists in `prefs.json` and is restored at boot. The palette is dark only in v0; U2 owns colours and
the font choice (explicit families are required, see §8).

## 6. Persistence

| Where | What | Owner |
|---|---|---|
| Server, `spaces` | space definitions (`{id,name,order,created}`, tombstones 30 days) | shared with web |
| Server, `spaces.members` | per space: `{members:[{kind:agent,name}|{kind:file,root,path}], updated}` | shared with web |
| Server, `notes` | notes, per agent | shared with web |
| `prefs.json` | text scale, rows, visible agent per space, seen marks, drafts, state-sync cursors and outbox, hotkey | this Mac |
| `snapshot.json` | the last board, spaces and members, for the first paint | this Mac |

State sync copies web's `stateSync.ts`: persisted cursor and outbox; pull then push; `state-changed` above
the cursor pulls again; 409 means local-only; 413 holds the outbox until the next edit; else backoff
500 ms → 10 s. Rows resolve last-write-wins on `(updated, writeID)`.

## 7. Testing

- **Fixtures** (`testdata/`, recorded by `testdata/record.sh` from the tailnet serve, read-only, scrubbed
  by `scrub.py`): the board, the first two SSE frames, `viewer`, the three state namespaces, and for three
  agents (`mupu` claude with every kind but one, `conductor-line` with `task_notification`,
  `grill-confirm-lubo` codex) detail, a tail page and a `before=` page; plus a tail and a `before=` page from
  `riko`, the ~90 MB transcript. `just fetch-big riko` pages the whole thing into `testdata/big/`
  (gitignored) for perf runs.
- **`api` tests** (`tests/fixtures.rs`) decode every fixture into the types and check the paging invariants
  (`prevOffset` is the first entry's offset; a `before=` page ends before the tail's `from`).
- **`store` tests** (in-module) feed fixture-built events to `Store::apply` and assert state and effects.
  No network, no clock, milliseconds to run.
- **Layering** (`tests/layering.rs`), see §1.
- **UI harness** (`harness`, `just harness "<steps>"`): a scripted run opens its window with `focus: false`,
  orders it behind every other app's windows (`platform_mac::order_windows_back`) and never calls
  `activate`, so the owner keeps focus; it quits when the script says so. Steps: `wait:`, `shot:` (needs the
  `shots` feature = GPUI `test-support`, `render_to_image`), `rss`, `quit`; units add `key:`, `type:`,
  `cpuscroll:`, `keycpu:` from the spike as they need them. Screenshots and presented-frame timings need an
  unlocked screen; CPU frame cost (`Window::draw` timed directly) does not.
- **Perf** is acceptance at each rung, measured with the screen on: cold start < 300 ms, idle ≈ 0 % CPU,
  RSS < 150 MB with the 88 MB transcript and a terminal, keystroke to paint < 16 ms, smooth scrolling on
  the 88 MB transcript. `harness::metric` lines on stderr carry the numbers.
- **Live smoke** once per unit against the tailnet serve, by hand.

## 8. Line budgets (Rung 1)

| File | Budget | File | Budget |
|---|---|---|---|
| `api/types.rs` | 300 | `views/lens.rs` | 350 |
| `api/client.rs` | 200 | `views/space.rs` | 300 |
| `api/sse.rs` | 120 | `views/transcript.rs` | 400 |
| `store/mod.rs` | 250 | `views/composer.rs` | 150 |
| `store/fleet.rs` | 120 | `views/notes.rs` | 200 |
| `store/spaces.rs` | 250 | `views/theme.rs` | 100 |
| `store/transcript.rs` | 400 | `shell.rs` | 300 |
| `store/notes.rs` | 250 | `local.rs`, `platform_mac.rs`, `harness.rs` | 100, 150, 200 |

About 4,000 lines for Rung 1, tests excluded. Going over a budget needs a stated reason in the unit's DONE
report and the reviewer's agreement; the usual answer is a move into the right module, not a bigger number.

## 9. From the spike: lifted, rewritten, dropped

**Lifted** (rewritten to fit, credit where it came from): the SSE parser and its blocking thread with
backoff; the tool/result pairing and `<internal>` stripping (now typed `Item`s in `store::transcript`);
seeding theme fonts before `gpui_kit::init` (150 ms of the cold-start budget); binding actions under
`Lens` / `Lens > Input`; the harness steps and `rss_mb`; the card-to-window morph and the sideways swipe
(U2); notification `tag` = agent name, click opens it (U6); the `global-hotkey` install (U6); the disk
snapshot for first paint (`local`, U1); `bundle.sh`.

**Rewritten:** the 1,700-line `main.rs` becomes `shell` + `views`; the layouts import becomes
`spaces.members`; 4 s forward polling becomes `entry:` wakes; loading the whole history forward from byte 0
becomes `before=` paging on scroll.

**Dropped:** guessing membership from groups; the `layouts.json` import; `LENS_*` env knobs other than the
script; the fleet-sidebar and command-palette ideas (settled decision 5).
