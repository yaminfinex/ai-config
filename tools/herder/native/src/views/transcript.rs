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

use crate::store::condense::{self, Pill, Row, Seg};
use crate::store::transcript::{Item, Key, Step, Tone, Transcript};
use crate::store::{Effect, Event, Store};
use crate::views::entries::{
    self, answer, bits, card, chevron, expander, header, md_detail, mono, now, queued, stamp,
    system, time, toned,
};
use crate::views::lens::{State, Ui};
use crate::views::markdown::{self, Mentions};
use crate::views::space::Zoom;
use crate::views::theme::{self, MONO_T, SANS_T, TypeScale, pal, type_scale};
use crate::views::{Host, dim};
use gpui_kit::base::TextView;
use gpui_kit::base::{Scrollbar, ScrollbarMode};
use gpui_kit::prelude::FluentBuilder as _;
use gpui_kit::*;
use std::cell::{Cell, RefCell};
use std::collections::{BTreeMap, BTreeSet, HashMap, HashSet};
use std::rc::Rc;

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

/// Open or close an item's part (`View::open`): a click on a fold, a status chip or an internal note.
#[derive(Clone, Copy, Debug, PartialEq, Action)]
#[action(namespace = transcript, no_json)]
pub struct Fold(pub Key, pub usize);

actions!(transcript, [ToggleRun]);

/// Rows above the viewport's top under which the page before is read. A row holds about four entries.
const PREFETCH: usize = 20;
/// Characters of a status pill, as web's (`statusChipChars`); opening the run shows it in full.
const STATUS: usize = 26;
/// Linked markdown kept per row; dropped beyond this, as the rows scroll by.
const MD_CACHE: usize = 1500;

/// By item key and part: an answer's segment, a delivery cut (0) or whole (1), a summary (1).
type Linked = HashMap<(Key, usize), SharedString>;

