//! The zoom shell (`Space` context): one space's agents as tabs over the zoomed agent's panel
//! (`panel`: its transcript, notes and composer), plus a preview tab for an outsider opened from a
//! mention (local only, never a member). Zooming into an agent marks it seen and clears the space's unread mark. A notified agent in
//! no space opens alone, as a preview in a zoom of no space (`Zoom::alone`); nothing joins a space.
//!
//! Transitions, as the spike: `enter` morphs the card's bounds to the window (280 ms) and `escape`
//! morphs back (200 ms); `[` `]` and `n` swipe sideways (240 ms). Each is one-shot, and the lens is
//! drawn underneath only while a morph runs.

use crate::store::spaces::{Move, Space};
use crate::store::transcript;
use crate::store::{Event, Store};
use crate::views::dock::{self, Ask};
use crate::views::lens::{self, Nav, State, Ui};
use crate::views::markdown::{AGENT, PATH};
use crate::views::theme::{TypeScale, pal};
use crate::views::{Host, dim, on};
use gpui_kit::*;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Duration;

/// Every zoom key is one action, like `lens::Nav`; `n` / `N` here are `lens::Nav::NextNeeding`.
#[derive(Clone, Copy, Debug, PartialEq, Action)]
#[action(namespace = space, no_json)]
pub enum Zoomed {
    Out,
    /// `[` `]`: the previous or next space in lens order, wrapping.
    Space(isize),
    /// `tab` / `shift-tab`, `alt-right` / `alt-left`: the next or previous tab in the focused group,
    /// wrapping.
    Agent(isize),
    /// `alt-u`: mark the zoomed agent read if it is unread, else unread (web's toggle; RM).
    Read,
}

/// A clicked tab: show that agent in this zoom.
#[derive(Clone, Debug, PartialEq, Action)]
#[action(namespace = space, no_json)]
pub struct Tab(pub SharedString);

/// Bring the app to a notification's agent (`agent:<name>`) or space (`space:<id>`), or to the lens
/// (`""`, the summon chord); U6.
#[derive(Clone, Debug, PartialEq, Action)]
#[action(namespace = space, no_json)]
pub struct Summon(pub SharedString);

/// Which space and agent are zoomed. `space` is empty in a zoom of no space.
#[derive(Clone, Debug, PartialEq)]
pub struct Zoom {
    pub space: String,
    pub agent: Option<String>,
}

impl Zoom {
    /// The zoom of no space (U6): a notified agent that sits in no space, alone as a preview.
    pub fn alone(&self) -> bool {
        self.space.is_empty()
    }
}

/// A zoom transition: morph in from the card, morph out to it (drawing the zoom being left), or a
/// sideways swipe (`1.0` from the right, `-1.0` from the left).
#[derive(Clone, Debug)]
pub struct Anim {
    kind: Kind,
    seq: u64,
    card: Option<Bounds<Pixels>>,
    leaving: Option<Zoom>,
}

#[derive(Clone, Copy, Debug, PartialEq)]
enum Kind {
    In,
    Out,
    Swipe(f32),
}

static SEQ: AtomicU64 = AtomicU64::new(0);

impl Anim {
    fn new(kind: Kind, card: Option<Bounds<Pixels>>, leaving: Option<Zoom>) -> Self {
        let seq = SEQ.fetch_add(1, Ordering::Relaxed);
        Anim {
            kind,
            seq,
            card,
            leaving,
        }
    }

    pub fn seq(&self) -> u64 {
        self.seq
    }

    pub fn duration(&self) -> Duration {
        Duration::from_millis(match self.kind {
            Kind::In => 280,
            Kind::Out => 200,
            Kind::Swipe(_) => 240,
        })
    }

    pub(super) fn leaving(&self) -> Option<&Zoom> {
        self.leaving.as_ref()
    }

    /// Morphing in: the lens shows around the growing card.
    pub(super) fn morphs(&self) -> bool {
        self.kind == Kind::In
    }

    /// `pane` moving between the card's bounds and the full window, or sliding in sideways.
    pub(super) fn animate(&self, pane: AnyElement, window: Size<Pixels>) -> AnyElement {
        let full = Bounds::new(point(px(0.), px(0.)), window);
        let middle =
            Bounds::centered_at(full.center(), size(window.width * 0.4, window.height * 0.4));
        let (kind, card) = (self.kind, self.card.unwrap_or(middle));
        let lerp = |a: Pixels, b: Pixels, t: f32| a + (b - a) * t;
        let id = ElementId::NamedInteger("zoom-anim".into(), self.seq);
        let wrap = div()
            .absolute()
            .overflow_hidden()
            .bg(rgb(pal::GROUND))
            .child(pane);
        let ease = Animation::new(self.duration()).with_easing(ease_out_quint());
        let step = move |el: Div, t: f32| match kind {
            Kind::Swipe(dir) => {
                let el = el.top_0().w(window.width).h(window.height);
                el.left(window.width * (0.3 * dir * (1.0 - t))).opacity(t)
            }
            Kind::In | Kind::Out => {
                let t = if kind == Kind::Out { 1.0 - t } else { t };
                el.left(lerp(card.origin.x, px(0.), t))
                    .top(lerp(card.origin.y, px(0.), t))
                    .w(lerp(card.size.width, window.width, t))
                    .h(lerp(card.size.height, window.height, t))
                    .rounded(px(8.) * (1.0 - t))
                    .opacity(0.3 + 0.7 * t)
            }
        };
        wrap.with_animation(id, ease, step).into_any_element()
    }
}

