//! The lens, the app's home: every space as a card in one of three owner-chosen rows (focus, watch,
//! background). A bright card with an unread count needs you; a dim one does not; a working agent's
//! dot pulses. The keys (ARCHITECTURE §4) move the selection, place spaces and zoom in (`space`).
//!
//! `Ui` (selection, zoom, help, card text and size) is view state, not domain state: it is not
//! persisted and never goes through the store. Rows, the visible agent, seen marks and unread spaces
//! are the store's, changed by dispatching `Move`s.

use crate::store::fleet::Agent;
use crate::store::spaces::{Move, Row, Space, Stop};
use crate::store::{Conn, Event, Store};
use crate::views::space::{self, Anim, Zoom};
use crate::views::theme::{TypeScale, pal};
use crate::views::{Dots, Host, dim, glyph, help, label, on, pill, working};
use gpui_kit::prelude::FluentBuilder as _;
use gpui_kit::*;
use std::cell::{Cell, RefCell};
use std::collections::HashMap;
use std::ops::{Deref, DerefMut};
use std::rc::Rc;

/// Every lens key is one action, so one handler (`act`) reads as the whole keymap.
#[derive(Clone, Copy, Debug, PartialEq, Action)]
#[action(namespace = lens, no_json)]
pub enum Nav {
    Step(isize),
    StepRow(isize),
    Place(Row),
    CycleVisible,
    Read,
    Unread,
    CardText,
    CardSize,
    Help,
    ZoomIn,
    /// `n`, or `N` with `true`: the next space needing you, and zoom in.
    NextNeeding(bool),
}

/// A click on a card: select its space (`space`, an id); a double-click (`zoom`) zooms in too.
#[derive(Clone, Debug, PartialEq, Action)]
#[action(namespace = lens, no_json)]
pub struct Pick {
    pub space: SharedString,
    pub zoom: bool,
}

/// What a card's text shows (`t` cycles it): the visible agent's last answer, its status and title,
/// or its working directory.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
enum Text {
    #[default]
    Answer,
    Status,
    Cwd,
}

/// Card sizes (`s`): width in design pixels and text lines, as the prototype; the first is the default.
const SIZES: [(f32, usize); 4] = [(284., 5), (360., 8), (440., 12), (240., 3)];

/// Where an action asks focus to go: into the composer's box (where it is writable), into the notes
/// editor or list, or out of them to the zoom (or the lens).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Focus {
    Box,
    Editor,
    List,
    Out,
}

/// The lens's focus handles around its view state.
pub struct Ui {
    home: FocusHandle,
    pub(super) zoom_focus: FocusHandle,
    pub(super) composer: crate::views::composer::View,
    pub(super) notes: crate::views::notes::View,
    state: State,
}

/// The lens's view state, apart from focus so the key logic is testable without a window.
#[derive(Default)]
pub struct State {
    /// The selected space's id; `None` is the first card.
    selected: Option<String>,
    pub(super) zoom: Option<Zoom>,
    /// The last action's focus request, taken by `views::on` as it returns; or a landed file-back's,
    /// taken at the next render (`composer::sync`).
    pub(super) focus: Option<Focus>,
    /// A zoom transition in flight (`space::Anim`), cleared when it lands.
    pub(super) anim: Option<Anim>,
    help: bool,
    /// Card text (`t`).
    text: Text,
    size: usize,
    scroll: ScrollHandle,
    /// The selection moved: scroll its card into view once it is laid out.
    pub(super) reveal: Rc<Cell<bool>>,
    /// The window size and text size last laid out; a change reflows the rows, so reveal again.
    layout: Cell<(Size<Pixels>, Pixels)>,
    /// Each card's last laid-out bounds, where the zoom morphs from and back to.
    pub(super) cards: Rc<RefCell<HashMap<String, Bounds<Pixels>>>>,
    pub dots: Dots,
    pub(super) transcript: crate::views::transcript::View,
}

impl Deref for Ui {
    type Target = State;
    fn deref(&self) -> &State {
        &self.state
    }
}

impl DerefMut for Ui {
    fn deref_mut(&mut self) -> &mut State {
        &mut self.state
    }
}

