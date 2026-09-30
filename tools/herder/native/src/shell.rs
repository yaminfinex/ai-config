//! The app shell: owns the `Store`, the background threads, the one channel into the foreground, the
//! window and the keymap. Views render from the store it holds and own their own widget entities; every
//! change to the store goes through `Shell::dispatch`, which runs `Store::apply` and then the effects.
//!
//! Boot order matters for the 300 ms budget: theme fonts are seeded before `gpui_kit::init` (see
//! `theme::seed`), then the window opens and paints from the disk snapshot, which is applied
//! synchronously before the stream thread or any REST call starts. `Event::Boot` then opens the stream
//! and pulls in parallel.
//!
//! Threads: the SSE read blocks on its own `std::thread` (`sse::Reader`); REST calls and disk writes
//! run on GPUI's background executor. All of them report back as `Event`s on the one channel, which one foreground
//! task drains into `dispatch`.
//!
//! Durability: every POST of state rows waits for a successful save of the outbox as it stood when the
//! send was decided (`save_then_send`); a save still pending elsewhere cannot be overtaken.

use crate::api::client::{Client, Page, base_url};
use crate::api::{Wire, sse};
use crate::local::{self, Disk};
use crate::store::sync::Step;
use crate::store::transcript;
use crate::store::{Effect, Event, Fetch, Persist, Store, StreamEvent, TextScale, Write};
use crate::views::{Host, lens, theme};
use crate::{harness, platform_mac};
use futures::StreamExt as _;
use futures::channel::mpsc::{UnboundedSender, unbounded};
use gpui_kit::*;
use std::collections::HashSet;
use std::sync::Arc;
use std::time::Duration;

/// Window title and app name.
pub const APP_NAME: &str = "herder native";
const APP_ID: &str = "dev.herder.native";
/// A burst of changes (a held ⌘+, a run of fleet frames) becomes one write of the latest state.
const PREFS_COALESCE: Duration = Duration::from_millis(150);
const SNAPSHOT_COALESCE: Duration = Duration::from_secs(1);
/// Entries read for a card's `<status>` line: a turn ends with its text, a hook chip and its duration.
const STATUS_TAIL: u32 = 12;

actions!(herder, [Quit, TextBigger, TextSmaller, TextReset]);

pub struct Shell {
    pub store: Store,
    ui: lens::Ui,
    client: Client,
    disk: Arc<Disk>,
    tx: UnboundedSender<Event>,
    stream: Option<sse::Reader>,
    /// Files with a write waiting out its coalescing window; it reads the store when it fires.
    saves: HashSet<&'static str>,
    painted: bool,
    live_painted: bool,
}

impl Shell {
    fn new(cx: &mut Context<Self>) -> Self {
        let (mut store, disk) = (Store::default(), Disk::home());
        if let Some(prefs) = disk.load_prefs() {
            store.apply(Event::PrefsLoaded(prefs));
        }
        // Local state first, synchronously: nothing live has started yet, so nothing can be overwritten.
        if let Some(snapshot) = disk.load_snapshot() {
            store.apply(Event::Snapshot(snapshot));
        }
        if let Some(outbox) = disk.load_outbox() {
            store.apply(Event::OutboxLoaded(outbox));
        }
        harness::metric("local state applied");
        theme::apply(store.prefs.text_scale, cx);
        let (tx, mut rx) = unbounded();
        cx.spawn(async move |this, cx| {
            while let Some(event) = rx.next().await {
                if this.update(cx, |s, cx| s.dispatch(event, cx)).is_err() {
                    break;
                }
            }
        })
        .detach();
        lens::pulse(cx);
        Shell {
            store,
            ui: lens::Ui::new(cx),
            client: Client::new(base_url()),
            disk: Arc::new(disk),
            tx,
            stream: None,
            saves: HashSet::new(),
            painted: false,
            live_painted: false,
        }
    }
}

impl Host for Shell {
    fn parts(&mut self) -> (&Store, &mut lens::Ui) {
        (&self.store, &mut self.ui)
    }

    /// The only path to a state change: reduce, then run the effects.
    fn dispatch(&mut self, event: Event, cx: &mut Context<Self>) {
        let is_live_board = matches!(
            event,
            Event::Stream {
                event: StreamEvent::Frame(Wire::Fleet(_)),
                ..
            }
        );
        let scale = self.store.prefs.text_scale;
        let effects = self.store.apply(event);
        if self.store.prefs.text_scale != scale {
            theme::apply(self.store.prefs.text_scale, cx);
        }
        if is_live_board && !self.live_painted {
            self.live_painted = true;
            harness::metric("live board applied");
        }
        self.run(effects, cx);
        cx.notify();
    }
}

