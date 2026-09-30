//! The lens, the app's home: every space as a card in one of three owner-chosen rows (focus, watch,
//! background). A bright card with an unread count needs you; a dim one does not; a working agent's
//! dot pulses. The keys (ARCHITECTURE §4) move the selection, place spaces and zoom in (`space`).
//!
//! `Ui` (selection, zoom, help, card text and size, pulse phase) is view state, not domain state: it
//! is not persisted and never goes through the store. Rows, the visible agent and seen marks are the
//! store's, changed by dispatching `Move`s.

use crate::store::spaces::{Move, Row, Space};
use crate::store::{Conn, Event, Store};
use crate::views::space::{self, dim, glyph, label, pill, working};
use crate::views::theme::{TypeScale, pal};
use crate::views::{Host, on};
use gpui_kit::prelude::FluentBuilder as _;
use gpui_kit::*;
use std::time::Duration;

/// Every lens key is one action, so one handler (`act`) reads as the whole keymap.
#[derive(Clone, Copy, Debug, PartialEq, Action)]
#[action(namespace = lens, no_json)]
pub enum Nav {
    Step(isize),
    StepRow(isize),
    Place(Row),
    CycleVisible,
    Seen,
    Unseen,
    CardText,
    CardSize,
    Help,
    ZoomIn,
    /// `n`, or `N` with `true`: the next space needing you, and zoom in.
    NextNeeding(bool),
}

pub const HOME: &str = "Lens && !Input && !Terminal";
/// Card sizes (`s`): width in design pixels and text lines, as the prototype.
const SIZES: [(f32, usize); 4] = [(240., 3), (284., 5), (360., 8), (440., 12)];
/// The working dot's pulse: 8 steps over 1.6 s, redrawn only while a working agent is on screen.
const PULSE_STEP: Duration = Duration::from_millis(200);
const PULSE_STEPS: u32 = 8;

pub fn bind(cx: &mut App) {
    cx.bind_keys(bindings());
}

/// Every lens and zoom binding, under `HOME` or `space::SPACE`; a test checks none fires in an Input.
pub fn bindings() -> Vec<KeyBinding> {
    let keys = [
        ("left", Nav::Step(-1)),
        ("h", Nav::Step(-1)),
        ("right", Nav::Step(1)),
        ("l", Nav::Step(1)),
        ("j", Nav::StepRow(1)),
        ("k", Nav::StepRow(-1)),
        ("1", Nav::Place(Row::Focus)),
        ("2", Nav::Place(Row::Watch)),
        ("3", Nav::Place(Row::Background)),
        ("v", Nav::CycleVisible),
        ("m", Nav::Seen),
        ("u", Nav::Unseen),
        ("t", Nav::CardText),
        ("s", Nav::CardSize),
        ("?", Nav::Help),
        ("enter", Nav::ZoomIn),
    ];
    let next = [
        ("n", Nav::NextNeeding(false)),
        ("shift-n", Nav::NextNeeding(true)),
    ];
    let home = keys
        .into_iter()
        .chain(next)
        .map(|(k, a)| KeyBinding::new(k, a, Some(HOME)));
    let zoomed = next.map(|(k, a)| KeyBinding::new(k, a, Some(space::SPACE)));
    home.chain(zoomed).chain(space::bindings()).collect()
}

pub struct Ui {
    home: FocusHandle,
    pub(super) zoom_focus: FocusHandle,
    /// The selected space's id; `None` is the first card.
    selected: Option<String>,
    pub(super) zoom: Option<space::Zoom>,
    help: bool,
    /// Card text: the agent's latest `<status>` line instead of its status (`t`).
    status_line: bool,
    size: usize,
    pulse: u32,
}

impl Ui {
    pub fn new(cx: &mut App) -> Self {
        Ui {
            home: cx.focus_handle(),
            zoom_focus: cx.focus_handle(),
            selected: None,
            zoom: None,
            help: false,
            status_line: false,
            size: 1,
            pulse: 0,
        }
    }

    /// The element that must hold focus: the zoom shell while zoomed, else the lens.
    pub fn focus_target(&self) -> &FocusHandle {
        if self.zoom.is_some() {
            &self.zoom_focus
        } else {
            &self.home
        }
    }

    /// Opacity of a working dot now: 1 → 0.3 → 1 over one cycle.
    pub(super) fn pulse(&self) -> f32 {
        let p = (self.pulse % PULSE_STEPS) as f32 / PULSE_STEPS as f32;
        0.3 + 0.7 * (2.0 * p - 1.0).abs()
    }

    pub(super) fn select(&mut self, id: &str) {
        self.selected = Some(id.to_string());
    }
}

