//! The zoom body: the zoomed agent's transcript in compact mode, over a context strip and above its
//! queued messages. A GPUI `list` anchored at the bottom follows the tail while it is at the bottom.
//! Its rows are `condense::rows` of the store's items: a standalone item, or a run of activity drawn
//! as one strip of pills that a click (or `o`) opens to its members. A run has no key of its own; it is
//! the range of its members' `(offset, sub)` keys, which never move, so its open state, the members'
//! folds and the markdown cache are all kept by member key. An older page or a live entry is a splice
//! at either end (`plan`) that keeps the scroll position, growing the end rows in place; when a page grows
//! the head, `hold` keeps the member or pill at the viewport's top where it was. Laying out any
//! of the first rows asks for the page before, until the start. When the last row is a closed run, its
//! last member shows in full under its age (web's latest activity).
//!
//! Assistant text renders with the kit's markdown (fenced blocks highlighted by tree-sitter), with
//! mentions and paths linked by `markdown::link`; a click on one dispatches `OpenLink`, which the zoom
//! shell handles: an agent in this space becomes its tab, any other opens as a preview tab (never a
//! member), and a path resolves and opens in VS Code. Reaching the bottom counts as viewing. Text
//! selected here with the pointer is offered to the notes strip for capture (U5).

use crate::store::condense::{self, Pill, Row};
use crate::store::transcript::{Item, Key, Step, Tone, Transcript};
use crate::store::{Event, Store};
use crate::views::lens::{State, Ui};
use crate::views::markdown::{self, Mentions};
use crate::views::space::Zoom;
use crate::views::theme::{self, MONO_T, SANS_T, TypeScale, pal, type_scale};
use crate::views::{Host, dim};
use gpui_kit::base::{Scrollbar, ScrollbarMode};
use gpui_kit::component::text::TextView;
use gpui_kit::prelude::FluentBuilder as _;
use gpui_kit::*;
use std::cell::{Cell, RefCell};
use std::collections::{BTreeMap, BTreeSet, HashMap, HashSet};
use std::rc::Rc;
use std::time::{SystemTime, UNIX_EPOCH};

/// `j k space shift-space g G` (ARCHITECTURE §4).
#[derive(Clone, Copy, Debug, PartialEq, Action)]
#[action(namespace = transcript, no_json)]
pub enum Scroll {
    Lines(i8),
    Pages(i8),
    Top,
    Bottom,
}

/// A clicked `herder-agent:` or `herder-path:` link.
#[derive(Clone, Debug, PartialEq, Action)]
#[action(namespace = transcript, no_json)]
pub struct OpenLink(pub SharedString);

actions!(transcript, [ToggleRun]);

/// Rows above the viewport's top under which the page before is read. A row holds about four entries.
const PREFETCH: usize = 20;
/// Characters of a status pill, as web's (`statusChipChars`); opening the run shows it in full.
const STATUS: usize = 26;
/// Linked markdown kept per row; dropped beyond this, as the rows scroll by.
const MD_CACHE: usize = 1500;

type Linked = HashMap<(Key, bool), SharedString>;

/// The list and what it currently mirrors. Interior mutability: views render from `&Ui`.
pub struct View {
    pub(super) list: ListState,
    /// `(agent, generation)` the rows belong to, the item count they were grouped from (items are only
    /// ever inserted, so the count is a complete change signal), and the rows.
    pub(super) rows: RefCell<((String, u64), usize, Vec<Row>)>,
    /// Unfolded items (a tool's result, thinking, a long delivery), by key.
    open: RefCell<HashSet<Key>>,
    /// Open runs: a run is open while any of its members' keys is here, so it stays open as a page or
    /// an entry grows it at either end. Apart from `open`, so a member's own fold never closes its run.
    runs: RefCell<BTreeSet<Key>>,
    /// Linked text per row and fold state.
    md: RefCell<(Mentions, Linked)>,
    /// Herder web, where a mermaid diagram links to.
    web: String,
    /// The wheel's scroll handler is on the list.
    wheel: Cell<bool>,
    /// Where each row, and each mark in it, last laid out (window coordinates).
    pub(super) painted: Rc<RefCell<Painted>>,
    /// What was read when a page grew the head, for `hold`.
    anchor: Cell<Option<Anchor>>,
}

#[derive(Default)]
pub(super) struct Painted {
    pub(super) rows: HashMap<usize, Bounds<Pixels>>,
    pub(super) marks: Vec<(Mark, Bounds<Pixels>)>,
}

/// What a row draws that a reader's eye can be on: a member in full (open, or the latest), or a pill,
/// as the first and last members it merges.
#[derive(Clone, Copy, Debug, PartialEq)]
pub(super) enum Mark {
    Member(Key),
    Pill(Key, Key),
}

impl Mark {
    fn key(self) -> Key {
        match self {
            Mark::Member(key) | Mark::Pill(key, _) => key,
        }
    }