/// The list and what it currently mirrors. Interior mutability: views render from `&Ui`.
pub struct View {
    pub(super) list: ListState,
    /// `(agent, generation)` the rows belong to, the item count they were grouped from (items are only
    /// ever inserted, so the count is a complete change signal), and the rows.
    pub(super) rows: RefCell<((String, u64), usize, Vec<Row>)>,
    /// Unfolded parts, by item key and part: an item's own fold is part 0 (a tool's result, thinking,
    /// a long delivery, a compaction's summary), an answer's are its fenced segments (a status chip,
    /// an internal note). By key, so they stay open as pages regroup the rows.
    open: RefCell<HashSet<(Key, usize)>>,
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
    /// The list's width as last laid out, which cards indent by a share of (rows cannot ask the list
    /// while it lays them out).
    width: Cell<Pixels>,
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
            width: Cell::default(),
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
        // The paint holds only if the list's top is where it painted: the same row, at the same offset.
        let fresh = |(ix, at): &(&usize, &Bounds<Pixels>)| {
            let off = at.top() - screen.top() + top.offset_in_item;
            **ix == top.item_ix && off.abs() < px(0.5)
        };
        let Some((ix, at)) = first.filter(fresh) else {
            // Scrolled since the last frame (a key, the scrollbar, the harness's `find`), even within
            // one row: the list's top, not the paint.
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

    /// Open or close an item's part, remeasuring the row that holds it.
    pub(super) fn fold(&self, Fold(key, part): Fold) {
        let mut open = self.open.borrow_mut();
        if !open.remove(&(key, part)) {
            open.insert((key, part));
        }
        let ix = self.rows.borrow().2.partition_point(|r| r.last() < key);
        self.list.remeasure_items(ix..ix + 1);
    }

    /// How many answer parts are open of each kind, status chips and internal notes, for the harness.
    pub(super) fn parts(&self, items: &BTreeMap<Key, Item>) -> (usize, usize) {
        let open = self.open.borrow();
        let seg = |&(key, part): &(Key, usize)| match items.get(&key) {
            Some(Item::Assistant(segs)) => segs.get(part),
            _ => None,
        };
        let segs: Vec<&Seg> = open.iter().filter_map(seg).collect();
        let status = segs.iter().filter(|s| matches!(s, Seg::Status(_))).count();
        (status, segs.len() - status)
    }

    /// The first tool in an open run (`failed`: that failed), which `click:member` (`click:failed`)
    /// opens, for the harness.
    pub(super) fn first_tool(&self, items: &BTreeMap<Key, Item>, failed: bool) -> Option<Key> {
        let rows = &self.rows.borrow().2;
        let open = rows.iter().filter_map(|r| match *r {
            Row::Run(first, last) if self.opened(first, last) => Some(items.range(first..=last)),
            _ => None,
        });
        let mut tools = open.flatten().filter(|(_, i)| match i {
            Item::Tool { result, .. } => !failed || result.as_ref().is_some_and(|r| r.error),
            _ => false,
        });
        tools.next().map(|(&key, _)| key)
    }

    /// How many tools are open, and how many of those show an output, for the harness.
    pub(super) fn tools(&self, items: &BTreeMap<Key, Item>) -> (usize, usize) {
        let open = self.open.borrow();
        let result = |&(key, part): &(Key, usize)| match items.get(&key) {
            Some(Item::Tool { result, .. }) if part == 0 => Some(result.is_some()),
            _ => None,
        };
        let tools: Vec<bool> = open.iter().filter_map(result).collect();
        (tools.len(), tools.iter().filter(|&&r| r).count())
    }

    /// Bring the member `key` of an open run to the viewport's top, as a page's anchor does (`hold`);
    /// `false` when its run is closed. For the harness's `find:`.
    pub(super) fn reveal(&self, key: Key) -> bool {
        let rows = self.rows.borrow();
        let ix = rows.2.partition_point(|r| r.last() < key);
        let open = rows
            .2
            .get(ix)
            .is_some_and(|r| self.opened(r.first(), r.last()));
        if open {
            let (mark, y) = (Some(Mark::Member(key)), px(0.));
            self.anchor.set(Some(Anchor {
                mark,
                key,
                y,
                row: y,
            }));
        }
        open
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
    /// An answer of only statuses and notes, bare (web's `.assistant-fenced-content`).
    Fenced,
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
        items.get(&key).map_or(Kind::Answer, Kind::item)
    }

    /// An item's kind, as its own row or as a run's member: a tool, a thinking or a pill is a fold.
    fn item(item: &Item) -> Kind {
        match item {
            Item::Prompt(_) | Item::Delivery { .. } => Kind::Card,
            Item::CompactDivider(_) => Kind::Divider,
            Item::SystemChip(_) | Item::CompactSummary(_) => Kind::System,
            Item::Tool { .. } | Item::Thinking(_) | Item::Chip { .. } => Kind::System,
            Item::Assistant(segs) if condense::marker(segs).is_some() => Kind::Fenced,
            Item::Assistant(_) | Item::Error(_) => Kind::Answer,
        }
    }

    /// Web's vertical margin around the kind: an assistant block, a bare fenced answer, an activity strip, an entry card,
    /// the compact divider, a system chip or a fold (`.entry-expander`, a compaction's summary).
    fn margin(self) -> f32 {
        match self {
            Kind::Answer => 10.,
            Kind::Fenced | Kind::Strip => 5.,
            Kind::Card => 9.,
            Kind::Divider => 14.,
            Kind::System => 6.,
        }
    }
}

/// The transcript's padding under its last row and the first row's offset from its top (web's
/// padding 8 plus the window note's margin 2).
pub(super) const PAD: f32 = 14.;
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

/// Reduce an event as the shell does: a tail the list left by a route that publishes nothing (the
/// scrollbar's handle moves the list without its scroll handler) is published first, so a fleet frame
/// that lands before the next render is not seen.
pub fn reduce(store: &mut Store, ui: &State, event: Event) -> Vec<Effect> {
    let view = &ui.transcript;
    let left = left(store, view, view.list.is_following_tail());
    let mut effects = left.map(|e| store.apply(e)).unwrap_or_default();
    effects.extend(store.apply(event));
    effects
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
    view.width.set(view.list.viewport_bounds().size.width);
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
    let waiting = tr.detail.as_ref().and_then(|d| d.queued.as_deref());
    let waiting = waiting.filter(|_| !tr.retired()).and_then(|q| queued(q, t));
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
        .children(waiting)
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
    let width = view.width.get() - t.css(PAD) * 2.;
    let paint = Paint {
        view,
        tr,
        t,
        host,
        ix,
        width,
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
        Row::One(key) => el.children(tr.items.get(&key).map(|item| paint.body(key, item, false))),
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
    /// The row's width inside its padding, which cards indent by a share of.
    width: Pixels,
}

impl<H: Host> Paint<'_, H> {
    fn id(&self, kind: &str, key: Key) -> ElementId {
        let generation = self.tr.generation;
        ElementId::Name(format!("{kind}-{generation}-{}-{}", key.0, key.1).into())
    }

    /// A run (spec §1 "Activity strip", "Expanded run details", "Latest activity"): its strip of
    /// pills, a click opening it to every member on a rail. Closed and last, its last member is drawn
    /// in full under its age instead of as a pill.
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
        let lit = |el: Stateful<Div>| el.border_color(rgb(pal::RULE)).bg(rgb(pal::PANEL));
        let strip = (!pills.is_empty()).then(|| {
            let el = div()
                .id(self.id("run", first))
                .cursor_pointer()
                .on_click(toggle);
            let el = el.flex().flex_wrap().items_center().gap(t.css(5.));
            let el = el.min_h(t.css(24.)).px(t.css(5.)).py(t.css(2.));
            let el = el.border_1().rounded(t.css(6.)).text_size(t.css(10.));
            let el = match open {
                true => lit(el),
                false => el
                    .border_color(transparent_black())
                    .hover(|s| s.border_color(rgb(pal::RULE)).bg(rgb(pal::PANEL))),
            };
            el.child(chevron(open, t))
                .children(pills.iter().enumerate().map(|(i, p)| {
                    let end = pills.get(i + 1).map_or(n, |next| next.at);
                    let mark = Mark::Pill(keys[p.at], keys[end - 1]);
                    let at = record(&view.painted, move |pt, b| pt.marks.push((mark, b)));
                    pill(p, t).relative().child(at)
                }))
        });
        // Members on the rail, each a fold 6 apart, or an answer or card at its own margins (spec §2).
        let detail = open.then(|| {
            let mut above = None;
            let each = members().map(|(&key, item)| {
                let kind = Kind::item(item);
                let gap = above.map_or(kind.margin(), |a: Kind| a.margin().max(kind.margin()));
                above = Some(kind);
                let mark = Mark::Member(key);
                let at = record(&view.painted, move |p, b| p.marks.push((mark, b)));
                let el = div().relative().pt(t.css(gap));
                el.child(self.body(key, item, true)).child(at)
            });
            let el = div().flex().flex_col().children(each.collect::<Vec<_>>());
            let below = above.map_or(0., Kind::margin);
            let el = el
                .border_l_1()
                .border_color(rgb(pal::RULE))
                .text_color(rgb(pal::SLATE));
            el.pt(t.css(2.))
                .pr(t.css(8.))
                .pb(t.css(4. + below))
                .pl(t.css(14.))
        });
        // Its member's margins collapse into the block's 5 below, and the row adds that 5.
        let latest = latest.map(|(&key, item)| {
            let now = now();
            let age = tr
                .times
                .get(&key.0)
                .map(|&at| ago(now.saturating_sub(at / 1000)));
            let age = age.unwrap_or_else(|| "time unknown".into());
            let line = mono(div(), 9., t).flex().items_center().gap(t.css(4.));
            let line = line
                .min_h(t.css(18.))
                .px(t.css(9.))
                .text_color(rgb(pal::DIMMER));
            let line = line.child("Latest activity").child(format!("· {age}"));
            let mark = Mark::Member(key);
            let at = record(&view.painted, move |p, b| p.marks.push((mark, b)));
            let margin = Kind::item(item).margin();
            let el = div().relative().child(line);
            let el = el.child(div().pt(t.css(margin)).child(self.body(key, item, true)));
            el.pb(t.css((margin - 5.).max(0.))).child(at)
        });
        let strip =
            (strip.is_some() || detail.is_some()).then(|| div().children(strip).children(detail));
        let el = div().flex().flex_col().gap(t.css(5.));
        el.children(strip).children(latest)
    }

