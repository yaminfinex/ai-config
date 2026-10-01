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
//! send was decided (`io::save_then_send`); a save still pending elsewhere cannot be overtaken. The
//! REST reads and that save run in `io`.

use crate::api::client::{Client, base_url};
use crate::api::{Wire, sse};
use crate::local::{self, Disk};
use crate::store::sync::Step;
use crate::store::transcript;
use crate::store::{Effect, Event, Persist, Store, StreamEvent, TextScale, Write};
use crate::views::transcript as transcript_view;
use crate::views::{
    Frame, Host, Quit, TextBigger, TextReset, TextSmaller, composer, lens, markdown, space, theme,
};
use crate::{harness, platform_mac};
use futures::StreamExt as _;
use futures::channel::mpsc::{UnboundedSender, unbounded};
use gpui_kit::*;
use io::run_fetch;
pub use io::{save_then_message, save_then_send};
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
    saves: HashSet<&'static str>,
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
                if this.update(cx, |s, cx| s.dispatch(event, cx)).is_err() {
                    break;
                }
            }
        })
        .detach();
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
        let (mut sends, mut filed) = (Vec::new(), Vec::new());
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
                Effect::Fetch(fetch) => self.background(cx, |_, client| run_fetch(client, fetch)),
                Effect::Send(Write::Message { agent, text }) => {
                    let (bytes, seq) = (local::encode(&self.store.prefs), local::next_seq());
                    let send = (agent, text);
                    self.background(cx, move |disk, client| {
                        save_then_message(disk, client, &bytes, seq, send)
                    })
                }
                Effect::Send(write) => sends.push(write),
                Effect::Retry { ns, after_ms } => {
                    let step = Step::Retry;
                    self.later(after_ms, Event::Sync { ns, step }, cx)
                }
                Effect::RetryViewer { after_ms } => self.later(after_ms, Event::ViewerRetry, cx),
                Effect::RetryTranscript { timer, after_ms } => {
                    let step = transcript::Step::Retry(timer);
                    self.later(after_ms, Event::Transcript(step), cx)
                }
                Effect::Persist(Persist::Outbox) => save_outbox = true,
                Effect::Persist(Persist::Prefs) => {
                    self.save_later(local::PREFS, PREFS_COALESCE, cx)
                }
                Effect::Persist(Persist::Snapshot) => {
                    self.save_later(local::SNAPSHOT, SNAPSHOT_COALESCE, cx)
                }
                Effect::FiledBack { agent } => {
                    filed.extend(composer::filed_back(&self.store, &mut self.ui, &agent, cx))
                }
                Effect::OpenFile { path, line } => {
                    match markdown::vscode_url(&self.store.prefs.vscode_host, &path, line) {
                        Some(url) => cx.open_url(&url),
                        None => eprintln!("open: no VS Code URL for {path}"),
                    }
                }
            }
        }
        for event in filed {
            self.dispatch(event, cx);
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

impl Render for Shell {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if !std::mem::replace(&mut self.painted, true) {
            let empty = self.store.spaces.is_empty();
            let from = if empty { "empty" } else { "snapshot" };
            window.on_next_frame(move |_, _| harness::metric(format!("first paint ({from})")));
        }
        harness::RENDERS.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
        composer::sync(&mut self.ui, &self.store, window, cx);
        let t = theme::type_scale(self.store.prefs.text_scale);
        let lens = lens::render(&self.store, &self.ui, t, window.viewport_size(), cx);
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
            platform_mac::order_windows(harness::visible());
        } else {
            cx.activate(true);
        }
        let _ = handle.update(cx, |_, window, cx| {
            let focus = shell.read(cx).ui.focus_target().clone();
            window.focus(&focus, cx);
            if let Some(script) = script.clone() {
                let [s, s2, s3, s4] = [(); 4].map(|_| shell.clone());
                let probe = harness::Probe {
                    shown: Box::new(move |cx| space::shown(&s.read(cx).store, &s.read(cx).ui)),
                    link: |url| Box::new(transcript_view::OpenLink(url.to_string().into())),
                    start: Box::new(move |cx| {
                        let open = s2.read(cx).store.transcript.open.as_ref();
                        let t = open.filter(|t| t.at_start())?;
                        Some(format!(
                            "{}: start reached, {} rows",
                            t.agent,
                            t.items.len()
                        ))
                    }),
                    composer: Box::new(move |w, cx| composer::probe(&s3.read(cx).ui, w, cx)),
                    says: Box::new(move |cx| composer::says(&s4.read(cx).store, &s4.read(cx).ui)),
                };
                window
                    .spawn(cx, async move |cx| harness::run(script, probe, cx).await)
                    .detach();
            }
        });
        harness::metric("window opened");
        shell.update(cx, |s, cx| s.dispatch(Event::Boot, cx));
    });
}