    /// The same thing as `was`, laid out again: a pill once grown at either end still holds its key.
    pub(super) fn is(self, was: Mark) -> bool {
        match (self, was) {
            (Mark::Member(a), Mark::Member(b)) => a == b,
            (Mark::Pill(first, last), Mark::Pill(key, _)) => first <= key && key <= last,
            _ => false,
        }
    }
}

/// At the viewport's top when a page grew the head: the mark (`None`: the row has none on screen), a
/// key in its row, and how far below the viewport's top the mark and the row were.
#[derive(Clone, Copy)]
struct Anchor {
    mark: Option<Mark>,
    key: Key,
    y: Pixels,
    row: Pixels,
}

impl Default for View {
    fn default() -> Self {
        let list = ListState::new(0, ListAlignment::Bottom, px(1200.));
        list.set_follow_mode(FollowMode::Tail);
        View {
            list,
            rows: RefCell::default(),
            open: RefCell::default(),
            runs: RefCell::default(),
            md: RefCell::default(),
            web: String::new(),
            wheel: Cell::default(),
            painted: Rc::default(),
            anchor: Cell::default(),
        }
    }
}

impl View {
    /// Zoomed out: let the rows and their linked text go.
    pub fn clear(&self) {
        self.list.reset(0);
        *self.rows.borrow_mut() = Default::default();
        self.md.borrow_mut().1 = HashMap::new();
        self.open.borrow_mut().clear();
        self.runs.borrow_mut().clear();
    }

    /// Point the list at the transcript's rows: a new transcript resets it, rows grown at either end
    /// are splices (`plan`), anything else (rare) a reset.
    pub(super) fn sync(&self, t: &Transcript, store: &Store) {
        let mut rows = self.rows.borrow_mut();
        let tag = (t.agent.clone(), t.generation);
        let same = rows.0 == tag;
        let names = store.fleet.agents.keys().map(String::as_str);
        let mentions = Mentions::new(names);
        let mut md = self.md.borrow_mut();
        if md.0 != mentions || !same || md.1.len() > MD_CACHE {
            *md = (mentions, HashMap::new());
        }
        if same && rows.1 == t.items.len() {
            return;
        }
        let new = condense::rows(&t.items);
        let m = rows.2.len();
        match plan(&rows.2, &new).filter(|_| same) {
            Some((before, after)) => {
                // A page that grows the head can push what is read down: `hold` puts it back.
                let head = new.first().map(Row::first) != rows.2.first().map(Row::first);
                if head && !self.list.is_following_tail() {
                    self.anchor.set(self.reading(&rows.2));
                }
                self.list.splice(m..m, after);
                self.list.splice(0..0, before);
                // The end rows may have grown, and a last row's height depends on being last.
                self.list.remeasure_items(before..before + 1);
                self.list.remeasure_items(before + m - 1..before + m);
            }
            None => {
                self.list.reset(new.len());
                self.list.set_follow_mode(FollowMode::Tail);
                self.open.borrow_mut().clear();
                self.runs.borrow_mut().clear();
            }
        }
        *rows = (tag, t.items.len(), new);
    }

    /// What the viewport's top shows, as last laid out: in the topmost row on screen, the mark nearest
    /// the viewport's top, else the row. A scroll not yet painted has only the list's top to go by.
    fn reading(&self, rows: &[Row]) -> Option<Anchor> {
        let painted = self.painted.borrow();
        let screen = self.list.viewport_bounds();
        let shown = |b: &Bounds<Pixels>| b.bottom() > screen.top() && b.top() < screen.bottom();
        let near = |b: &Bounds<Pixels>| f32::from((b.top() - screen.top()).abs());
        let on = painted.rows.iter().filter(|(_, b)| shown(b));
        let first = on.min_by(|a, b| f32::from(a.1.top()).total_cmp(&f32::from(b.1.top())));
        let top = self.list.logical_scroll_top();
        let Some((ix, at)) = first.filter(|(ix, _)| **ix == top.item_ix) else {
            // Scrolled since the last frame (a key, the harness's `find`): the list's top, not the paint.
            let y = -top.offset_in_item;
            let row = rows.get(top.item_ix)?;
            return Some(Anchor {
                mark: None,
                key: row.first(),
                y,
                row: y,
            });
        };
        let row = rows.get(*ix)?;
        let inside = |m: &Mark| row.first() <= m.key() && m.key() <= row.last();
        let marks = painted.marks.iter().filter(|(m, b)| inside(m) && shown(b));
        let mark = marks.min_by(|a, b| near(&a.1).total_cmp(&near(&b.1)));
        let row_y = at.top() - screen.top();
        Some(Anchor {
            mark: mark.map(|(m, _)| *m),
            key: mark.map_or(row.first(), |(m, _)| m.key()),
            y: mark.map_or(row_y, |(_, b)| b.top() - screen.top()),
            row: row_y,
        })
    }

    /// Open or close the run from `first` to `last`, list row `ix`.
    pub(super) fn toggle(&self, (first, last): (Key, Key), ix: usize) {
        let mut runs = self.runs.borrow_mut();
        let open: Vec<Key> = runs.range(first..=last).copied().collect();
        if open.is_empty() {
            runs.insert(first);
        }
        for key in open {
            runs.remove(&key);
        }
        self.list.remeasure_items(ix..ix + 1);
    }