/// Zoom into `space` at its first agent needing you, else its visible one, marking it seen. From
/// the lens this morphs out of the card; `swipe` slides in from that side instead.
pub fn zoom_into(store: &Store, ui: &mut State, space: &Space, swipe: Option<f32>) -> Vec<Event> {
    let agent = space
        .agents()
        .find(|a| store.agent_needs_you(a))
        .or_else(|| store.visible(space));
    zoom_to(ui, &space.id, agent.map(str::to_string), swipe)
}

/// Zoom into the space `id` (empty: no space) at `agent`.
fn zoom_to(ui: &mut State, id: &str, agent: Option<String>, swipe: Option<f32>) -> Vec<Event> {
    let kind = match (swipe, &ui.zoom) {
        (Some(dir), _) => Kind::Swipe(dir),
        (None, None) => Kind::In,
        (None, Some(_)) => Kind::Swipe(0.0),
    };
    let card = ui.cards.borrow().get(id).copied();
    ui.anim = Some(Anim::new(kind, card, None));
    if !id.is_empty() {
        ui.select(id);
    }
    show(ui, id.to_string(), agent);
    Vec::new()
}

/// A summon (`Summon`): a notified agent is zoomed into in the first space holding it, or alone in a
/// zoom of no space when no space holds it, and seen even when that zoom is already open; otherwise
/// the zoom closes onto the lens, with the summary's space selected.
pub fn summon(store: &Store, ui: &mut State, tag: &str) -> Vec<Event> {
    if let Some(agent) = tag.strip_prefix("agent:").filter(|a| !a.is_empty()) {
        let home = store.home(agent);
        let to = Zoom {
            space: home.map(|s| s.id.clone()).unwrap_or_default(),
            agent: Some(agent.into()),
        };
        if ui.zoom.as_ref() == Some(&to) {
            if !to.alone() {
                ui.select(&to.space);
            }
            let Zoom { space, agent } = to;
            let beside = Vec::new();
            return vec![Event::Lens(Move::View {
                space,
                agent,
                beside,
            })];
        }
        return zoom_to(ui, &to.space, to.agent, None);
    }
    let out = act(store, ui, Zoomed::Out);
    if let Some(id) = tag.strip_prefix("space:") {
        ui.select(id);
    }
    out
}

/// The zoom is on `agent` in `space`: the dock shows its tab, opened if it has none, and focuses it
/// (`dock::sync`), which tells the store.
pub(super) fn show(ui: &mut State, space: String, agent: Option<String>) {
    ui.zoom = Some(Zoom { space, agent });
}

/// A link clicked in the zoomed agent's panel: a path resolves and opens in VS Code; an agent opens
/// its tab (a preview unless a member) in this group, or `beside` it (an `alt`-click).
pub(super) fn open(ui: &mut State, url: &str, beside: bool) -> Vec<Event> {
    let (Some(zoom), Some(shown)) = (ui.zoom.clone(), ui.zoomed_agent()) else {
        return Vec::new();
    };
    if let Some(mention) = url.strip_prefix(PATH) {
        let (agent, mention) = (shown.to_string(), mention.to_string());
        return vec![Event::Transcript(transcript::Step::OpenPath {
            agent,
            mention,
        })];
    }
    let Some(agent) = url.strip_prefix(AGENT) else {
        return Vec::new();
    };
    // Linked names are board names; one that has left the board since (retired) opens read-only.
    if agent.is_empty() || zoom.agent.as_deref() == Some(agent) {
        return Vec::new();
    }
    if beside {
        ui.asks.push(Ask::Beside);
    }
    show(ui, zoom.space, Some(agent.to_string()));
    Vec::new()
}

/// A clicked tab: its agent.
fn tab(ui: &mut State, agent: &str) -> Vec<Event> {
    if let Some(zoom) = ui.zoom.clone() {
        show(ui, zoom.space, Some(agent.to_string()));
    }
    Vec::new()
}

pub(super) fn zoomed<'a>(store: &'a Store, zoom: &Zoom) -> Option<&'a Space> {
    store.spaces.iter().find(|s| s.id == zoom.space)
}

