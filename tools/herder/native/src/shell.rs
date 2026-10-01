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
//! send was decided (`io::save_then_send`); a save still pending elsewhere cannot be overtaken. A note
//! transfer's destination is saved and reported back before its source changes: a draft at once
//! (`io::save_then_land`), a queued note by that same outbox save, before its posts. The REST reads
//! and those saves run in `io`.

use crate::api::client::{Client, base_url};
use crate::api::{Wire, sse};
use crate::local::{self, Disk};
use crate::store::{Effect, Event, Persist, Store, StreamEvent, TextScale};
use crate::views::transcript as transcript_view;
use crate::views::{
    Frame, Host, Quit, TextBigger, TextReset, TextSmaller, composer, lens, markdown, notes,
    notes_list, probe, space, theme,
};
use crate::{harness, platform_mac};
use futures::StreamExt as _;
use futures::channel::mpsc::{UnboundedSender, unbounded};
use gpui_kit::*;
use io::run_fetch;
pub use io::{Batch, save_then_land, save_then_message, save_then_send};
use std::collections::HashSet;
use std::sync::Arc;
use std::time::Duration;

mod io;

/// Window title and app name.
pub const APP_NAME: &str = "herder native";
const APP_ID: &str = "dev.herder.native";
/// A burst of changes (a held ⌘+, a run of fleet frames) becomes one write of the latest state.
const PREFS_COALESCE: Duration = Duration::from_millis(150);
const SNAPSHOT_COALESCE: Duration = Duration::from_secs(1);

pub struct Shell {
    pub store: Store,
    ui: lens::Ui,
    client: Client,
    disk: Arc<Disk>,
    tx: UnboundedSender<Event>,
    stream: Option<sse::Reader>,
    /// Files with a write waiting out its coalescing window; it reads the store when it fires.
    saves: HashSet<Persist>,
    painted: bool,
    live_painted: bool,
}