    fn opened(&self, first: Key, last: Key) -> bool {
        self.runs.borrow().range(first..=last).next().is_some()
    }

    /// The rows, how many are runs and how many of those are open, for the harness.
    pub(super) fn census(&self) -> (usize, usize, usize) {
        let rows = &self.rows.borrow().2;
        let runs = rows.iter().filter_map(|r| match *r {
            Row::Run(first, last) => Some(self.opened(first, last)),
            Row::One(_) => None,
        });
        let runs: Vec<bool> = runs.collect();
        let open = runs.iter().filter(|&&o| o).count();
        (rows.len(), runs.len(), open)
    }

    /// Whether jump-to-bottom shows: while the list does not follow the tail.
    pub(super) fn jumps(&self) -> bool {
        !self.list.is_following_tail()
    }

    /// Whether the list follows the bottom, tagged with the transcript its rows mirror (`Step::Tail`).
    pub(super) fn tail(&self, tail: bool) -> Event {
        let (agent, generation) = self.rows.borrow().0.clone();
        Event::Transcript(Step::Tail {
            agent,
            generation,
            tail,
        })
    }
}

/// Where `new` rows go around the `old` ones the list holds, as rows before and after them: only when
/// `new` is `old` grown at its ends (a `before=` page, a live entry, or both), its first and last rows
/// perhaps grown into longer runs and every row between unchanged. `None`: anything else (a mid
/// insertion, which paging never makes), and the list resets.
pub fn plan(old: &[Row], new: &[Row]) -> Option<(usize, usize)> {
    let (first, last, m) = (old.first()?, old.last()?, old.len());
    // The new rows holding old's first and last rows.
    let b = new.partition_point(|r| r.last() < first.last());
    let e = new.partition_point(|r| r.last() < last.first());
    let ends = new.get(b)?.first() <= first.first() && new.get(e)?.last() >= last.last();
    let fits = ends && e + 1 == b + m && (m < 2 || old[1..m - 1] == new[b + 1..e]);
    fits.then(|| (b, new.len() - 1 - e))
}

/// What a row is, for the space around it (spec §2).
#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Kind {
    Answer,
    Strip,
    Card,
    Divider,
    System,
}

impl Kind {
    fn of(row: Row, items: &BTreeMap<Key, Item>) -> Kind {
        let Row::One(key) = row else {
            return Kind::Strip;
        };
        match items.get(&key) {
            Some(Item::Prompt(_) | Item::Delivery { .. }) => Kind::Card,
            Some(Item::CompactDivider(_)) => Kind::Divider,
            Some(Item::SystemChip(_)) => Kind::System,
            _ => Kind::Answer,
        }
    }

    /// Web's vertical margin around the kind: an assistant block, an activity strip, an entry card,
    /// the compact divider, a system chip.
    fn margin(self) -> f32 {
        match self {
            Kind::Answer => 10.,
            Kind::Strip => 5.,
            Kind::Card => 9.,
            Kind::Divider => 14.,
            Kind::System => 6.,
        }
    }
}

/// The transcript's padding under its last row and the first row's offset from its top (web's
/// padding 8 plus the window note's margin 2).
const PAD: f32 = 14.;
const TOP: f32 = 10.;

/// Web's space above a row after `prev` (`None`: the first row): margins collapse, so the larger of
/// the two kinds' margins.
pub fn gap(prev: Option<Kind>, next: Kind) -> f32 {
    prev.map_or(TOP, |p| p.margin().max(next.margin()))
}

/// `o`: open or close the lowest run on screen.
pub fn toggle_lowest(ui: &State) -> Vec<Event> {
    let view = &ui.transcript;
    let list = &view.list;
    let rows = view.rows.borrow();
    // As laid out: following the tail, the list keeps no top to ask.
    let screen = list.viewport_bounds();
    let painted = view.painted.borrow();
    let shown = |ix: usize| {
        let b = painted.rows.get(&ix);
        b.is_some_and(|b| b.bottom() > screen.top() && b.top() < screen.bottom())
    };
    let mut runs = rows
        .2
        .iter()
        .enumerate()
        .rev()
        .filter_map(|(ix, r)| match *r {
            Row::Run(first, last) => Some(((first, last), ix)),
            Row::One(_) => None,
        });
    if let Some((run, ix)) = runs.find(|&(_, ix)| shown(ix)) {
        view.toggle(run, ix);
    }
    Vec::new()
}

/// The first row on screen. Following the tail, the list lays out up from the end and keeps no top:
/// the first row when every row fits, else (somewhere near the bottom) the row count.
fn top(list: &ListState) -> usize {
    let (top, n) = (list.logical_scroll_top().item_ix, list.item_count());
    let fits = list.max_offset_for_scrollbar().y <= px(0.);
    if top < n || !fits { top.min(n) } else { 0 }
}