impl Shell {
    fn run(&mut self, effects: Vec<Effect>, cx: &mut Context<Self>) {
        let mut save_outbox = false;
        let mut sends = Vec::new();
        for effect in effects {
            match effect {
                Effect::Stream { generation, agents } => {
                    self.stream = None; // Dropping a reader closes it.
                    let query = format!("agents={}", agents.join(","));
                    let tx = self.tx.clone();
                    let reader =
                        sse::Reader::spawn(self.client.base().into(), query, move |wire| {
                            let event = wire.map_or(StreamEvent::Dropped, StreamEvent::Frame);
                            let _ = tx.unbounded_send(Event::Stream { generation, event });
                        });
                    self.stream = Some(reader);
                }
                Effect::Fetch(fetch) => {
                    let (client, tx) = (self.client.clone(), self.tx.clone());
                    cx.background_executor()
                        .spawn(async move {
                            let _ = tx.unbounded_send(run_fetch(&client, fetch));
                        })
                        .detach();
                }
                Effect::Send(write) => sends.push(write),
                Effect::Retry { ns, after_ms } => {
                    let step = Step::Retry;
                    self.later(after_ms, Event::Sync { ns, step }, cx)
                }
                Effect::RetryViewer { after_ms } => self.later(after_ms, Event::ViewerRetry, cx),
                Effect::Persist(Persist::Outbox) => save_outbox = true,
                Effect::Persist(Persist::Prefs) => {
                    self.save_later(local::PREFS, PREFS_COALESCE, cx)
                }
                Effect::Persist(Persist::Snapshot) => {
                    self.save_later(local::SNAPSHOT, SNAPSHOT_COALESCE, cx)
                }
            }
        }
        if !save_outbox && sends.is_empty() {
            return;
        }
        // Saved now even when this batch did not change the outbox: an edit's own save may still be
        // waiting on another task, and a send must never overtake it.
        let (bytes, seq) = (local::encode(&self.store.outbox()), local::next_seq());
        let (disk, client, tx) = (self.disk.clone(), self.client.clone(), self.tx.clone());
        cx.background_executor()
            .spawn(async move {
                save_then_send(&disk, &client, &bytes, seq, sends, |event| {
                    let _ = tx.unbounded_send(event);
                })
            })
            .detach();
    }

    /// Dispatch `event` after `after_ms`.
    fn later(&self, after_ms: u64, event: Event, cx: &mut Context<Self>) {
        let tx = self.tx.clone();
        let timer = cx
            .background_executor()
            .timer(Duration::from_millis(after_ms));
        cx.background_executor()
            .spawn(async move {
                timer.await;
                let _ = tx.unbounded_send(event);
            })
            .detach();
    }

    /// Write `name` once `delay` has passed, from the store as it is then. A write already waiting
    /// covers this change too.
    fn save_later(&mut self, name: &'static str, delay: Duration, cx: &mut Context<Self>) {
        if !self.saves.insert(name) {
            return;
        }
        cx.spawn(async move |this, cx| {
            cx.background_executor().timer(delay).await;
            let Ok((bytes, disk)) = this.update(cx, |s, _| {
                s.saves.remove(name);
                let bytes = match name {
                    local::PREFS => local::encode(&s.store.prefs),
                    _ => local::encode(&s.store.snapshot()),
                };
                (bytes, s.disk.clone())
            }) else {
                return;
            };
            let seq = local::next_seq();
            cx.background_executor()
                .spawn(async move {
                    if let Err(e) = disk.write(name, &bytes, seq) {
                        eprintln!("local: could not save {name}: {e}");
                    }
                })
                .await;
        })
        .detach();
    }
}

fn run_fetch(client: &Client, fetch: Fetch) -> Event {
    match fetch {
        Fetch::Viewer => Event::Viewer(client.viewer().map(|v| v.viewer).map_err(|e| {
            eprintln!("viewer: {e}");
            e.status()
        })),
        Fetch::State { ns, since } => {
            let step = match client.state(ns.name(), since) {
                Ok(rows) => Step::Pulled(rows),
                Err(e) => {
                    eprintln!("state {}: {e}", ns.name());
                    Step::PullFailed(e.status())
                }
            };
            Event::Sync { ns, step }
        }
        Fetch::StatusLine { agent, turn } => {
            let tail = client.entries(&agent, &Page::Tail { limit: STATUS_TAIL });
            let line = tail.ok().and_then(|t| transcript::status_line(&t.entries));
            Event::StatusLine { agent, turn, line }
        }
    }
}