    /// Linked, selectable markdown (U5's selection seam) in `ink`: an item's part, cached by part.
    fn md(&self, key: Key, part: usize, text: &str, ink: u32) -> Div {
        let (view, t) = (self.view, self.t);
        let linked = {
            let mut md = view.md.borrow_mut();
            let (mentions, cache) = &mut *md;
            let web = format!("{}/agents/{}", view.web, self.tr.agent);
            let link = || SharedString::from(markdown::link(text, mentions, &web));
            cache.entry((key, part)).or_insert_with(link).clone()
        };
        let id = self.id(&format!("md{part}"), key);
        let text = TextView::markdown(id, linked).selectable(true);
        let text = text.line_height(relative(1.55));
        // No actions are drawn; asking for them gives each fenced block an id, which its sideways
        // scroll keeps its offset under.
        let style = theme::prose(t).with_foreground(rgb(ink).into());
        let text = text.style(style).code_block_actions(|_, _, _| Empty);
        let text = text.on_link_click(|url, _, window, cx| match markdown::route(url) {
            Some(link) => window.dispatch_action(Box::new(OpenLink(link.into())), cx),
            None if url.starts_with("http://") || url.starts_with("https://") => cx.open_url(url),
            None => {}
        });
        sideways(div().w_full().max_w(t.css(900.)).child(text))
    }