/// Leaving the bottom is published as it happens (a scroll key, the wheel), so a fleet frame drained
/// before the next render does not land as seen. Coming back is seen by that render.
fn left(store: &Store, view: &View, following: bool) -> Option<Event> {
    let open = store.transcript.open.as_ref()?;
    (open.tail && !following).then(|| view.tail(false))
}

/// Herder web's address (the shell's server), for diagram links.
pub fn set_web(ui: &mut State, base: &str) {
    ui.transcript.web = base.trim_end_matches('/').to_string();
}

/// A scroll key: lines are three text lines, pages most of the viewport. `g` goes to the top of the
/// loaded rows, which reads the page before.
pub fn scroll(store: &Store, ui: &mut State, s: Scroll) -> Vec<Event> {
    let list = &ui.transcript.list;
    let line = type_scale(store.prefs.text_scale).line * 3.;
    let page = list.viewport_bounds().size.height * 0.9;
    // The wheel's arithmetic: `scroll_by` counts from the follow anchor (the content's end, not the
    // viewport's top), so scrolling up from the tail by less than a viewport would snap back.
    let by = |d: Pixels| {
        let top = (-list.scroll_px_offset_for_scrollbar().y).min(list.max_offset_for_scrollbar().y);
        list.set_offset_from_scrollbar(point(px(0.), -(top + d)));
    };
    match s {
        Scroll::Lines(n) => by(line * n as f32),
        Scroll::Pages(n) => by(page * n as f32),
        Scroll::Top => list.scroll_to(ListOffset::default()),
        Scroll::Bottom => list.set_follow_mode(FollowMode::Tail),
    }
    let view = &ui.transcript;
    left(store, view, view.list.is_following_tail())
        .into_iter()
        .collect()
}

/// The body under the tabs.
pub fn render<H: Host>(
    store: &Store,
    ui: &Ui,
    zoom: &Zoom,
    t: TypeScale,
    cx: &mut Context<H>,
) -> Div {
    let body = div().flex_1().min_h_0().flex().flex_col();
    let Some(name) = zoom.agent.as_deref() else {
        return body.p(t.css(24.)).child(dim("No agents in this space."));
    };
    let tr = store.transcript.open.as_ref().filter(|tr| tr.agent == name);
    let Some(tr) = tr else {
        // Morphing back to the lens, the transcript is already gone.
        return body.when(ui.zoom.is_some(), |b| {
            b.p(t.css(24.)).child(dim("loading…"))
        });
    };
    let view = &ui.transcript;
    view.sync(tr, store);
    let hold = hold(view, t, cx.weak_entity());
    *view.painted.borrow_mut() = Painted::default();
    // Read the page before while the viewport's top is near the first rows (or there are none).
    let top = top(&view.list);
    let more = tr.loaded() && !tr.at_start() && !tr.paging() && !tr.blocked();
    if more && top < PREFETCH {
        let event = Event::Transcript(Step::Older);
        cx.spawn(async move |host, cx| host.update(cx, |h, cx| h.dispatch(event, cx)))
            .detach();
    }
    // Following the bottom is watching the tail: the store sees what lands (`attention::watch`). Seen
    // here, it is read again as it is dispatched, and the store drops it if the transcript moved on.
    if view.list.is_following_tail() != tr.tail {
        let follow = |h: &mut H, cx: &mut Context<H>| {
            let view = &h.view().1.transcript;
            let event = view.tail(view.list.is_following_tail());
            h.dispatch(event, cx)
        };
        cx.spawn(async move |host, cx| host.update(cx, follow))
            .detach();
    }
    // The wheel stops following inside the list's own handler: published from there at once.
    if !view.wheel.replace(true) {
        let host = cx.weak_entity();
        let wheel = move |e: &ListScrollEvent, _: &mut Window, cx: &mut App| {
            let publish = |h: &mut H, cx: &mut Context<H>| {
                let (store, ui) = h.view();
                if let Some(event) = left(store, &ui.transcript, e.is_following_tail) {
                    h.dispatch(event, cx)
                }
            };
            host.update(cx, publish).ok();
        };
        view.list.set_scroll_handler(wheel);
    }
    let note = |s: &'static str| {
        div()
            .flex_1()
            .p(t.css(24.))
            .child(dim(s))
            .into_any_element()
    };
    let rows = if !tr.loaded() {
        note("loading transcript…")
    } else if tr.items.is_empty() {
        note("(nothing readable yet)")
    } else {
        let host = cx.weak_entity();
        let rows = list(view.list.clone(), move |ix, _, cx| match host.upgrade() {
            Some(host) => row::<H>(host.read(cx).view(), ix, t, host.downgrade()),
            None => div().into_any_element(),
        });
        // With web's overlay scrollbar and jump-to-bottom.
        let frame = div().relative().flex_1().min_h_0().flex().flex_col();
        frame
            .child(rows.flex_1())
            .child(scrollbar(&view.list, t))
            .when(view.jumps(), |f| f.child(jump(t)))
            .into_any_element()
    };
    let head = strip(store, tr, t, cx);
    let queued = tr.detail.as_ref().and_then(|d| d.queued.clone());
    let queued = queued
        .filter(|_| !tr.retired())
        .into_iter()
        .flatten()
        .map(|q| {
            let first = q.preview.lines().next().unwrap_or("");
            let line = format!("queued · {}: {first}", q.sender);
            dim(line).truncate().px(t.css(PAD)).text_size(t.small)
        });
    let notice = tr.notice().map(|n| {
        let dismiss = cx
            .listener(|h, _: &ClickEvent, _, cx| h.dispatch(Event::Transcript(Step::Dismiss), cx));
        let el = div().id("notice").px(t.css(PAD)).py(t.css(4.));
        el.text_size(t.small)
            .text_color(rgb(pal::AMBER))
            .child(format!("{n}  ✕"))
            .on_click(dismiss)
    });
    // Where the pointer lets go, the selection it made is what `c` (or the strip's chip) captures.
    let let_go = cx.listener(|h: &mut H, _: &MouseUpEvent, window, cx| {
        let text = gpui_kit::base::TextSelection::selected_text(window, cx);
        let ui = h.parts().1;
        let agent = ui.zoomed_agent().map(String::from);
        if ui.notes.selected(agent, &text) {
            cx.notify();
        }
    });
    body.capture_any_mouse_up(let_go)
        .child(head)
        .children(hold)
        .child(rows)
        .children(queued)
        .children(notice)
}