pub fn act(store: &Store, ui: &mut State, key: Zoomed) -> Vec<Event> {
    let Some(zoom) = ui.zoom.clone() else {
        return Vec::new();
    };
    if key == Zoomed::Read {
        let agent = zoom.agent.map(Move::Toggle);
        return agent.map(Event::Lens).into_iter().collect();
    }
    // Alone, there is no space to move through: only `escape` does anything.
    if zoom.alone() && key != Zoomed::Out {
        return Vec::new();
    }
    let (Some(space), Zoomed::Space(by) | Zoomed::Agent(by)) = (zoomed(store, &zoom), key) else {
        // `escape`, or the space has gone: morph back to its card, letting the transcript go.
        let card = ui.cards.borrow().get(&zoom.space).copied();
        ui.panels.values().for_each(|p| p.transcript.clear());
        if let Some(dock) = ui.dock.as_mut() {
            dock.told = None;
        }
        ui.anim = Some(Anim::new(Kind::Out, card, ui.zoom.take()));
        ui.reveal.set(true);
        return vec![Event::Transcript(transcript::Step::Hide)];
    };
    match key {
        Zoomed::Space(by) => {
            let order = store.lens();
            let at = order.iter().position(|o| o.id == space.id).unwrap_or(0);
            let next = (at as isize + by).rem_euclid(order.len().max(1) as isize) as usize;
            zoom_into(store, ui, order[next], Some(by as f32))
        }
        // The focused group's next tab (`dock::sync`).
        _ => {
            ui.asks.push(Ask::Step(by));
            Vec::new()
        }
    }
}

pub fn render<H: Host>(
    store: &Store,
    ui: &Ui,
    zoom: &Zoom,
    t: TypeScale,
    cx: &mut Context<H>,
) -> AnyElement {
    let space = zoomed(store, zoom);
    let current = zoom.agent.as_deref();
    let gone = if zoom.alone() {
        "no space"
    } else {
        "(space gone)"
    };
    let lens = div()
        .id("crumb-lens")
        .cursor_pointer()
        .hover(|s| s.text_color(rgb(pal::ACC)))
        .on_click(|_, window, cx| window.dispatch_action(Zoomed::Out.boxed_clone(), cx))
        .child("lens ›");
    let crumb = format!(
        "{} › {}",
        space.map_or(gone, |s| s.name.as_str()),
        current.unwrap_or("no agents")
    );
    let crumb = div().flex().gap(t.px(8.)).child(lens).child(crumb);
    // A slim line over the dock (DK2): where the zoom is, and the keys; ⌥⏎ restores a maximized group.
    let maximized = (ui.dock.as_ref()).is_some_and(|d| d.area.read(cx).zoomed_group().is_some());
    let keys = match (zoom.alone(), maximized) {
        (true, _) => "esc lens",
        (false, false) => "esc lens · [ ] spaces · tab tabs · ⌘W close · ⌥⏎ maximize",
        (false, true) => "esc lens · [ ] spaces · tab tabs · ⌘W close · ⌥⏎ restore",
    };
    let bar = div()
        .flex()
        .flex_none()
        .items_center()
        .gap(t.px(16.))
        .h(t.css(26.))
        .px(t.css(12.))
        .text_size(t.small)
        .text_color(rgb(pal::SLATE))
        .border_b_1()
        .border_color(rgb(pal::RULE))
        .child(div().flex_1().child(crumb))
        .child(dim(keys).id("crumb-keys").aria_label(keys).test_support());
    let empty = dock::empty(ui, cx).then(|| {
        let empty = div().flex_1().min_h_0().flex().flex_col().p(t.css(24.));
        empty.child(dim("No agents in this space."))
    });
    let area = ui
        .dock
        .as_ref()
        .filter(|_| empty.is_none())
        .map(|d| d.area.clone());
    div()
        .id("space")
        .key_context("Space")
        .track_focus(&ui.zoom_focus)
        .on_action(on(cx, |store, ui, key: &Zoomed| act(store, ui, *key)))
        .on_action(on(cx, |_, ui, t: &Tab| tab(ui, &t.0)))
        .on_action(on(cx, |store, ui, c: &dock::Close| {
            dock::close_tab(store, ui, c.0.as_deref())
        }))
        .on_action(on(cx, |store, ui, p: &dock::Pin| {
            dock::pin_tab(store, ui, &p.0)
        }))
        .on_action(on(cx, |_, ui, _: &dock::Maximize| {
            ui.asks.push(dock::Ask::Maximize);
            Vec::new()
        }))
        .on_action(on(cx, |_, ui, n: &dock::Nth| {
            ui.asks.push(dock::Ask::Nth(n.0.saturating_sub(1)));
            Vec::new()
        }))
        .on_action(on(cx, |store, ui, nav: &Nav| match nav {
            Nav::NextNeeding(_) => lens::next_needing(store, ui, true),
            _ => Vec::new(),
        }))
        .size_full()
        .bg(rgb(pal::GROUND))
        .flex()
        .flex_col()
        .child(bar)
        .children(area.map(|a| div().flex_1().min_h_0().flex().child(a)))
        .children(empty)
        .into_any_element()
}
