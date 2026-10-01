//! The zoom shell (`Space` context): one space's agents as tabs over the zoomed agent's transcript
//! (`transcript`), plus a preview tab for an outsider opened from a mention (local only, never a
//! member). Zooming into an agent marks it seen and clears the space's unread mark. A notified agent in
//! no space opens alone, as a preview in a zoom of no space (`Zoom::alone`); nothing joins a space.
//!
//! Transitions, as the spike: `enter` morphs the card's bounds to the window (280 ms) and `escape`
//! morphs back (200 ms); `[` `]` and `n` swipe sideways (240 ms). Each is one-shot, and the lens is
//! drawn underneath only while a morph runs.

use crate::store::spaces::{Move, Space};
use crate::store::transcript;
use crate::store::{Event, Store};
use crate::views::composer::{self, Compose};
use crate::views::lens::{self, Nav, State, Ui};
use crate::views::markdown::{AGENT, PATH};
use crate::views::notes::{self, Notes};
use crate::views::notes_list::{self, Card};
use crate::views::theme::{TypeScale, pal};
use crate::views::transcript::{self as body, OpenLink, Scroll, ToggleRun};
use crate::views::{Host, dim, glyph, on, pill};
use gpui_kit::prelude::FluentBuilder as _;
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
    /// `tab` / `shift-tab`: the next or previous agent in the space, wrapping.
    Agent(isize),
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
    show(ui, id.to_string(), agent)
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
            return vec![Event::Lens(Move::View { space, agent })];
        }
        return zoom_to(ui, &to.space, to.agent, None);
    }
    let out = act(store, ui, Zoomed::Out);
    if let Some(id) = tag.strip_prefix("space:") {
        ui.select(id);
    }
    out
}

pub(super) fn show(ui: &mut State, space: String, agent: Option<String>) -> Vec<Event> {
    ui.zoom = Some(Zoom {
        space: space.clone(),
        agent: agent.clone(),
    });
    vec![Event::Lens(Move::View { space, agent })]
}

/// A clicked link: a path resolves and opens in VS Code; a member becomes its tab and any other agent
/// a preview tab in this zoom, never added to the space.
fn open(ui: &mut State, url: &str) -> Vec<Event> {
    if let Some(path) = url.strip_prefix(PATH) {
        return vec![Event::Transcript(transcript::Step::OpenPath(path.into()))];
    }
    let (Some(agent), Some(zoom)) = (url.strip_prefix(AGENT), ui.zoom.clone()) else {
        return Vec::new();
    };
    // Linked names are board names; one that has left the board since (retired) opens read-only.
    if agent.is_empty() || zoom.agent.as_deref() == Some(agent) {
        return Vec::new();
    }
    show(ui, zoom.space, Some(agent.to_string()))
}

/// A clicked tab: its agent, unless it is already shown.
fn tab(ui: &mut State, agent: &str) -> Vec<Event> {
    match ui.zoom.clone() {
        Some(zoom) if zoom.agent.as_deref() != Some(agent) => {
            show(ui, zoom.space, Some(agent.to_string()))
        }
        _ => Vec::new(),
    }
}

pub(super) fn zoomed<'a>(store: &'a Store, zoom: &Zoom) -> Option<&'a Space> {
    store.spaces.iter().find(|s| s.id == zoom.space)
}