impl Ui {
    pub fn new<H: Host>(window: &mut Window, cx: &mut Context<H>) -> Self {
        let (home, zoom_focus) = (cx.focus_handle(), cx.focus_handle());
        Ui {
            home,
            zoom_focus,
            composer: crate::views::composer::View::new(window, cx),
            notes: crate::views::notes::View::new(window, cx),
            state: State::default(),
        }
    }

    /// The element that must hold focus: the zoom shell while zoomed, else the lens.
    pub fn focus_target(&self) -> &FocusHandle {
        match self.zoom {
            Some(_) => &self.zoom_focus,
            None => &self.home,
        }
    }
}

impl State {
    pub(super) fn select(&mut self, id: &str) {
        self.selected = Some(id.to_string());
        self.reveal.set(true);
    }

    /// The transition numbered `seq` has landed.
    pub(super) fn settle(&mut self, seq: u64) {
        self.anim.take_if(|a| a.seq() == seq);
    }

    /// The agent zoomed in on, if any.
    pub fn zoomed_agent(&self) -> Option<&str> {
        self.zoom.as_ref()?.agent.as_deref()
    }

    pub fn selected<'a>(&self, store: &'a Store) -> Option<&'a Space> {
        let order = store.lens();
        let id = self.selected.as_deref();
        let found = order.iter().find(|s| Some(s.id.as_str()) == id);
        found.or(order.first()).copied()
    }
}

/// What a lens key does: move the view state, and return the move for the store.
pub fn act(store: &Store, ui: &mut State, nav: Nav) -> Vec<Event> {
    let Some(space) = ui.selected(store) else {
        return Vec::new();
    };
    let order = store.lens();
    let at = order.iter().position(|s| s.id == space.id).unwrap_or(0);
    // Every key keeps the selection in view: placing, card size and text all reflow the rows.
    ui.reveal.set(true);
    match nav {
        Nav::Step(by) => ui.select(&order.get(at.saturating_add_signed(by)).unwrap_or(&space).id),
        Nav::StepRow(by) => step_row(store, ui, space, by),
        // Pin the implicit first-card selection, which would otherwise move with the space.
        Nav::Place(_) => ui.select(&space.id),
        Nav::CardText => {
            ui.text = match ui.text {
                Text::Answer => Text::Status,
                Text::Status => Text::Cwd,
                Text::Cwd => Text::Answer,
            }
        }
        Nav::CardSize => ui.size = (ui.size + 1) % SIZES.len(),
        Nav::Help => ui.help ^= true,
        Nav::ZoomIn => return space::zoom_into(store, ui, space, None),
        Nav::NextNeeding(zoom) => return next_needing(store, ui, zoom),
        Nav::CycleVisible | Nav::Read | Nav::Unread => {}
    }
    let id = space.id.clone();
    let mv = match nav {
        Nav::Place(row) => Move::SetRow { space: id, row },
        Nav::CycleVisible => Move::CycleVisible(id),
        Nav::Read => Move::Read(id),
        Nav::Unread => Move::Unread(id),
        _ => return Vec::new(),
    };
    vec![Event::Lens(mv)]
}

/// A clicked card: select it, and zoom in on a double-click.
fn pick(store: &Store, ui: &mut State, p: &Pick) -> Vec<Event> {
    let Some(space) = store.spaces.iter().find(|s| *s.id == *p.space) else {
        return Vec::new();
    };
    ui.select(&space.id);
    match p.zoom {
        true => space::zoom_into(store, ui, space, None),
        false => Vec::new(),
    }
}