    /// A click that opens or closes the item's `part` (`Fold`).
    fn fold(&self, key: Key, part: usize, kind: &str) -> Stateful<Div> {
        let fold = Fold(key, part);
        let el = div()
            .id(self.id(&format!("{kind}{part}"), key))
            .cursor_pointer();
        el.on_click(move |_, window, cx| window.dispatch_action(Box::new(fold), cx))
    }

    fn opened(&self, key: Key, part: usize) -> bool {
        self.view.open.borrow().contains(&(key, part))
    }

    /// One item as its own row, or as a `member` of an open run or the latest, where web shows its
    /// internal notes open.
    fn body(&self, key: Key, item: &Item, member: bool) -> AnyElement {
        let t = self.t;
        let open = self.opened(key, 0);
        let small = |text: String| dim(text).text_size(t.css(10.));
        let at = self.tr.times.get(&key.0).copied();
        let when = stamp(at.map(|ms| ms / 1000), now());
        let head = |operator| header(bits(item, &self.tr.agent), when.clone(), operator, t);
        match item {
            Item::Prompt(text) => {
                let body = self.md(key, 0, text, pal::INK);
                card(item, self.width, head(false), body, t)
            }
            Item::Delivery { text, operator, .. } => {
                // An operator's note in full, as web; another agent's cut, with web's toggle.
                let (shown, long) = if *operator {
                    (text.clone(), false)
                } else {
                    preview(text, open)
                };
                let hidden = text.lines().count().saturating_sub(shown.lines().count());
                let label = match (open, hidden) {
                    (true, _) => "Show less".to_string(),
                    (false, 0) => "Show full message".to_string(),
                    (false, 1) => "Show full message · 1 more line".to_string(),
                    (false, n) => format!("Show full message · {n} more lines"),
                };
                let more = long.then(|| {
                    let el = self.fold(key, 0, "more").mt(t.css(8.)).font_family(MONO_T);
                    el.text_size(t.css(10.))
                        .line_height(t.css(12.))
                        .text_color(rgb(pal::LINK))
                        .hover(|s| s.underline())
                        .child(label)
                });
                // The last paragraph's margin, under the text when nothing follows it.
                let body = self.md(key, usize::from(open), &shown, pal::INK);
                let body = div().when(!long, |b| b.pb(t.css(6.))).child(body);
                card(item, self.width, head(*operator), body.children(more), t)
            }
            // Only statuses and notes: web draws the parts bare, no rule or header, at margins 5.
            Item::Assistant(segs) if condense::marker(segs).is_some() => {
                self.segments(key, segs, member).mb_0().into_any_element()
            }
            Item::Assistant(segs) => answer(head(false), self.segments(key, segs, member), t),
            // The pill uncut, then what it stands for when that says more.
            Item::Chip { tone, label, text } => {
                let full = toned(div(), *tone, t).line_height(t.css(10.)).max_w_full();
                let more = (text != label && !text.is_empty()).then(|| small(text.clone()));
                let el = div().flex().flex_col().items_start().gap(t.css(2.));
                el.child(full.child(label.clone()))
                    .children(more)
                    .into_any_element()
            }
            Item::SystemChip(s) => system(s, when, t),
            Item::CompactDivider(s) => {
                let rule = || div().flex_1().h(px(1.)).bg(rgb(pal::PURPLE_RULE));
                let el = div().flex().items_center().gap(t.css(8.));
                el.text_size(t.css(10.))
                    .text_color(rgb(pal::PURPLE))
                    .child(rule())
                    .child(s.clone())
                    .child(time(when, t))
                    .child(rule())
                    .into_any_element()
            }
            Item::CompactSummary(text) => {
                let summary = expander(self.fold(key, 0, "fold"), open, t);
                let summary = summary
                    .text_color(rgb(pal::PURPLE))
                    .child("compaction summary")
                    .child(time(when, t).ml_auto());
                let body = open.then(|| md_detail(self.md(key, 1, text, pal::CODE_INK), t));
                div().child(summary).children(body).into_any_element()
            }
            // How long until the next entry (web), here the next item's.
            Item::Thinking(text) => {
                let next = self.tr.items.range((key.0 + 1, 0)..).next();
                let next = next.and_then(|(k, _)| self.tr.times.get(&k.0));
                let took = match (at, next) {
                    (Some(at), Some(&next)) => entries::took(next as i64 - at as i64),
                    _ => "duration unknown".into(),
                };
                entries::thinking(self.fold(key, 0, "fold"), open, text, took, when, t)
            }
            Item::Tool {
                name,
                summary,
                input,
                result,
            } => {
                let ids = [self.id("pre0", key), self.id("pre1", key)];
                let call = (name.as_str(), summary.as_str(), input.as_str());
                let fold = self.fold(key, 0, "fold");
                entries::tool(fold, open, call, result.as_ref(), at, ids, t)
            }
            Item::Error(text) => div()
                .text_color(rgb(pal::PORT))
                .child(format!("✗ {text}"))
                .into_any_element(),
        }
    }

