//! The app shell: owns the `Store`, the background threads, the one channel into the foreground, the
//! window and the keymap. Views render from the store it holds and own their own widget entities; every
//! change to the store goes through `Shell::dispatch`, which runs `Store::apply` and then the effects.
//!
//! Boot order matters for the 300 ms budget: theme fonts are seeded before `gpui_kit::init` (the
//! kit otherwise enumerates every installed font to resolve `.SystemUIFont`, ~150 ms), then the window
//! opens and paints from the disk snapshot, which is applied synchronously before the stream thread or
//! any REST call starts. `Event::Boot` then opens the stream and pulls in parallel.
//!
//! Threads: the SSE read blocks on its own `std::thread` (`sse::Reader`); REST calls and disk writes run on GPUI's
//! background executor. All of them report back as `Event`s on the one channel, which one foreground
//! task drains into `dispatch`.

use crate::api::client::{Client, base_url};
use crate::api::{Wire, sse};
use crate::store::{Effect, Event, Fetch, Persist, Store, StreamEvent, TextScale, Write};
use crate::views::{debug, theme};
use crate::{harness, local, platform_mac};
use futures::StreamExt as _;
use futures::channel::mpsc::{UnboundedSender, unbounded};
use gpui_kit::component::theme::{ThemeConfig, ThemeRegistry};
use gpui_kit::component::{ActiveTheme as _, Theme, ThemeMode};
use gpui_kit::*;
use std::collections::HashSet;
use std::time::Duration;

/// Window title and app name.
pub const APP_NAME: &str = "herder native";
const APP_ID: &str = "dev.herder.native";
/// The families are explicit so the kit never enumerates fonts (see the module doc). U2 owns the choice.
const FONT: &str = "Menlo";
const MONO: &str = "Monaco";
/// A burst of changes (a held ⌘+, a run of fleet frames) becomes one write of the latest state.
const PREFS_COALESCE: Duration = Duration::from_millis(150);
const SNAPSHOT_COALESCE: Duration = Duration::from_secs(1);

actions!(herder, [Quit, TextBigger, TextSmaller, TextReset]);

pub struct Shell {
    pub store: Store,
    focus: FocusHandle,
    client: Client,
    tx: UnboundedSender<Event>,
    stream: Option<sse::Reader>,
    /// Files with a write waiting out its coalescing window; it reads the store when it fires.
    saves: HashSet<&'static str>,
    painted: bool,
    live_painted: bool,
}