impl Shell {
    fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
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
                let ok = match event {
                    // The chord toggles: hide when frontmost, else come forward on the lens.
                    Event::Summon => this
                        .read_with(cx, |s, _| s.store.alerts.front)
                        .map(|front| {
                            cx.update(|cx| if front { cx.hide() } else { summon("", cx) })
                        }),
                    event => this.update(cx, |s, cx| s.dispatch(event, cx)),
                };
                if ok.is_err() {
                    break;
                }
            }
        })
        .detach();
        // Seeded from the window: a first activation missed must not leave a frontmost app in the back.
        let assume = platform_mac::assume_front();
        store.apply(Event::Front(assume || window.is_window_active()));
        if !assume {
            cx.observe_window_activation(window, |s: &mut Self, window, cx| {
                s.dispatch(Event::Front(window.is_window_active()), cx)
            })
            .detach();
        }
        let mut ui = lens::Ui::new(window, cx);
        transcript_view::set_web(&mut ui, &base_url());
        Shell {
            store,
            ui,
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

    fn view(&self) -> (&Store, &lens::Ui) {
        (&self.store, &self.ui)
    }

    /// A scripted run never touches the owner's clipboard: it logs what it would have copied.
    fn copy(&mut self, text: String, cx: &mut Context<Self>) {
        match platform_mac::quiet() {
            true => platform_mac::log(format!("would copy {:?}", text)),
            false => cx.write_to_clipboard(ClipboardItem::new_string(text)),
        }
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
        let (mut batch, mut filed) = (Batch::default(), Vec::new());
        for effect in effects.into_iter().filter_map(|e| batch.take(e)) {
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
                Effect::Fetch(fetch) => self.background(cx, |_, client| run_fetch(client, fetch)),
                Effect::Message { agent, text } => {
                    let ((_, bytes, seq), send) = (self.bytes(Persist::Prefs), (agent, text));
                    self.background(cx, move |disk, client| {
                        save_then_message(disk, client, &bytes, seq, send)
                    })
                }
                Effect::After { after_ms, wake } => self.later(after_ms, Event::Wake(wake), cx),
                Effect::Persist(file) => self.save_later(file, cx),
                // Taken by the batch (`Batch::take`), as is the outbox's persist.
                Effect::Post { .. } | Effect::Transfer { .. } => {}
                Effect::HandedOff {
                    agent,
                    order,
                    removed,
                } => notes_list::handed_off(&mut self.ui, &agent, &order, &removed),
                Effect::FiledBack { agent } => {
                    filed.extend(composer::filed_back(&self.store, &mut self.ui, &agent, cx))
                }
                Effect::Notify(n) if platform_mac::quiet() => {
                    platform_mac::log(format!("would notify {}: {} / {}", n.tag, n.title, n.body))
                }
                Effect::Notify(n) => cx.show_system_notification(SystemNotification {
                    tag: n.tag.into(),
                    title: n.title.into(),
                    body: n.body.into(),
                    actions: Vec::new(),
                }),
                Effect::Badge(n) => platform_mac::badge(n),
                Effect::OpenFile { path, line } => {
                    match markdown::vscode_url(&self.store.prefs.vscode_host, &path, line) {
                        Some(url) => cx.open_url(&url),
                        None => eprintln!("open: no VS Code URL for {path}"),
                    }
                }
            }
        }
        for agent in std::mem::take(&mut batch.drafts) {
            let (_, bytes, seq) = self.bytes(Persist::Prefs);
            self.background(cx, move |disk, _| save_then_land(disk, &bytes, seq, agent))
        }
        for event in filed {
            self.dispatch(event, cx);
        }
        if !batch.save && !batch.waits() {
            return;
        }
        // Saved now even when this batch did not change the outbox: an edit's own save may still be
        // waiting on another task, and a send must never overtake it.
        let (_, bytes, seq) = self.bytes(Persist::Outbox);
        let (disk, client, tx) = (self.disk.clone(), self.client.clone(), self.tx.clone());
        cx.background_executor()
            .spawn(async move {
                save_then_send(
                    &disk,
                    &client,
                    &bytes,
                    seq,
                    batch.sends,
                    batch.lands,
                    |event| {
                        let _ = tx.unbounded_send(event);
                    },
                )
            })
            .detach();
    }

    /// Run `f` on the background executor and dispatch the event it returns.
    fn background(
        &self,
        cx: &mut Context<Self>,
        f: impl FnOnce(&Disk, &Client) -> Event + Send + 'static,
    ) {
        let (disk, client, tx) = (self.disk.clone(), self.client.clone(), self.tx.clone());
        let task = async move { drop(tx.unbounded_send(f(&disk, &client))) };
        cx.background_executor().spawn(task).detach();
    }

    /// Dispatch `event` after `after_ms`.
    fn later(&self, after_ms: u64, event: Event, cx: &mut Context<Self>) {
        let (tx, after) = (self.tx.clone(), Duration::from_millis(after_ms));
        let timer = cx.background_executor().timer(after);
        let task = async move {
            timer.await;
            drop(tx.unbounded_send(event))
        };
        cx.background_executor().spawn(task).detach();
    }

    /// `file` as the store holds it now: its name, its bytes, and the sequence that orders this write
    /// after every earlier one.
    fn bytes(&self, file: Persist) -> (&'static str, Vec<u8>, u64) {
        let (name, bytes) = match file {
            Persist::Prefs => (local::PREFS, local::encode(&self.store.prefs)),
            Persist::Outbox => (local::OUTBOX, local::encode(&self.store.outbox())),
            Persist::Snapshot => (local::SNAPSHOT, local::encode(&self.store.snapshot())),
        };
        (name, bytes, local::next_seq())
    }

    /// Write `file` once its coalescing delay has passed, from the store as it is then. A write already
    /// waiting covers this change too.
    fn save_later(&mut self, file: Persist, cx: &mut Context<Self>) {
        if !self.saves.insert(file) {
            return;
        }
        let delay = match file {
            Persist::Prefs => PREFS_COALESCE,
            _ => SNAPSHOT_COALESCE,
        };
        cx.spawn(async move |this, cx| {
            cx.background_executor().timer(delay).await;
            let Ok(((name, bytes, seq), disk)) = this.update(cx, |s, _| {
                s.saves.remove(&file);
                (s.bytes(file), s.disk.clone())
            }) else {
                return;
            };
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

impl Render for Shell {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if !std::mem::replace(&mut self.painted, true) {
            let empty = self.store.spaces.is_empty();
            let from = if empty { "empty" } else { "snapshot" };
            window.on_next_frame(move |_, _| harness::metric(format!("first paint ({from})")));
        }
        harness::RENDERS.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
        composer::sync(&mut self.ui, &self.store, window, cx);
        notes::sync(&mut self.ui, &self.store, window, cx);
        let t = theme::type_scale(self.store.prefs.text_scale);
        let lens = lens::render(&self.store, &self.ui, t, window.viewport_size(), cx);
        div()
            .id("root")
            .key_context("Lens")
            .on_action(|_: &Quit, _, cx| cx.quit())
            .on_action(crate::views::on(cx, |store, ui, s: &space::Summon| {
                space::summon(store, ui, &s.0)
            }))
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
    let script = harness::script().unwrap_or_else(|e| {
        eprintln!("harness: {e}");
        std::process::exit(2)
    });
    gpui_kit::application().run(move |cx| {
        theme::seed(cx);
        gpui_kit::init(cx);
        cx.set_app_identity(APP_ID, APP_NAME);
        theme::dark(cx);
        crate::views::bind(cx);
        harness::metric("init done");

        let automated = script.is_some();
        let opts = WindowOptions {
            window_bounds: Some(WindowBounds::centered(harness::window_size(), cx)),
            titlebar: Some(TitlebarOptions {
                title: Some(APP_NAME.into()),
                ..Default::default()
            }),
            // An automated run never takes focus (settled decision 8).
            focus: !automated,
            ..Default::default()
        };
        let mut shell = None;
        let frame = |window: &mut Window, cx: &mut App| {
            let s = cx.new(|cx| Shell::new(window, cx));
            let dots = s.read(cx).ui.dots.clone();
            shell = Some(s.clone());
            cx.new(|cx| Frame::new(s.into(), dots, cx))
        };
        let (handle, _) = gpui_kit::open_window(opts, cx, frame).expect("window");
        let shell = shell.expect("the window built the shell");
        if automated {
            platform_mac::order_back();
        } else {
            cx.activate(true);
        }
        let _ = handle.update(cx, |_, window, cx| {
            let focus = shell.read(cx).ui.focus_target().clone();
            window.focus(&focus, cx);
            if let Some(script) = script.clone() {
                let probe = shell.clone();
                window
                    .spawn(cx, async move |cx| harness::run(script, probe, cx).await)
                    .detach();
            }
        });
        harness::metric("window opened");
        shell.update(cx, |s, cx| s.dispatch(Event::Boot, cx));
        // The run loop has started: the chord and notification clicks can be wired (U6).
        let (chord, tx) = shell.read_with(cx, |s, _| (s.store.prefs.hotkey.clone(), s.tx.clone()));
        platform_mac::summon_chord(&chord, move || drop(tx.unbounded_send(Event::Summon)));
        if !platform_mac::quiet() {
            cx.on_system_notification_response(|response, cx| summon(&response.tag, cx));
        }
    });
}

impl harness::Probe for Entity<Shell> {
    fn ask(&self, op: &str, window: &Window, cx: &App) -> Option<String> {
        let s = self.read(cx);
        probe::ask(&s.store, &s.ui, op, window, cx)
    }

    fn action(&self, op: &str, arg: &str, cx: &App) -> Option<Box<dyn Action>> {
        let s = self.read(cx);
        probe::action(&s.store, &s.ui, op, arg)
    }

    fn select(&self, text: &str, cx: &mut App) {
        self.update(cx, |s, cx| {
            probe::select(&mut s.ui, text);
            cx.notify()
        })
    }

    fn find(&self, text: &str, cx: &mut App) -> bool {
        let s = self.read(cx);
        probe::find(&s.store, &s.ui, text)
    }
}

/// Bring the app forward, to a notification's agent or space (`space::summon`); `""` is the lens.
fn summon(tag: &str, cx: &mut App) {
    cx.activate(true);
    let Some(window) = cx.windows().into_iter().next() else {
        return;
    };
    let _ = window.update(cx, |_, window, cx| {
        window.activate_window();
        window.dispatch_action(Box::new(space::Summon(tag.to_string().into())), cx);
    });
}
