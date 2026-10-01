//! The zoom body: the zoomed agent's transcript in compact mode, over a context strip and above its
//! queued messages. A GPUI `list` anchored at the bottom follows the tail while it is at the bottom; its
//! rows mirror the store's `(offset, sub)` keys, so an older page is a splice at the top that keeps the
//! scroll position. Laying out any of the first rows asks for the page before, until the start.
//!
//! Assistant text renders with the kit's markdown (fenced blocks highlighted by tree-sitter), with
//! mentions and paths linked by `markdown::link`; a click on one dispatches `OpenLink`, which the zoom
//! shell handles: an agent in this space becomes its tab, any other opens as a preview tab (never a
//! member), and a path resolves and opens in VS Code. Reaching the bottom counts as viewing.

use crate::store::spaces::Move;
use crate::store::transcript::{Item, Key, Step, Transcript};
use crate::store::{Event, Store};
use crate::views::lens::{State, Ui};
use crate::views::markdown::{self, Mentions};
use crate::views::space::Zoom;
use crate::views::theme::{TypeScale, pal, type_scale};
use crate::views::{Host, dim};
use gpui_kit::component::text::TextView;
use gpui_kit::prelude::FluentBuilder as _;
use gpui_kit::*;
use std::cell::RefCell;
use std::collections::{HashMap, HashSet};

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

/// Rows above the viewport's top under which the page before is read.
const PREFETCH: usize = 60;
/// Linked markdown kept per row; dropped beyond this, as the rows scroll by.
const MD_CACHE: usize = 1500;

type Linked = HashMap<(Key, bool), SharedString>;

/// The list and what it currently mirrors. Interior mutability: views render from `&Ui`.
pub struct View {
    list: ListState,
    /// `(agent, generation)` the rows belong to, and their keys in order.
    rows: RefCell<((String, u64), Vec<Key>)>,
    open: RefCell<HashSet<Key>>,
    /// Linked text per row and fold state.
    md: RefCell<(Mentions, Linked)>,
    /// The agent and turn a "seen at the bottom" was last sent for.
    seen: RefCell<Option<(String, Option<u64>)>>,
    /// Herder web, where a mermaid diagram links to.
    web: String,
}

impl Default for View {
    fn default() -> Self {
        let list = ListState::new(0, ListAlignment::Bottom, px(1200.));
        list.set_follow_mode(FollowMode::Tail);
        View {
            list,
            rows: RefCell::default(),
            open: RefCell::default(),
            md: RefCell::default(),
            seen: RefCell::default(),
            web: String::new(),
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
    }

    /// Point the list at the transcript's rows: a new transcript resets it, rows before the first or
    /// after the last are splices, anything else (rare) a reset.
    fn sync(&self, t: &Transcript, store: &Store) {
        let mut rows = self.rows.borrow_mut();
        let tag = (t.agent.clone(), t.generation);
        let n = t.items.len();
        let (first, last) = (rows.1.first().copied(), rows.1.last().copied());
        let same = rows.0 == tag;
        let names = store.fleet.agents.keys().map(String::as_str);
        let mentions = Mentions::new(names);
        let mut md = self.md.borrow_mut();
        if md.0 != mentions || !same || md.1.len() > MD_CACHE {
            *md = (mentions, HashMap::new());
        }
        if same && n == rows.1.len() {
            return;
        }
        let keys: Vec<Key> = t.items.keys().copied().collect();
        let before = first.map_or(0, |f| t.items.range(..f).count());
        let after = last.map_or(0, |l| n - t.items.range(..=l).count());
        if same && before + after + rows.1.len() == n && !rows.1.is_empty() {
            let old = rows.1.len();
            self.list.splice(old..old, after);
            self.list.splice(0..0, before);
        } else {
            self.list.reset(n);
            self.list.set_follow_mode(FollowMode::Tail);
            self.open.borrow_mut().clear();
        }
        *rows = (tag, keys);
    }
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
    Vec::new()
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
        return body.p(t.px(24.)).child(dim("No agents in this space."));
    };
    let tr = store.transcript.open.as_ref().filter(|tr| tr.agent == name);
    let Some(tr) = tr else {
        // Morphing back to the lens, the transcript is already gone.
        return body.when(ui.zoom.is_some(), |b| b.p(t.px(24.)).child(dim("loading…")));
    };
    let view = &ui.transcript;
    view.sync(tr, store);
    // Read the page before while the viewport's top is near the first rows (or there are none).
    let top = view.list.logical_scroll_top().item_ix.min(tr.items.len());
    let more = tr.loaded() && !tr.at_start() && !tr.paging() && !tr.blocked();
    if more && top < PREFETCH {
        let older = Event::Transcript(Step::Older);
        cx.spawn(async move |host, cx| host.update(cx, |h, cx| h.dispatch(older, cx)))
            .detach();
    }
    // Reaching the bottom counts as viewing: mark a new turn seen once while following the tail.
    let turn = store.fleet.agents.get(name).and_then(|a| a.turn_end);
    let key = Some((name.to_string(), turn));
    if store.agent_needs_you(name) && view.list.is_following_tail() && *view.seen.borrow() != key {
        *view.seen.borrow_mut() = key;
        let (space, agent) = (zoom.space.clone(), Some(name.to_string()));
        let seen = Event::Lens(Move::View { space, agent });
        cx.spawn(async move |host, cx| host.update(cx, |h, cx| h.dispatch(seen, cx)))
            .detach();
    }
    let note = |s: &'static str| div().flex_1().p(t.px(24.)).child(dim(s)).into_any_element();
    let rows = if !tr.loaded() {
        note("loading transcript…")
    } else if tr.items.is_empty() {
        note("(nothing readable yet)")
    } else {
        let host = cx.weak_entity();
        list(view.list.clone(), move |ix, _, cx| match host.upgrade() {
            Some(host) => row::<H>(host.read(cx).view(), ix, t, host.downgrade()),
            None => div().into_any_element(),
        })
        .flex_1()
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
            dim(line).truncate().px(t.px(20.)).text_size(t.small)
        });
    let notice = tr.notice().map(|n| {
        let dismiss = cx
            .listener(|h, _: &ClickEvent, _, cx| h.dispatch(Event::Transcript(Step::Dismiss), cx));
        let el = div().id("notice").px(t.px(20.)).py(t.px(4.));
        el.text_size(t.small)
            .text_color(rgb(pal::AMBER))
            .child(format!("{n}  ✕"))
            .on_click(dismiss)
    });
    body.child(head)
        .child(rows)
        .children(queued)
        .children(notice)
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