    /// An answer's markdown, or its fenced parts in a column 4 apart (spec §1 "Assistant block"):
    /// text, a status chip, an internal note (held open in a run's `member`). A flex item keeps its
    /// paragraphs' margins, so fenced text sits 6 inside its own box.
    fn segments(&self, key: Key, segs: &[Seg], member: bool) -> Div {
        let t = self.t;
        let fenced = segs.iter().any(|s| !matches!(s, Seg::Text(_)));
        let parts = segs.iter().enumerate().filter_map(|(part, seg)| match seg {
            Seg::Text(s) if s.trim().is_empty() => None,
            Seg::Text(s) if !fenced => Some(self.md(key, part, s, pal::INK).pb(t.css(6.))),
            Seg::Text(s) => Some(div().w_full().py(t.css(6.)).child(self.md(
                key,
                part,
                s,
                pal::INK,
            ))),
            Seg::Status(s) => Some(self.status(key, part, s)),
            Seg::Internal(s) => Some(self.internal(key, part, s, member)),
        });
        let column = div().flex().flex_col().items_start();
        column
            .when(fenced, |c| c.gap(t.css(4.)).mb(t.css(5.)))
            .children(parts)
    }

    /// An inline status (spec §1 "Status chip"): cut at web's 26 characters, a click opens it in
    /// full, wrapping, and its `‹` closes it again; a short one is only the chip.
    fn status(&self, key: Key, part: usize, text: &str) -> Div {
        let t = self.t;
        let long = long(text);
        let open = long && self.opened(key, part);
        let pill = |el: Div| toned(el, Tone::Status, t);
        if !long {
            return pill(div()).line_height(t.css(10.)).child(text.to_string());
        }
        if !open {
            // A button: it takes the row's line height, as web's does.
            let el = toned(self.fold(key, part, "status"), Tone::Status, t);
            let el = el.hover(|s| s.border_color(rgb(pal::BLUE)));
            return div().child(el.child(cut(text, STATUS)));
        }
        let close = self.fold(key, part, "status").ml(t.css(5.)).px(t.css(3.));
        let close = close.rounded(t.css(4.)).child("‹");
        let el = pill(div())
            .max_w_full()
            .line_height(t.css(10.))
            .flex()
            .items_end();
        el.child(div().flex_1().child(text.to_string()))
            .child(close)
    }

