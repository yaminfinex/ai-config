//! The zoom shell (`Space` context): one space's agents as tabs over the zoomed agent. In U2 the body is
//! a placeholder (name, status, cwd); U3 puts the transcript there. Zooming into an agent marks it seen.
//! Entering, `[` `]` and `n` slide the body in from the side they came from; the slide is one-shot, so
//! nothing redraws once it lands.

use crate::store::fleet::{Agent, Status};
use crate::store::spaces::{Move, Space};
use crate::store::{Event, Store};
use crate::views::lens::{self, Nav, Ui};
use crate::views::theme::{TypeScale, pal};
use crate::views::{Host, on};
use gpui_kit::prelude::FluentBuilder as _;
use gpui_kit::*;
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

pub const SPACE: &str = "Space && !Input && !Terminal";
const SLIDE: Duration = Duration::from_millis(220);

pub(super) fn bindings() -> [KeyBinding; 5] {
    let keys = [
        ("escape", Zoomed::Out),
        ("[", Zoomed::Space(-1)),
        ("]", Zoomed::Space(1)),
        ("tab", Zoomed::Agent(1)),
        ("shift-tab", Zoomed::Agent(-1)),
    ];
    keys.map(|(k, a)| KeyBinding::new(k, a, Some(SPACE)))
}

/// Which space and agent are zoomed, and how the body slid in (`-1` from the left, `1` from the
/// right, `0` in place) under a number that changes on every move, so each move replays the slide.
pub struct Zoom {
    pub space: String,
    pub agent: Option<String>,
    slide: (usize, f32),
}

/// Zoom into `space`: its first agent needing you, else its visible one. Marks that agent seen.
pub fn zoom_into(store: &Store, ui: &mut Ui, space: &Space, from: f32) -> Vec<Event> {
    let agent = space
        .agents()
        .find(|a| store.agent_needs_you(a))
        .or_else(|| store.visible(space));
    ui.select(&space.id);
    show(ui, space.id.clone(), agent.map(str::to_string), from)
}

fn show(ui: &mut Ui, space: String, agent: Option<String>, from: f32) -> Vec<Event> {
    let n = ui.zoom.as_ref().map_or(0, |z| z.slide.0 + 1);
    let seen = agent.clone().map(|a| Event::Lens(Move::Seen(a)));
    ui.zoom = Some(Zoom {
        space,
        agent,
        slide: (n, from),
    });
    seen.into_iter().collect()
}

pub(super) fn zoomed<'a>(store: &'a Store, zoom: &Zoom) -> Option<&'a Space> {
    store.spaces.iter().find(|s| s.id == zoom.space)
}

fn act(store: &Store, ui: &mut Ui, key: Zoomed) -> Vec<Event> {
    let Some(zoom) = ui.zoom.as_ref() else {
        return Vec::new();
    };
    let (Some(space), Zoomed::Space(by) | Zoomed::Agent(by)) = (zoomed(store, zoom), key) else {
        // `escape`, or the space has gone: back to the lens.
        ui.zoom = None;
        return Vec::new();
    };
    let wrap = |at: usize, len: usize| (at as isize + by).rem_euclid(len.max(1) as isize) as usize;
    if let Zoomed::Space(by) = key {
        let order = store.lens();
        let at = order.iter().position(|o| o.id == space.id).unwrap_or(0);
        return zoom_into(store, ui, order[wrap(at, order.len())], by as f32);
    }
    let agents: Vec<&str> = space.agents().collect();
    let at = agents
        .iter()
        .position(|a| Some(*a) == zoom.agent.as_deref());
    match agents.get(wrap(at.unwrap_or(0), agents.len())) {
        Some(next) => show(ui, space.id.clone(), Some(next.to_string()), 0.0),
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
            .child(agent.map_or_else(|| div().child("·"), |a| glyph(a, ui, t)))
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
    let (n, from) = zoom.slide;
    let width = t.px(240.);
    let body = body(store, current, ui, t).with_animation(
        ElementId::NamedInteger("zoom".into(), n as u64),
        Animation::new(SLIDE).with_easing(ease_out_quint()),
        move |el, p| el.left(width * (from * (1.0 - p))).opacity(0.4 + 0.6 * p),
    );
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
        .flex()
        .flex_col()
        .child(bar)
        .child(
            div()
                .flex()
                .gap(t.px(4.))
                .px(t.px(12.))
                .py(t.px(6.))
                .children(tabs),
        )
        .child(div().flex_1().relative().overflow_hidden().child(body))
        .into_any_element()
}

/// The U2 placeholder for an agent: name, status and working directory.
fn body(store: &Store, name: Option<&str>, ui: &Ui, t: TypeScale) -> Div {
    let el = div()
        .absolute()
        .size_full()
        .p(t.px(24.))
        .flex()
        .flex_col()
        .gap(t.px(8.));
    let Some(name) = name else {
        return el.child(dim("No agents in this space."));
    };
    let Some(agent) = store.fleet.agents.get(name) else {
        return el
            .child(div().text_size(t.title).child(name.to_string()))
            .child("not on the board");
    };
    let meta = |k: &str, v: String| {
        div()
            .flex()
            .gap(t.px(10.))
            .child(dim(k.to_string()).w(t.px(70.)))
            .child(v)
    };
    el.child(
        div()
            .flex()
            .items_center()
            .gap(t.px(8.))
            .text_size(t.title)
            .child(glyph(agent, ui, t))
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

/// Agent chrome shared by the tabs here and the lens cards.
pub(super) fn working(agent: &Agent) -> bool {
    agent.status() == Status::Working
}

/// The status glyph in its navigation-light colour; a working one pulses.
pub(super) fn glyph(agent: &Agent, ui: &Ui, t: TypeScale) -> Div {
    let (g, color, opacity) = match agent.status() {
        Status::Working => ("●", pal::GREEN, ui.pulse()),
        Status::Blocked => ("■", pal::PORT, 1.0),
        Status::Done => ("✓", pal::SLATE, 1.0),
        Status::Idle => ("○", pal::SLATE, 1.0),
    };
    div()
        .flex_none()
        .w(t.px(12.))
        .text_color(rgb(color))
        .opacity(opacity)
        .child(g)
}

pub(super) fn label(agent: &Agent, needs_you: bool) -> &'static str {
    match agent.status() {
        _ if needs_you => "your turn",
        Status::Working => "working",
        Status::Blocked => "blocked",
        Status::Done => "done",
        Status::Idle => "idle",
    }
}

pub(super) fn pill(n: usize, t: TypeScale) -> Div {
    div()
        .flex_none()
        .px(t.px(7.))
        .rounded_full()
        .bg(rgb(pal::ACC))
        .text_color(rgb(pal::GROUND))
        .text_size(t.small)
        .font_weight(FontWeight::BOLD)
        .child(n.to_string())
}

/// Secondary text.
pub(super) fn dim(text: impl Into<SharedString>) -> Div {
    div().text_color(rgb(pal::SLATE)).child(text.into())
}