/// `j k`: the same column in the next or previous row that has cards, else that row's last card.
fn step_row(store: &Store, ui: &mut State, from: &Space, by: isize) {
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
/// already zoomed (a sideways swipe there). After the spaces, `N` (and `n` zoomed) opens each agent
/// needing you in no space alone, as its notification's click does; with no card for one on the lens,
/// `n` there passes them by.
pub(super) fn next_needing(store: &Store, ui: &mut State, zoom: bool) -> Vec<Event> {
    let from = match ui.zoom.as_ref() {
        Some(z) if z.alone() => z.agent.as_deref().map(Stop::Alone),
        Some(z) => space::zoomed(store, z).map(Stop::Space),
        None => ui.selected(store).map(Stop::Space),
    };
    let next = match store.next_needing(from, zoom || ui.zoom.is_some()) {
        Some(Stop::Space(next)) => next,
        Some(Stop::Alone(agent)) => return space::summon(store, ui, &format!("agent:{agent}")),
        None => return Vec::new(),
    };
    ui.select(&next.id);
    match (zoom, ui.zoom.is_some()) {
        (_, true) => space::zoom_into(store, ui, next, Some(1.0)),
        (true, false) => space::zoom_into(store, ui, next, None),
        (false, false) => Vec::new(),
    }
}

/// The lens, the zoom, or a transition between them. `window` is the full window's size.
pub fn render<H: Host>(
    store: &Store,
    ui: &Ui,
    t: TypeScale,
    window: Size<Pixels>,
    cx: &mut Context<H>,
) -> AnyElement {
    ui.dots.clear(ui.anim.is_some() || ui.help);
    let reflowed = ui.layout.replace((window, t.body)) != (window, t.body);
    ui.reveal.set(ui.reveal.get() || reflowed);
    match (&ui.zoom, &ui.anim) {
        (Some(zoom), None) => space::render(store, ui, zoom, t, cx),
        (Some(zoom), Some(anim)) if anim.leaving().is_none() => {
            let pane = space::render(store, ui, zoom, t, cx);
            let under = anim.morphs().then(|| home(store, ui, t, cx));
            let el = div().size_full().relative().children(under);
            el.child(anim.animate(pane, window)).into_any_element()
        }
        (_, Some(anim)) => {
            let pane = anim.leaving().map(|z| space::render(store, ui, z, t, cx));
            let el = div().size_full().relative().child(home(store, ui, t, cx));
            el.children(pane.map(|p| anim.animate(p, window)))
                .into_any_element()
        }
        (None, None) => home(store, ui, t, cx),
    }
}

fn home<H: Host>(store: &Store, ui: &Ui, t: TypeScale, cx: &mut Context<H>) -> AnyElement {
    let chosen = ui.selected(store).map(|s| s.id.as_str());
    let names = ["FOCUS last answer", "WATCH two lines", "BACKGROUND no text"];
    let rows = store.rows().into_iter().zip(Row::ALL).enumerate();
    let rows = rows.map(|(i, (spaces, row))| {
        let head = dim(format!("{} {} · {}", i + 1, names[i], spaces.len())).text_size(t.small);
        let cards = spaces
            .into_iter()
            .map(|s| card(store, ui, s, row, chosen == Some(&s.id), t));
        let grid = div().flex().flex_wrap().items_start().gap(t.px(10.));
        let row = div().flex().flex_col().gap(t.px(8.)).child(head);
        row.child(grid.children(cards))
    });
    div()
        .id("lens")
        .track_focus(&ui.home)
        .on_action(on(cx, |store, ui, nav: &Nav| act(store, ui, *nav)))
        .on_action(on(cx, |store, ui, p: &Pick| pick(store, ui, p)))
        .size_full()
        .relative()
        .child(
            div()
                .id("rows")
                .track_scroll(&ui.scroll)
                .size_full()
                .overflow_y_scroll()
                .p(t.px(20.))
                .flex()
                .flex_col()
                .gap(t.px(22.))
                .child(dim(header_line(store)).text_size(t.small))
                .children(rows),
        )
        .when(ui.help, |el| el.child(help(t)))
        .into_any_element()
}

/// The lens header: the connection, the spaces, how many need you (the dock badge's count).
pub(super) fn header_line(store: &Store) -> String {
    let conn = match &store.conn {
        Conn::Offline => "offline",
        Conn::Live => "live",
    };
    let needs = store.needs_you_total();
    let fresh = store.server_updated;
    let updated = if fresh { " · server updated" } else { "" };
    let spaces = store.spaces.len();
    format!("herder · {conn} · {spaces} spaces · {needs} need you{updated} · ? keys")
}

/// One space: bright with its unread count when it needs you, dim otherwise. The card shows the
/// visible agent and `+N` for the other members, then its text (the agent's last answer, `cards`):
/// focus cards at the chosen size (`s`), watch cards two lines, background cards none. A click selects
/// it, a double-click zooms in, and the pointer over it lifts its border.
fn card(store: &Store, ui: &Ui, space: &Space, row: Row, on: bool, t: TypeScale) -> Stateful<Div> {
    let needs = store.needs_you(space);
    let name = store.visible(space);
    let agent = name.and_then(|n| store.fleet.agents.get(n));
    let others = space.members.len() - usize::from(name.is_some());
    let (width, lines) = match (row, SIZES[ui.size]) {
        (Row::Focus, size) => size,
        (Row::Watch, (width, _)) => (width, 2),
        (Row::Background, _) => (SIZES[3].0, 0),
    };
    let status = |a: &Agent| match (label(a, store.agent_needs_you(&a.name)), &a.title) {
        (label, Some(title)) => format!("{label} · {title}"),
        (label, None) => label.into(),
    };
    let text = match (agent, ui.text) {
        (None, _) => "no agent on the board".to_string(),
        (Some(a), Text::Cwd) => a.cwd.clone().unwrap_or_else(|| a.workspace.clone()),
        (Some(a), Text::Answer) => match store.cards.text(&a.name) {
            Some(answer) => answer.to_string(),
            None => status(a),
        },
        (Some(a), Text::Status) => status(a),
    };
    let (bg, border) = match (on, needs > 0) {
        (true, n) => (if n { pal::ACCW } else { pal::PANEL }, pal::AMBER),
        (false, true) => (pal::ACCW, pal::ACC),
        (false, false) => (pal::PANEL, pal::RULE),
    };
    let name_el = div().flex_1().truncate().font_weight(FontWeight::BOLD);
    let title = div().flex().items_center().gap(t.px(8.)).text_size(t.title);
    let title = title
        .child(name_el.child(space.name.clone()))
        .when(needs > 0, |el| el.child(pill(needs, t)));
    let who = div()
        .flex()
        .items_center()
        .gap(t.px(6.))
        .children(agent.map(|a| glyph(a, &ui.dots, t)))
        .child(div().truncate().child(name.unwrap_or("—").to_string()))
        .when(others > 0, |el| el.child(dim(format!("+{others}"))));
    let click = Pick {
        space: space.id.clone().into(),
        zoom: false,
    };
    div()
        .id(SharedString::from(format!("card-{}", space.id)))
        .cursor_pointer()
        .on_click(move |e, window, cx| {
            let zoom = e.click_count() >= 2;
            window.dispatch_action(
                Pick {
                    zoom,
                    ..click.clone()
                }
                .boxed_clone(),
                cx,
            )
        })
        .hover(move |s| s.border_color(rgb(if on { pal::AMBER } else { pal::SLATE })))
        .relative()
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
        .child(recorder(ui, &space.id, on))
        .child(title)
        .child(who)
        .when(lines > 0, |el| el.child(body(text, lines, t)))
}

/// A card's text (the prototype's `.body`, G1): wrapped at words, at most `lines` lines with an
/// ellipsis where it is cut, and always that many lines tall, so every card in a lane is as tall.
pub(super) fn body(text: String, lines: usize, t: TypeScale) -> Div {
    let el = dim(text).line_height(t.line).min_h(t.line * lines as f32);
    el.line_clamp(lines).text_ellipsis()
}

/// Records the card's bounds for the zoom's morph, and scrolls the selected card into view after
/// the selection moved (the least scroll that shows it whole).
fn recorder(ui: &Ui, id: &str, selected: bool) -> impl IntoElement {
    let (cards, id) = (ui.cards.clone(), id.to_string());
    let reveal = selected.then(|| (ui.reveal.clone(), ui.scroll.clone()));
    let record = move |b: Bounds<Pixels>, window: &mut Window, cx: &mut App| {
        cards.borrow_mut().insert(id, b);
        let Some((_, scroll)) = reveal.filter(|(r, _)| r.take()) else {
            return;
        };
        let (view, offset) = (scroll.bounds(), scroll.offset());
        let y = if b.top() < view.top() {
            offset.y + (view.top() - b.top())
        } else if b.bottom() > view.bottom() {
            offset.y - (b.bottom() - view.bottom())
        } else {
            return;
        };
        let y = y.clamp(-scroll.max_offset().y, px(0.));
        // After this frame, notifying the shell: it is a cached view, which a refresh would reuse.
        let shell = window.current_view();
        cx.defer(move |cx| {
            scroll.set_offset(point(offset.x, y));
            cx.notify(shell);
        });
    };
    canvas(record, |_, _, _, _| {}).absolute().size_full()
}