impl Shell {
    fn new(cx: &mut Context<Self>) -> Self {
        let mut store = Store::default();
        if let Some(prefs) = local::load_prefs() {
            store.apply(Event::PrefsLoaded(prefs));
        }
        // Local state first, synchronously: nothing live has started yet, so nothing can be overwritten.
        if let Some(snapshot) = local::load_snapshot() {
            store.apply(Event::Snapshot(snapshot));
        }
        if let Some(outbox) = local::load_outbox() {
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
        Shell {
            store,
            focus: cx.focus_handle(),
            client: Client::new(base_url()),
            tx,
            stream: None,
            saves: HashSet::new(),
            painted: false,
            live_painted: false,
        }
    }

    /// The only path to a state change: reduce, then run the effects.
    pub fn dispatch(&mut self, event: Event, cx: &mut Context<Self>) {
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

    fn run(&mut self, effects: Vec<Effect>, cx: &mut Context<Self>) {
        // The outbox is written before any send in the same batch, on the same background task.
        let mut outbox = None;
        let mut sends = Vec::new();
        for effect in effects {
            match effect {
                Effect::Stream { generation, agents } => {
                    if let Some(old) = self.stream.take() {
                        old.close();
                    }
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
                    let tx = self.tx.clone();
                    let timer = cx
                        .background_executor()
                        .timer(Duration::from_millis(after_ms));
                    cx.background_executor()
                        .spawn(async move {
                            timer.await;
                            let _ = tx.unbounded_send(Event::Retry(ns));
                        })
                        .detach();
                }
                Effect::Persist(Persist::Outbox) => {
                    outbox = Some((local::encode(&self.store.outbox()), local::next_seq()));
                }
                Effect::Persist(Persist::Prefs) => {
                    self.save_later(local::PREFS, PREFS_COALESCE, cx)
                }
                Effect::Persist(Persist::Snapshot) => {
                    self.save_later(local::SNAPSHOT, SNAPSHOT_COALESCE, cx)
                }
            }
        }
        if outbox.is_none() && sends.is_empty() {
            return;
        }
        let (client, tx) = (self.client.clone(), self.tx.clone());
        cx.background_executor()
            .spawn(async move {
                if let Some((bytes, seq)) = outbox {
                    local::write(local::OUTBOX, &bytes, seq);
                }
                for write in sends {
                    let _ = tx.unbounded_send(run_send(&client, write));
                }
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
            let Ok(bytes) = this.update(cx, |s, _| {
                s.saves.remove(name);
                match name {
                    local::PREFS => local::encode(&s.store.prefs),
                    _ => local::encode(&s.store.snapshot()),
                }
            }) else {
                return;
            };
            let seq = local::next_seq();
            cx.background_executor()
                .spawn(async move { local::write(name, &bytes, seq) })
                .await;
        })
        .detach();
    }
}

fn run_fetch(client: &Client, fetch: Fetch) -> Event {
    match fetch {
        Fetch::Viewer => Event::Viewer(client.viewer().ok().map(|v| v.viewer)),
        Fetch::State { ns, since } => match client.state(ns.name(), since) {
            Ok(rows) => Event::Pulled { ns, rows },
            Err(e) => {
                eprintln!("state {}: {e}", ns.name());
                Event::PullFailed {
                    ns,
                    status: e.status(),
                }
            }
        },
    }
}

fn run_send(client: &Client, write: Write) -> Event {
    match write {
        Write::State { ns, rows } => match client.post_state(ns.name(), &rows) {
            Ok(_) => Event::Posted { ns },
            Err(e) => {
                eprintln!("state {} post: {e}", ns.name());
                Event::PostFailed {
                    ns,
                    status: e.status(),
                }
            }
        },
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
        let colors = cx.theme();
        let t = theme::type_scale(self.store.prefs.text_scale);
        div()
            .id("root")
            .track_focus(&self.focus)
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
            .bg(colors.background)
            .text_color(colors.foreground)
            .font_family(FONT)
            .text_size(t.body)
            .line_height(t.line)
            .child(debug::render(&self.store, t, colors.muted_foreground))
    }
}

fn with_fonts(c: &ThemeConfig) -> std::rc::Rc<ThemeConfig> {
    let mut c = c.clone();
    c.font_family = Some(FONT.into());
    c.mono_font_family = Some(MONO.into());
    std::rc::Rc::new(c)
}

/// Seed explicit font families before `gpui_kit::init` so it never enumerates installed fonts.
fn seed_theme(cx: &mut App) {
    let mut theme = Theme::default();
    theme.light_theme = with_fonts(&ThemeConfig::default());
    theme.dark_theme = theme.light_theme.clone();
    theme.font_family = FONT.into();
    theme.mono_font_family = MONO.into();
    cx.set_global(theme);
}

pub fn run() {
    harness::start_clock();
    let script = harness::script();
    gpui_kit::application().run(move |cx| {
        seed_theme(cx);
        gpui_kit::init(cx);
        cx.set_app_identity(APP_ID, APP_NAME);
        // v0 is dark only: the kit's default dark theme, with our fonts.
        let dark = with_fonts(ThemeRegistry::global(cx).default_dark_theme());
        Theme::global_mut(cx).dark_theme = dark;
        Theme::change(ThemeMode::Dark, None, cx);
        Theme::sync_base(cx);
        // App-wide chords bind on `Lens` alone; navigation letters (U2+) must use
        // `Lens && !Input && !Terminal`, because a predicate sees the whole focus stack.
        cx.bind_keys([
            KeyBinding::new("cmd-q", Quit, Some("Lens")),
            KeyBinding::new("cmd-=", TextBigger, Some("Lens")),
            KeyBinding::new("cmd-shift-=", TextBigger, Some("Lens")),
            KeyBinding::new("cmd--", TextSmaller, Some("Lens")),
            KeyBinding::new("cmd-0", TextReset, Some("Lens")),
        ]);
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
            let focus = shell.read(cx).focus.clone();
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