/// One row of the list.
fn row<H: Host>(
    (store, ui): (&Store, &Ui),
    ix: usize,
    t: TypeScale,
    host: WeakEntity<H>,
) -> AnyElement {
    let view = &ui.transcript;
    let rows = view.rows.borrow();
    let tr = store
        .transcript
        .open
        .as_ref()
        .filter(|tr| tr.agent == rows.0.0);
    let found = tr
        .zip(rows.1.get(ix))
        .and_then(|(tr, &k)| Some((tr, k, tr.items.get(&k)?)));
    let Some((tr, key, item)) = found else {
        return div().into_any_element();
    };
    let open = view.open.borrow().contains(&key);
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
    let id = |kind: &str| {
        ElementId::Name(format!("{kind}-{}-{}-{}", tr.generation, key.0, key.1).into())
    };
    let el = div().w_full().max_w(t.px(980.)).px(t.px(20.)).pb(t.px(10.));
    let small = |text: String| dim(text).text_size(t.small);
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
        text.on_link_click(|url, _, window, cx| match markdown::route(url) {
            Some(link) => window.dispatch_action(Box::new(OpenLink(link.into())), cx),
            None if url.starts_with("http://") || url.starts_with("https://") => cx.open_url(url),
            None => {}
        })
    };
    let body = match item {
        Item::Prompt(text) => div()
            .bg(rgb(pal::ACCW))
            .border_l(t.px(3.))
            .border_color(rgb(pal::ACC))
            .rounded(t.px(4.))
            .px(t.px(10.))
            .py(t.px(6.))
            .child(md(text))
            .into_any_element(),
        Item::Delivery {
            sender,
            quiet: true,
            ..
        } => small(format!("· {sender}")).into_any_element(),
        Item::Delivery {
            sender,
            text,
            operator,
            ..
        } => {
            let from = format!("← {sender}{}", if *operator { " · operator" } else { "" });
            let (shown, long) = preview(text, open);
            let more = long.then(|| fold(if open { "less" } else { "more" }.into()));
            let el = div().child(small(from)).child(md(&shown));
            el.children(more).into_any_element()
        }
        Item::TaskNotification(s) => small(format!("⚑ {s}")).into_any_element(),
        Item::SystemChip(s) => small(format!("· {s}")).into_any_element(),
        Item::CompactDivider(s) => small(s.clone())
            .border_t_1()
            .border_color(rgb(pal::RULE))
            .pt(t.px(4.))
            .into_any_element(),
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
    };
    el.child(body).into_any_element()
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