pub fn act(store: &Store, ui: &mut State, key: Zoomed) -> Vec<Event> {
    let Some(zoom) = ui.zoom.clone() else {
        return Vec::new();
    };
    // Alone, there is no space to move through: only `escape` does anything.
    if zoom.alone() && key != Zoomed::Out {
        return Vec::new();
    }
    let (Some(space), Zoomed::Space(by) | Zoomed::Agent(by)) = (zoomed(store, &zoom), key) else {
        // `escape`, or the space has gone: morph back to its card, letting the transcript go.
        let card = ui.cards.borrow().get(&zoom.space).copied();
        ui.anim = Some(Anim::new(Kind::Out, card, ui.zoom.take()));
        ui.reveal.set(true);
        ui.transcript.clear();
        return vec![Event::Transcript(transcript::Step::Hide)];
    };
    let wrap = |at: usize, len: usize| (at as isize + by).rem_euclid(len.max(1) as isize) as usize;
    if let Zoomed::Space(by) = key {
        let order = store.lens();
        let at = order.iter().position(|o| o.id == space.id).unwrap_or(0);
        return zoom_into(store, ui, order[wrap(at, order.len())], Some(by as f32));
    }
    // The zoomed agent may have left the space meanwhile: then `tab` goes to the first one.
    let agents: Vec<&str> = space.agents().collect();
    let at = agents
        .iter()
        .position(|a| Some(*a) == zoom.agent.as_deref());
    match agents.get(at.map_or(0, |at| wrap(at, agents.len()))) {
        Some(next) => show(ui, space.id.clone(), Some(next.to_string())),
        None => Vec::new(),
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
    let members: Vec<&str> = space.into_iter().flat_map(Space::agents).collect();
    let preview = current.filter(|c| !members.contains(c));
    let tabs = members.into_iter().chain(preview).map(|name| {
        let on = Some(name) == current;
        let agent = store.fleet.agents.get(name);
        let click = Tab(name.to_string().into());
        div()
            .id(SharedString::from(format!("tab-{name}")))
            .cursor_pointer()
            .on_click(move |_, window, cx| window.dispatch_action(click.boxed_clone(), cx))
            .hover(|s| s.text_color(rgb(pal::INK)))
            .flex()
            .items_center()
            .gap(t.px(6.))
            .px(t.px(10.))
            .py(t.px(3.))
            .rounded(t.px(4.))
            .text_color(rgb(if on { pal::INK } else { pal::SLATE }))
            .when(on, |el| el.bg(rgb(pal::WASH)))
            .child(agent.map_or_else(|| div().child("·"), |a| glyph(a, &ui.dots, t)))
            .child(name.to_string())
            .when(Some(name) == preview, |el| {
                el.child(dim("preview").text_size(t.small))
            })
            .when(store.agent_needs_you(name), |el| el.child(pill(1, t)))
    });
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
    let bar = div()
        .flex()
        .items_center()
        .gap(t.px(16.))
        .h(t.px(44.))
        .px(t.px(16.))
        .border_b_1()
        .border_color(rgb(pal::RULE))
        .child(div().flex_1().font_weight(FontWeight::BOLD).child(crumb))
        .child(
            dim(match zoom.alone() {
                true => "esc lens",
                false => "esc lens · [ ] spaces · tab agents",
            })
            .text_size(t.small),
        );
    let strip = div().flex().gap(t.px(4.)).px(t.px(12.)).py(t.px(6.));
    div()
        .id("space")
        .key_context("Space")
        .track_focus(&ui.zoom_focus)
        .on_action(on(cx, |store, ui, key: &Zoomed| act(store, ui, *key)))
        .on_action(on(cx, |_, ui, t: &Tab| tab(ui, &t.0)))
        .on_action(on(cx, |store, ui, s: &Scroll| body::scroll(store, ui, *s)))
        .on_action(on(cx, |_, ui, _: &ToggleRun| body::toggle_lowest(ui)))
        .on_action(on(cx, |_, ui, l: &OpenLink| open(ui, &l.0)))
        .on_action(on(cx, |_, ui, c: &Compose| match c {
            Compose::Focus => composer::act(ui, *c),
            _ => Vec::new(),
        }))
        .on_action(on(cx, |store, ui, n: &Notes| notes::act(store, ui, n)))
        .on_action(on(cx, |store, ui, c: &Card| notes_list::act(store, ui, c)))
        .on_action(on(cx, |store, ui, nav: &Nav| match nav {
            Nav::NextNeeding(_) => lens::next_needing(store, ui, true),
            _ => Vec::new(),
        }))
        .size_full()
        .bg(rgb(pal::GROUND))
        .flex()
        .flex_col()
        .child(bar)
        .child(strip.children(tabs))
        .child(body::render(store, ui, zoom, t, cx))
        .children(current.and_then(|agent| notes::render(store, ui, agent, t, cx)))
        .children(current.map(|agent| composer::render(store, ui, agent, t, cx)))
        .into_any_element()
}