/// Step the pulse while a working agent is on screen; with none, the timer wakes and draws nothing.
pub fn pulse<H: Host>(cx: &mut Context<H>) {
    cx.spawn(async move |this, cx| {
        loop {
            cx.background_executor().timer(PULSE_STEP).await;
            let alive = this.update(cx, |host, cx| {
                let (store, ui) = host.parts();
                let busy = |name: &str| store.fleet.agents.get(name).is_some_and(working);
                let on_screen = match &ui.zoom {
                    Some(z) => space::zoomed(store, z).is_some_and(|s| s.agents().any(busy)),
                    None => store
                        .spaces
                        .iter()
                        .filter_map(|s| store.visible(s))
                        .any(busy),
                };
                if on_screen {
                    ui.pulse = ui.pulse.wrapping_add(1);
                    cx.notify();
                }
            });
            if alive.is_err() {
                break;
            }
        }
    })
    .detach();
}

/// The selected space: the owner's selection while it exists, else the first card.
fn selected<'a>(store: &'a Store, ui: &Ui) -> Option<&'a Space> {
    let order = store.lens();
    let id = ui.selected.as_deref();
    order
        .iter()
        .find(|s| Some(s.id.as_str()) == id)
        .or(order.first())
        .copied()
}

/// What a lens key does: move the view state, and return the moves for the store.
fn act(store: &Store, ui: &mut Ui, nav: Nav) -> Vec<Event> {
    let Some(space) = selected(store, ui) else {
        return Vec::new();
    };
    let order = store.lens();
    let at = order.iter().position(|s| s.id == space.id).unwrap_or(0);
    let moves = match nav {
        Nav::Step(by) => {
            let next = order.get(at.saturating_add_signed(by)).unwrap_or(&space);
            ui.select(&next.id);
            Vec::new()
        }
        Nav::StepRow(by) => {
            step_row(store, ui, space, by);
            Vec::new()
        }
        Nav::Place(row) => vec![Move::SetRow {
            space: space.id.clone(),
            row,
        }],
        Nav::CycleVisible => vec![Move::CycleVisible(space.id.clone())],
        Nav::Seen => space.agents().map(|a| Move::Seen(a.into())).collect(),
        Nav::Unseen => store
            .visible(space)
            .map(|a| Move::Unseen(a.into()))
            .into_iter()
            .collect(),
        Nav::CardText => toggle(&mut ui.status_line),
        Nav::CardSize => {
            ui.size = (ui.size + 1) % SIZES.len();
            Vec::new()
        }
        Nav::Help => toggle(&mut ui.help),
        Nav::ZoomIn => return space::zoom_into(store, ui, space, 0.0),
        Nav::NextNeeding(zoom) => return next_needing(store, ui, zoom),
    };
    moves.into_iter().map(Event::Lens).collect()
}

fn toggle(flag: &mut bool) -> Vec<Move> {
    *flag = !*flag;
    Vec::new()
}

/// `j k`: the same column in the next or previous row that has cards, else that row's last card.
fn step_row(store: &Store, ui: &mut Ui, from: &Space, by: isize) {
    let rows = store.rows();
    let here = store.row(from) as usize;
    let col = rows[here].iter().position(|s| s.id == from.id).unwrap_or(0);
    let mut r = here as isize + by;
    while let Some(row) = usize::try_from(r).ok().and_then(|r| rows.get(r)) {
        if let Some(s) = row.get(col).or(row.last()) {
            return ui.select(&s.id);
        }
        r += by;
    }
}

/// `n` / `N`: select the next space needing you after the current one; zoom into it for `N` or when
/// already zoomed.
pub(super) fn next_needing(store: &Store, ui: &mut Ui, zoom: bool) -> Vec<Event> {
    let from = match &ui.zoom {
        Some(z) => Some(z.space.clone()),
        None => selected(store, ui).map(|s| s.id.clone()),
    };
    let Some(next) = store.next_needing(from.as_deref()) else {
        return Vec::new();
    };
    ui.select(&next.id);
    if zoom || ui.zoom.is_some() {
        space::zoom_into(store, ui, next, 1.0)
    } else {
        Vec::new()
    }
}

pub fn render<H: Host>(store: &Store, ui: &Ui, t: TypeScale, cx: &mut Context<H>) -> AnyElement {
    if let Some(zoom) = &ui.zoom {
        return space::render(store, ui, zoom, t, cx);
    }
    let chosen = selected(store, ui).map(|s| s.id.as_str());
    let names = ["FOCUS full cards", "WATCH two lines", "BACKGROUND one line"];
    let rows = store
        .rows()
        .into_iter()
        .zip(Row::ALL)
        .enumerate()
        .map(|(i, (spaces, row))| {
            let head = format!("{} {} · {}", i + 1, names[i], spaces.len());
            let head = dim(head).text_size(t.small);
            let cards = spaces
                .into_iter()
                .map(|s| card(store, ui, s, row, chosen == Some(&s.id), t));
            let cards = div()
                .flex()
                .flex_wrap()
                .items_start()
                .gap(t.px(10.))
                .children(cards);
            div()
                .flex()
                .flex_col()
                .gap(t.px(8.))
                .child(head)
                .child(cards)
        });
    div()
        .id("lens")
        .track_focus(&ui.home)
        .on_action(on(cx, |store, ui, nav: &Nav| act(store, ui, *nav)))
        .size_full()
        .relative()
        .child(
            div()
                .id("rows")
                .size_full()
                .overflow_y_scroll()
                .p(t.px(20.))
                .flex()
                .flex_col()
                .gap(t.px(22.))
                .child(header(store, t))
                .children(rows),
        )
        .when(ui.help, |el| el.child(help(t)))
        .into_any_element()
}