/// Save the outbox, then post each write and report its answer. A failed save posts nothing: every
/// write comes back as a transport-style failure, which backs off and tries again (saving first again).
pub fn save_then_send(
    disk: &Disk,
    client: &Client,
    outbox: &[u8],
    seq: u64,
    sends: Vec<Write>,
    mut on: impl FnMut(Event),
) {
    let saved = disk.write(local::OUTBOX, outbox, seq);
    if let Err(e) = &saved {
        eprintln!("local: could not save outbox.json, not sending: {e}");
    }
    for Write::State { ns, rows } in sends {
        let step = match &saved {
            Err(_) => Step::PostFailed(None),
            Ok(()) => match client.post_state(ns.name(), &rows) {
                Ok(_) => Step::Posted,
                Err(e) => {
                    eprintln!("state {} post: {e}", ns.name());
                    Step::PostFailed(e.status())
                }
            },
        };
        on(Event::Sync { ns, step });
    }
}

impl Render for Shell {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if !self.painted {
            self.painted = true;
            let from = if self.store.spaces.is_empty() {
                "empty"
            } else {
                "snapshot"
            };
            window.on_next_frame(move |_, _| harness::metric(format!("first paint ({from})")));
        }
        let t = theme::type_scale(self.store.prefs.text_scale);
        let lens = lens::render(&self.store, &self.ui, t, cx);
        div()
            .id("root")
            .key_context("Lens")
            .on_action(|_: &Quit, _, cx| cx.quit())
            .on_action(cx.listener(|s, _: &TextBigger, _, cx| {
                s.dispatch(Event::TextScale(TextScale::Bigger), cx)
            }))
            .on_action(cx.listener(|s, _: &TextSmaller, _, cx| {
                s.dispatch(Event::TextScale(TextScale::Smaller), cx)
            }))
            .on_action(cx.listener(|s, _: &TextReset, _, cx| {
                s.dispatch(Event::TextScale(TextScale::Reset), cx)
            }))
            .size_full()
            .bg(rgb(theme::pal::GROUND))
            .text_color(rgb(theme::pal::INK))
            .font_family(theme::FONT)
            .text_size(t.body)
            .line_height(t.line)
            .child(lens)
    }
}

pub fn run() {
    harness::start_clock();
    let script = harness::script();
    gpui_kit::application().run(move |cx| {
        theme::seed(cx);
        gpui_kit::init(cx);
        cx.set_app_identity(APP_ID, APP_NAME);
        theme::dark(cx);
        // App-wide chords bind on `Lens` alone; navigation letters (`lens::bind`) use
        // `Lens && !Input && !Terminal`, because a predicate sees the whole focus stack.
        cx.bind_keys([
            KeyBinding::new("cmd-q", Quit, Some("Lens")),
            KeyBinding::new("cmd-=", TextBigger, Some("Lens")),
            KeyBinding::new("cmd-shift-=", TextBigger, Some("Lens")),
            KeyBinding::new("cmd--", TextSmaller, Some("Lens")),
            KeyBinding::new("cmd-0", TextReset, Some("Lens")),
        ]);
        lens::bind(cx);
        harness::metric("init done");

        let automated = script.is_some();
        let opts = WindowOptions {
            window_bounds: Some(WindowBounds::centered(size(px(1400.), px(900.)), cx)),
            titlebar: Some(TitlebarOptions {
                title: Some(APP_NAME.into()),
                ..Default::default()
            }),
            // An automated run never takes focus (settled decision 8).
            focus: !automated,
            ..Default::default()
        };
        let (handle, shell) =
            gpui_kit::open_window(opts, cx, |_, cx| cx.new(Shell::new)).expect("window");
        if automated {
            platform_mac::order_windows_back();
        } else {
            cx.activate(true);
        }
        let _ = handle.update(cx, |_, window, cx| {
            let focus = shell.read(cx).ui.focus_target().clone();
            window.focus(&focus, cx);
            if let Some(script) = script.clone() {
                window
                    .spawn(cx, async move |cx| harness::run(script, cx).await)
                    .detach();
            }
        });
        harness::metric("window opened");
        shell.update(cx, |s, cx| s.dispatch(Event::Boot, cx));
    });
}