    /// An internal note (spec §1 "Internal note"): a thinking pill, `› internal note · N words`; a
    /// click opens its body under it. `held` open in a run, as web's (`showSystem`).
    fn internal(&self, key: Key, part: usize, text: &str, held: bool) -> Div {
        let t = self.t;
        let open = held || self.opened(key, part);
        let words = text.split_whitespace().count();
        let unit = if words == 1 { "word" } else { "words" };
        // `⌄` raised to the turned `›`'s centre, as `entries::chevron`.
        let chevron = div().text_size(t.css(12.)).line_height(t.css(12.));
        let chevron = match open {
            true => chevron.relative().top(t.css(-3.5)).child("⌄"),
            false => chevron.child("›"),
        };
        let fold = match held {
            true => div().id(self.id(&format!("note{part}"), key)),
            false => self.fold(key, part, "note"),
        };
        let summary = toned(fold, Tone::Thinking, t);
        let summary = summary
            .flex()
            .items_center()
            .gap(t.css(4.))
            .line_height(t.css(10.));
        let summary = summary
            .child(chevron)
            .child(format!("internal note · {words} {unit}"));
        let body = self.md(key, part, text, pal::CODE_INK);
        let body = open.then(|| md_detail(body, t).mt(t.css(3.)).max_w(t.css(900.)));
        // Web's every `details` has margin-top 7; the internal note keeps it.
        div()
            .w_full()
            .mt(t.css(7.))
            .child(div().flex().child(summary))
            .children(body)
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

/// A run's pill (spec §1 "Activity strip and pills"): `Bash ×4`, 14 tall; a status cut to web's width,
/// on the button's taller line when cut; red when a merged tool failed.
fn pill(p: &Pill, t: TypeScale) -> Div {
    let text = match p.count {
        1 => p.label.clone(),
        n => format!("{} ×{n}", p.label),
    };
    let line = if p.tone == Tone::Status && long(&text) {
        13.5
    } else {
        10.
    };
    let text = match p.tone {
        Tone::Status => cut(&text, STATUS),
        _ => text,
    };
    let el = toned(div(), p.tone, t).flex_none().line_height(t.css(line));
    let el = el.when(p.error, |el| {
        el.border_color(rgb(pal::PORT)).text_color(rgb(pal::PORT))
    });
    el.child(text)
}

/// Whether a status is cut, so its chip opens (web's `statusChipTruncates`).
pub fn long(text: &str) -> bool {
    let flat = text.split_whitespace().collect::<Vec<_>>().join(" ");
    flat.chars().count() > STATUS
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
pub(super) fn sideways<E: InteractiveElement>(el: E) -> E {
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