fn header(store: &Store, t: TypeScale) -> Div {
    let conn = match &store.conn {
        Conn::Offline => "offline",
        Conn::Live { .. } => "live",
    };
    let needs: usize = store.spaces.iter().map(|s| store.needs_you(s)).sum();
    let updated = if store.server_updated {
        " · server updated"
    } else {
        ""
    };
    let spaces = store.spaces.len();
    let line = format!("herder · {conn} · {spaces} spaces · {needs} need you{updated} · ? keys");
    dim(line).text_size(t.small)
}

/// One space: bright with its unread count when anyone in it needs you, dim otherwise. The card
/// shows the visible agent and `+N` for the other members, then its text: focus cards at the chosen
/// size (`s`), watch cards two lines, background cards none.
fn card(store: &Store, ui: &Ui, space: &Space, row: Row, on: bool, t: TypeScale) -> Div {
    let needs = store.needs_you(space);
    let name = store.visible(space);
    let agent = name.and_then(|n| store.fleet.agents.get(n));
    let others = space.members.len() - usize::from(name.is_some());
    let (width, lines) = match (row, SIZES[ui.size]) {
        (Row::Focus, size) => size,
        (Row::Watch, (width, _)) => (width, 2),
        (Row::Background, _) => (SIZES[0].0, 0),
    };
    let text = match agent {
        None => "no agent on the board".to_string(),
        Some(a) if ui.status_line => match store.status_lines.get(&a.name) {
            Some((_, Some(line))) => line.clone(),
            _ => "no <status> line yet".into(),
        },
        Some(a) => [
            label(a, store.agent_needs_you(&a.name)),
            a.title.as_deref().unwrap_or(""),
        ]
        .join(" · ")
        .trim_end_matches(" · ")
        .to_string(),
    };
    let (bg, border) = match (on, needs > 0) {
        (true, n) => (if n { pal::ACCW } else { pal::PANEL }, pal::AMBER),
        (false, true) => (pal::ACCW, pal::ACC),
        (false, false) => (pal::PANEL, pal::RULE),
    };
    let title = div()
        .flex()
        .items_center()
        .gap(t.px(8.))
        .text_size(t.title)
        .child(
            div()
                .flex_1()
                .truncate()
                .font_weight(FontWeight::BOLD)
                .child(space.name.clone()),
        )
        .when(needs > 0, |el| el.child(pill(needs, t)));
    let who = div()
        .flex()
        .items_center()
        .gap(t.px(6.))
        .children(agent.map(|a| glyph(a, ui, t)))
        .child(div().truncate().child(name.unwrap_or("—").to_string()))
        .when(others > 0, |el| el.child(dim(format!("+{others}"))));
    div()
        .w(t.px(width))
        .px(t.px(14.))
        .py(t.px(if lines == 0 { 8. } else { 12. }))
        .flex()
        .flex_col()
        .gap(t.px(8.))
        .opacity(match (needs > 0 || on, agent.is_some_and(working)) {
            (true, _) => 1.0,
            (false, true) => 0.75,
            (false, false) => 0.5,
        })
        .bg(rgb(bg))
        .border_2()
        .border_color(rgb(border))
        .rounded(t.px(8.))
        .child(title)
        .child(who)
        .when(lines > 0, |el| el.child(dim(text).line_clamp(lines)))
}

/// The key help, in two monospace columns.
const HELP: &str = "\
lens
← → h l j k   move
enter         zoom in
n / N         next needing you / and zoom in
1 2 3         focus / watch / background
v             next visible agent
m / u         seen / unseen
t             status ↔ <status> line
s             card size
?             this help

zoomed in
esc           back to the lens
[ ]           previous / next space
tab ⇧tab      next / previous agent
n / N         next space needing you";

fn help(t: TypeScale) -> Div {
    let lines = HELP.lines().map(|l| div().min_h(t.line).child(l));
    let panel = div()
        .p(t.px(24.))
        .bg(rgb(pal::PANEL))
        .border_1()
        .border_color(rgb(pal::RULE))
        .rounded(t.px(10.))
        .children(lines);
    div()
        .absolute()
        .inset_0()
        .bg(rgba(0x060A1099))
        .flex()
        .items_center()
        .justify_center()
        .child(panel)
}