/// Web's scrollbar as Chromium draws it (spec §1 "Scrollbar"): a rounded #3a3c45 thumb about 8 wide on
/// no track, the same under the pointer. An overlay shown while scrolling, as macOS draws them.
fn scrollbar(list: &ListState, t: TypeScale) -> Scrollbar {
    let thumb = |s: gpui_kit::base::ScrollbarThumbStyle| {
        let s = s.bg(rgb(pal::EDGE)).width(t.css(8.)).radius(t.css(4.));
        s.inset(t.css(3.))
    };
    Scrollbar::vertical(list)
        .mode(ScrollbarMode::Scrolling)
        .styles(|s| s.thumb(thumb))
}

/// Web's jump-to-bottom (spec §1 "Jump to bottom"): an accent pill centred 10 above the bottom; a
/// click goes to the tail at once and follows it, as `G` does.
fn jump(t: TypeScale) -> impl IntoElement {
    let shadow = BoxShadow {
        color: hsla(0., 0., 0., 0.4),
        offset: point(px(0.), t.css(2.)),
        blur_radius: t.css(8.),
        spread_radius: px(0.),
        inset: false,
    };
    let button = div()
        .id("jump")
        .cursor_pointer()
        .px(t.css(9.))
        .py(t.css(5.));
    let button = button
        .font_family(SANS_T)
        .text_size(t.css(13.))
        .line_height(relative(1.5))
        .border_1()
        .border_color(rgb(pal::BLUE))
        .bg(rgb(pal::BLUE))
        .text_color(rgb(0xFFFFFF))
        .rounded(t.css(14.))
        .shadow(vec![shadow])
        .child("↓ Jump to bottom")
        .on_click(|_, window, cx| window.dispatch_action(Box::new(Scroll::Bottom), cx));
    let wrap = div().absolute().left_0().right_0().bottom(t.css(10.));
    wrap.flex().justify_center().child(button)
}

/// Model and context used, the working directory (opens in VS Code) and a read-only mark.
fn strip<H: Host>(store: &Store, tr: &Transcript, t: TypeScale, cx: &mut Context<H>) -> Div {
    let d = tr.detail.as_ref();
    let model = d.and_then(|d| d.model.clone());
    let ctx = d.and_then(|d| d.context_usage.clone()).map(|c| {
        let pct = c.used_percent.map(|p| format!(" ({p:.0}%)"));
        format!("ctx {}k{}", c.used_tokens / 1000, pct.unwrap_or_default())
    });
    let cwd = d
        .and_then(|d| d.cwd.clone())
        .or_else(|| store.fleet.agents.get(&tr.agent)?.cwd.clone());
    let open =
        cx.listener(|h, _: &ClickEvent, _, cx| h.dispatch(Event::Transcript(Step::OpenCwd), cx));
    let cwd = cwd.map(|c| {
        let link = div().id("cwd").cursor_pointer().on_click(open);
        link.text_color(rgb(pal::ACC)).child(format!("{c} ↗"))
    });
    let facts = [model, ctx].into_iter().flatten().map(dim);
    let retired = div()
        .text_color(rgb(pal::AMBER))
        .child("retired · read-only");
    let bar = div().flex().items_center().gap(t.px(14.));
    bar.px(t.px(20.))
        .py(t.px(5.))
        .border_b_1()
        .border_color(rgb(pal::RULE))
        .text_size(t.small)
        .children(facts)
        .children(cwd)
        .when(tr.retired(), |el| el.child(retired))
        .when(tr.paging(), |el| el.child(dim("reading older…")))
}

