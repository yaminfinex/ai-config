//! The zoom shell (`Space` context): one space's agents as tabs over the zoomed agent. In U2 the body is
//! a placeholder (name, status, cwd); U3 puts the transcript there. Zooming into an agent marks it seen
//! and clears the space's unread mark.
//!
//! Transitions, as the spike: `enter` morphs the card's bounds to the window (280 ms) and `escape`
//! morphs back (200 ms); `[` `]` and `n` swipe sideways (240 ms). Each is one-shot, and the lens is
//! drawn underneath only while a morph runs.

use crate::store::spaces::{Move, Space};
use crate::store::{Event, Store};
use crate::views::lens::{self, Nav, State, Ui};
use crate::views::theme::{TypeScale, pal};
use crate::views::{Host, dim, glyph, label, on, pill};
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

/// Which space and agent are zoomed.
#[derive(Clone, Debug, PartialEq)]
pub struct Zoom {
    pub space: String,
    pub agent: Option<String>,
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
    let kind = match (swipe, &ui.zoom) {
        (Some(dir), _) => Kind::Swipe(dir),
        (None, None) => Kind::In,
        (None, Some(_)) => Kind::Swipe(0.0),
    };
    let card = ui.cards.borrow().get(&space.id).copied();
    ui.anim = Some(Anim::new(kind, card, None));
    ui.select(&space.id);
    show(ui, space.id.clone(), agent.map(str::to_string))
}

fn show(ui: &mut State, space: String, agent: Option<String>) -> Vec<Event> {
    let (s, a) = (space.clone(), agent.clone());
    ui.zoom = Some(Zoom { space, agent });
    vec![Event::Lens(Move::View { space: s, agent: a })]
}

pub(super) fn zoomed<'a>(store: &'a Store, zoom: &Zoom) -> Option<&'a Space> {
    store.spaces.iter().find(|s| s.id == zoom.space)
}

pub fn act(store: &Store, ui: &mut State, key: Zoomed) -> Vec<Event> {
    let Some(zoom) = ui.zoom.clone() else {
        return Vec::new();
    };
    let (Some(space), Zoomed::Space(by) | Zoomed::Agent(by)) = (zoomed(store, &zoom), key) else {
        // `escape`, or the space has gone: morph back to its card.
        let card = ui.cards.borrow().get(&zoom.space).copied();
        ui.anim = Some(Anim::new(Kind::Out, card, ui.zoom.take()));
        ui.reveal.set(true);
        return Vec::new();
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
    let tabs = space.into_iter().flat_map(Space::agents).map(|name| {
        let on = Some(name) == current;
        let agent = store.fleet.agents.get(name);
        div()
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
            .when(store.agent_needs_you(name), |el| el.child(pill(1, t)))
    });
    let crumb = format!(
        "lens › {} › {}",
        space.map_or("(space gone)", |s| s.name.as_str()),
        current.unwrap_or("no agents")
    );
    let bar = div()
        .flex()
        .items_center()
        .gap(t.px(16.))
        .h(t.px(44.))
        .px(t.px(16.))
        .border_b_1()
        .border_color(rgb(pal::RULE))
        .child(div().flex_1().font_weight(FontWeight::BOLD).child(crumb))
        .child(dim("esc lens · [ ] spaces · tab agents").text_size(t.small));
    let strip = div().flex().gap(t.px(4.)).px(t.px(12.)).py(t.px(6.));
    div()
        .id("space")
        .key_context("Space")
        .track_focus(&ui.zoom_focus)
        .on_action(on(cx, |store, ui, key: &Zoomed| act(store, ui, *key)))
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
        .child(body(store, current, ui, t))
        .into_any_element()
}

/// The U2 placeholder for an agent: name, status and working directory.
fn body(store: &Store, name: Option<&str>, ui: &Ui, t: TypeScale) -> Div {
    let el = div().p(t.px(24.)).flex().flex_col().gap(t.px(8.));
    let Some(name) = name else {
        return el.child(dim("No agents in this space."));
    };
    let Some(agent) = store.fleet.agents.get(name) else {
        let name = div().text_size(t.title).child(name.to_string());
        return el.child(name).child("not on the board");
    };
    let meta = |k: &str, v: String| {
        let key = dim(k.to_string()).w(t.px(70.));
        div().flex().gap(t.px(10.)).child(key).child(v)
    };
    let head = div().flex().items_center().gap(t.px(8.)).text_size(t.title);
    el.child(
        head.child(glyph(agent, &ui.dots, t))
            .child(name.to_string()),
    )
    .child(meta(
        "status",
        label(agent, store.agent_needs_you(name)).into(),
    ))
    .child(meta("cwd", agent.cwd.clone().unwrap_or_else(|| "—".into())))
    .child(meta("tool", agent.tool.clone()))
    .children(agent.title.clone().map(|title| meta("title", title)))
    .child(dim("The transcript arrives in U3.").pt(t.px(16.)))
}