/// One row of the list: an item, or a run.
fn row<H: Host>(
    (store, ui): (&Store, &Ui),
    ix: usize,
    t: TypeScale,
    host: WeakEntity<H>,
) -> AnyElement {
    let view = &ui.transcript;
    let rows = view.rows.borrow();
    let tr = store.transcript.open.as_ref();
    let tr = tr.filter(|tr| tr.agent == rows.0.0);
    let (Some(tr), Some(&r)) = (tr, rows.2.get(ix)) else {
        return div().into_any_element();
    };
    let paint = Paint {
        view,
        tr,
        t,
        host,
        ix,
    };
    // In web's type (spec §1 "Transcript container"), here rather than on the list so that `hold`, which
    // lays a row out alone, measures it the same. Spaced as web's margins (spec §2), with the
    // transcript's padding under the last row.
    let kind = |ix: usize| Kind::of(rows.2[ix], &tr.items);
    let last = ix + 1 == rows.2.len();
    let above = gap(ix.checked_sub(1).map(kind), kind(ix));
    let below = if last { kind(ix).margin() + PAD } else { 0. };
    let el = div().relative().w_full().px(t.css(PAD));
    let el = el.pt(t.css(above)).pb(t.css(below)).font_family(SANS_T);
    let el = el.text_size(t.css(13.)).line_height(relative(1.5));
    let el = el.child(record(&view.painted, move |p, b| p.rows.insert(ix, b)));
    match r {
        Row::One(key) => el.children(tr.items.get(&key).map(|item| paint.body(key, item))),
        Row::Run(first, last_key) => el.child(paint.run((first, last_key), last)),
    }
    .into_any_element()
}

/// What a row draws with: the list row `ix`, which a fold or run toggle remeasures.
struct Paint<'a, H> {
    view: &'a View,
    tr: &'a Transcript,
    t: TypeScale,
    host: WeakEntity<H>,
    ix: usize,
}

impl<H: Host> Paint<'_, H> {
    fn id(&self, kind: &str, key: Key) -> ElementId {
        let generation = self.tr.generation;
        ElementId::Name(format!("{kind}-{generation}-{}-{}", key.0, key.1).into())
    }

    /// A run: its strip of pills, a click opening it to every member behind a rule. Closed and last,
    /// its last member is drawn in full under its age instead of as a pill.
    fn run(&self, (first, last): (Key, Key), tail: bool) -> Div {
        let (view, tr, t, ix) = (self.view, self.tr, self.t, self.ix);
        let members = || tr.items.range(first..=last);
        let open = view.opened(first, last);
        let latest = members().next_back().filter(|_| tail && !open);
        let n = members().count() - usize::from(latest.is_some());
        let pills = condense::pills(members().take(n).map(|(_, item)| item));
        let keys: Vec<Key> = members().take(n).map(|(&key, _)| key).collect();
        let host = self.host.clone();
        let toggle = move |_: &ClickEvent, _: &mut Window, cx: &mut App| {
            let _ = host.update(cx, |h, cx| {
                h.parts().1.transcript.toggle((first, last), ix);
                cx.notify();
            });
        };
        let strip = (!pills.is_empty()).then(|| {
            let el = div().id(self.id("run", first)).cursor_pointer();
            el.on_click(toggle)
                .flex()
                .flex_wrap()
                .items_center()
                .gap(t.css(5.))
                .child(dim(if open { "⌄" } else { "›" }))
                .children(pills.iter().enumerate().map(|(i, p)| {
                    let end = pills.get(i + 1).map_or(n, |next| next.at);
                    let mark = Mark::Pill(keys[p.at], keys[end - 1]);
                    let at = record(&view.painted, move |pt, b| pt.marks.push((mark, b)));
                    pill(p, t).relative().child(at)
                }))
        });
        let each = members().map(|(&key, item)| {
            let mark = Mark::Member(key);
            let at = record(&view.painted, move |p, b| p.marks.push((mark, b)));
            let el = div().relative().pb(t.css(4.));
            el.child(self.body(key, item)).child(at)
        });
        let detail = open.then(|| {
            let el = div().border_l_1().border_color(rgb(pal::RULE));
            el.ml(t.css(4.)).pl(t.css(12.)).pt(t.css(4.)).children(each)
        });
        let latest = latest.map(|(&key, item)| {
            let now = SystemTime::now().duration_since(UNIX_EPOCH);
            let now = now.map_or(0, |d| d.as_secs());
            let age = tr.times.get(&key.0).map(|&at| ago(now.saturating_sub(at)));
            let age = age.unwrap_or_else(|| "time unknown".into());
            let line = dim(format!("latest · {age}"));
            let line = line.font_family(MONO_T).text_size(t.css(9.));
            let mark = Mark::Member(key);
            let at = record(&view.painted, move |p, b| p.marks.push((mark, b)));
            let el = div().relative().pt(t.css(3.)).child(line);
            el.child(self.body(key, item)).child(at)
        });
        let el = div().flex().flex_col().gap(t.css(2.));
        el.children(strip).children(detail).children(latest)
    }

    /// One item as its own row, or as a member of an open run or the latest.
    fn body(&self, key: Key, item: &Item) -> AnyElement {
        let (view, tr, t, ix) = (self.view, self.tr, self.t, self.ix);
        let open = view.open.borrow().contains(&key);
        let host = self.host.clone();
        let toggle = move |_: &ClickEvent, _: &mut Window, cx: &mut App| {
            let _ = host.update(cx, |h, cx| {
                let v = &h.parts().1.transcript;
                let mut o = v.open.borrow_mut();
                if !o.remove(&key) {
                    o.insert(key);
                }
                v.list.remeasure_items(ix..ix + 1);
                cx.notify();
            });
        };
        let id = |kind: &str| self.id(kind, key);
        let small = |text: String| dim(text).text_size(t.css(10.));
        let glyph = if open { "▾" } else { "▸" };
        let fold = |text: String| {
            let el = div().id(id("fold")).cursor_pointer();
            el.on_click(toggle.clone())
                .child(small(format!("{glyph} {text}")))
        };
        // Prompts, deliveries and assistant text: linked, selectable markdown (U5's selection seam).
        let md = |text: &str| {
            let linked = {
                let mut md = view.md.borrow_mut();
                let (mentions, cache) = &mut *md;
                let web = format!("{}/agents/{}", view.web, tr.agent);
                let link = || SharedString::from(markdown::link(text, mentions, &web));
                cache.entry((key, open)).or_insert_with(link).clone()
            };
            let text = TextView::markdown(id("md"), linked).selectable(true);
            let text = text.line_height(relative(1.55));
            // No actions are drawn; asking for them gives each fenced block an id, which its sideways
            // scroll keeps its offset under.
            let text = text
                .style(theme::prose(t))
                .code_block_actions(|_, _, _| Empty);
            let text = text.on_link_click(|url, _, window, cx| match markdown::route(url) {
                Some(link) => window.dispatch_action(Box::new(OpenLink(link.into())), cx),
                None if url.starts_with("http://") || url.starts_with("https://") => {
                    cx.open_url(url)
                }
                None => {}
            });
            sideways(div().max_w(t.css(900.)).child(text))
        };
        match item {
            Item::Prompt(text) => div()
                .bg(rgb(pal::ACCW))
                .border_l(t.css(3.))
                .border_color(rgb(pal::ACC))
                .rounded(t.css(4.))
                .px(t.css(10.))
                .py(t.css(6.))
                .child(md(text))
                .into_any_element(),
            Item::Delivery {
                sender,
                text,
                operator,
            } => {
                let from = format!("← {sender}{}", if *operator { " · operator" } else { "" });
                let (shown, long) = preview(text, open);
                let more = long.then(|| fold(if open { "less" } else { "more" }.into()));
                let el = div().child(small(from)).child(md(&shown));
                el.children(more).into_any_element()
            }
            // The pill uncut, then what it stands for when that says more.
            Item::Chip { tone, label, text } => {
                let full = Pill {
                    tone: *tone,
                    label: label.clone(),
                    count: 1,
                    error: false,
                    at: 0,
                };
                let more = (text != label && !text.is_empty()).then(|| small(text.clone()));
                let el = div().flex().flex_col().items_start().gap(t.css(2.));
                el.child(chip(&full, label.clone(), t).max_w_full())
                    .children(more)
                    .into_any_element()
            }
            Item::SystemChip(s) => small(format!("· {s}")).into_any_element(),
            Item::CompactDivider(s) => {
                let rule = || div().flex_1().h(px(1.)).bg(rgb(pal::PURPLE_RULE));
                let el = div().flex().items_center().gap(t.css(8.));
                el.text_size(t.css(10.))
                    .text_color(rgb(pal::PURPLE))
                    .child(rule())
                    .child(s.clone())
                    .child(rule())
                    .into_any_element()
            }
            Item::Assistant { markdown } => md(markdown).into_any_element(),
            Item::Thinking(text) if text.trim().is_empty() => {
                small("∴ thinking".into()).into_any_element()
            }
            Item::Thinking(text) => div()
                .child(fold("thinking".into()))
                .when(open, |el| el.child(small(text.clone())))
                .into_any_element(),
            Item::Tool {
                name,
                summary,
                result,
            } => {
                let err = result.as_ref().is_some_and(|r| r.error);
                let mark = if err { " ✗" } else { "" };
                let line = fold(format!("{name} {summary}{mark}")).truncate();
                let line = line.when(err, |el| el.text_color(rgb(pal::PORT)));
                let detail = result.as_ref().filter(|_| open);
                let detail = detail.map(|r| small(format!("→ {}", r.text)));
                div().child(line).children(detail).into_any_element()
            }
            Item::Error(text) => div()
                .text_color(rgb(pal::PORT))
                .child(format!("✗ {text}"))
                .into_any_element(),
        }
    }
}

/// Records where its parent laid out into `painted` (a parent drawn this frame is on screen or in
/// the list's overdraw).
fn record<T>(
    painted: &Rc<RefCell<Painted>>,
    put: impl Fn(&mut Painted, Bounds<Pixels>) -> T + 'static,
) -> impl IntoElement {
    let painted = painted.clone();
    let at = move |b, _: &mut Window, _: &mut App| {
        put(&mut painted.borrow_mut(), b);
    };
    canvas(at, |_, _, _, _| {})
        .absolute()
        .top_0()
        .left_0()
        .size_full()
}

/// Keeps what is read in place when a page grows the head (`View::anchor`): before the list lays out,
/// lays the row now holding the anchor out of sight, finds the same mark in it (else takes the row's
/// top) and scrolls so that sits where it was. However the row grew or rewrapped, above or below.
fn hold<H: Host>(view: &View, t: TypeScale, host: WeakEntity<H>) -> Option<impl IntoElement> {
    let a = view.anchor.take()?;
    let ix = view.rows.borrow().2.partition_point(|r| r.last() < a.key);
    let (list, painted) = (view.list.clone(), view.painted.clone());
    let measure = move |_, window: &mut Window, cx: &mut App| {
        let Some(h) = host.upgrade() else { return };
        let mut el = row::<H>(h.read(cx).view(), ix, t, host.clone());
        let width = AvailableSpace::Definite(list.viewport_bounds().size.width);
        let space = size(width, AvailableSpace::MinContent);
        // Far above the window: measured, never seen or hit.
        let away = point(px(0.), px(-100_000.));
        let was = std::mem::take(&mut *painted.borrow_mut());
        el.prepaint_as_root(away, space, window, cx);
        let laid = std::mem::replace(&mut *painted.borrow_mut(), was);
        let mark = a
            .mark
            .and_then(|m| laid.marks.iter().copied().find(|(n, _)| n.is(m)));
        let (at, y) = mark.map_or((px(0.), a.row), |(_, b)| (b.top() - away.y, a.y));
        list.scroll_to(ListOffset {
            item_ix: ix,
            offset_in_item: (at - y).max(px(0.)),
        });
    };
    Some(canvas(measure, |_, _, _, _| {}))
}

/// A run's pill: `Bash ×4`, a status cut to web's width, red when a merged tool failed.
fn pill(p: &Pill, t: TypeScale) -> Div {
    let text = match p.count {
        1 => p.label.clone(),
        n => format!("{} ×{n}", p.label),
    };
    let text = match p.tone {
        Tone::Status => cut(&text, STATUS),
        _ => text,
    };
    chip(p, text, t).flex_none()
}

fn chip(p: &Pill, text: String, t: TypeScale) -> Div {
    let (border, ground, ink) = pal::chip(p.tone);
    let (border, ink) = if p.error {
        (pal::PORT, pal::PORT)
    } else {
        (border, ink)
    };
    div()
        .px(t.css(7.))
        .border_1()
        .border_color(rgb(border))
        .bg(rgb(ground))
        .text_color(rgb(ink))
        .rounded(t.css(10.))
        .font_family(MONO_T)
        .text_size(t.css(9.))
        .child(text)
}

/// `text` with its whitespace collapsed, cut to `max` characters with an ellipsis.
fn cut(text: &str, max: usize) -> String {
    let text = text.split_whitespace().collect::<Vec<_>>().join(" ");
    match text.char_indices().nth(max) {
        Some(_) => {
            let keep: String = text.chars().take(max - 1).collect();
            format!("{keep}…")
        }
        None => text,
    }
}

/// How long ago, in web's units: seconds (at least one), minutes, hours or days, rounded.
pub fn ago(secs: u64) -> String {
    let round = |unit: u64| (secs + unit / 2) / unit;
    match secs {
        0..60 => format!("{}s", secs.max(1)),
        60..3600 => format!("{}m", round(60)),
        3600..86_400 => format!("{}h", round(3600)),
        _ => format!("{}d", round(86_400)),
    }
}

/// Keeps a sideways gesture off the list. A fenced block scrolls itself sideways (`theme::prose`), but
/// the event goes on bubbling, and GPUI's list would apply its vertical part. Underneath the block, in
/// bubble order, this stops a gesture that is mostly sideways; a mostly vertical one goes on to the list.
/// The list never scrolls sideways, so stopping one over prose loses nothing.
pub(super) fn sideways(el: Div) -> Div {
    el.on_scroll_wheel(|e: &ScrollWheelEvent, _, cx| {
        let d = e.delta.pixel_delta(px(16.));
        if d.x.abs() > d.y.abs() {
            cx.stop_propagation();
        }
    })
}

/// A delivery as shown, and whether it is long: web's preview, tighter, of at most five lines and 420
/// characters cut at a word, unless unfolded.
fn preview(text: &str, open: bool) -> (String, bool) {
    let long = text.lines().count() > 5 || text.len() > 420;
    if !long || open {
        return (text.to_string(), long);
    }
    let joined = text.lines().take(5).collect::<Vec<_>>().join("\n");
    let Some((at, _)) = joined.char_indices().nth(420) else {
        return (format!("{joined}…"), long);
    };
    let cut = &joined[..at];
    let word = cut.rfind(' ').filter(|&s| s > 210);
    (format!("{}…", word.map_or(cut, |s| &cut[..s])), long)
}
