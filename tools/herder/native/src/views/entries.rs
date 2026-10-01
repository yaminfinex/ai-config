//! What one transcript entry looks like, as web's compact view draws it (spec §1, A2): the header of
//! an answer or a card, the entry cards (another agent's message, an operator's note, the owner's
//! prompt), the fold summary and its body, the system chip, the queued box, and a run member's status
//! dot, duration and detail sections (spec §1 "Expanded run details", A3). The list, its rows and what
//! is open are `transcript`'s; these only build elements.

use crate::api::Queued;
use crate::store::condense;
use crate::store::transcript::{Item, Tone, ToolResult};
use crate::views::theme::{MONO_T, TypeScale, pal};
use crate::views::transcript::{PAD, ago, sideways};
use gpui_kit::prelude::FluentBuilder as _;
use gpui_kit::*;
use std::time::{SystemTime, UNIX_EPOCH};

/// Seconds since the epoch, for ages.
pub(super) fn now() -> u64 {
    let now = SystemTime::now().duration_since(UNIX_EPOCH);
    now.map_or(0, |d| d.as_secs())
}

/// An entry's time as web's headers give it: `8h ago`, else `time unknown`.
pub fn stamp(at: Option<u64>, now: u64) -> String {
    match at {
        Some(at) => format!("{} ago", ago(now.saturating_sub(at))),
        None => "time unknown".into(),
    }
}

/// A queued message's age as web's queued box gives it, in whole seconds and minutes; past an hour,
/// hours (web: the local clock time).
pub fn waited(secs: u64) -> String {
    match secs {
        0..60 => format!("{secs}s ago"),
        60..3600 => format!("{}m ago", secs / 60),
        _ => format!("{}h ago", secs / 3600),
    }
}

/// How long a message has been queued, from hcom's `sent_at`; as sent when it does not parse (web's
/// fallback).
pub fn queued_age(sent_at: &str, now: u64) -> String {
    let at = condense::epoch(sent_at);
    at.map_or_else(|| sent_at.to_string(), |at| waited(now.saturating_sub(at)))
}

/// How long a tool or a thinking took, as web's `formatDuration`: `883ms`, `2.7s`, `14s`, `3m 5s`;
/// `—` when negative. Halves round up, as `toFixed`.
pub fn took(ms: i64) -> String {
    match ms {
        ..0 => "—".into(),
        0..1000 => format!("{ms}ms"),
        1000..10_000 => {
            let tenths = (ms + 50) / 100;
            format!("{}.{}s", tenths / 10, tenths % 10)
        }
        10_000..60_000 => format!("{}s", (ms + 500) / 1000),
        _ => format!("{}m {}s", ms / 60_000, (ms % 60_000 + 500) / 1000),
    }
}

/// A tool's status dot (web's `.tool-status`): green done, red failed, blue still running.
pub fn dot(result: Option<&ToolResult>) -> u32 {
    match result {
        None => pal::BLUE,
        Some(r) if r.error => pal::RED,
        Some(_) => pal::OPERATOR,
    }
}

/// A run's thinking (web's `.thinking-entry`, spec §1 "Expanded run details"): `thinking · 2.7s` in
/// purple italics and its time; open, its text in the thinking ink, or web's note when redacted.
pub(super) fn thinking(
    fold: Stateful<Div>,
    open: bool,
    text: &str,
    took: String,
    when: String,
    t: TypeScale,
) -> AnyElement {
    let what = div().italic().text_color(rgb(pal::PURPLE));
    let summary = expander(fold, open, t)
        .child(what.child(format!("thinking · {took}")))
        .child(time(when, t).ml_auto());
    let text = match text.trim() {
        "" => "Thinking content unavailable.",
        text => text,
    };
    let body = div().italic().text_color(rgb(pal::THINKING_INK));
    let body = open.then(|| detail(body.child(text.to_string()), t));
    div().child(summary).children(body).into_any_element()
}

/// A run's tool (web's `.tool-entry`): its status dot, name, summary cut to the row, duration and
/// time (the result's, once in); open, its input (pretty JSON) and output, each scrolling sideways
/// under its id.
pub(super) fn tool(
    fold: Stateful<Div>,
    open: bool,
    (name, summary, input): (&str, &str, &str),
    result: Option<&ToolResult>,
    at: Option<u64>,
    [input_id, output_id]: [ElementId; 2],
    t: TypeScale,
) -> AnyElement {
    let dot = div().flex_none().size(t.css(7.)).rounded_full();
    let dot = dot.bg(rgb(self::dot(result)));
    let label = if name.is_empty() {
        "unknown tool"
    } else {
        name
    };
    let name = mono(div(), 11., t).flex_none();
    let name = name
        .font_weight(FontWeight::SEMIBOLD)
        .text_color(rgb(pal::INK));
    let what = mono(div(), 11., t).min_w_0().truncate();
    let done = result.and_then(|r| r.at);
    let lasted = match (done, at) {
        (Some(done), Some(at)) => took(done as i64 - at as i64),
        (Some(_), None) => "—".into(),
        (None, _) => "running · no result yet".into(),
    };
    let when = stamp(done.or(at).map(|ms| ms / 1000), now());
    let summary = expander(fold, open, t)
        .child(dot)
        .child(name.child(label.to_string()))
        .child(what.child(summary.to_string()))
        .child(time(lasted, t).ml_auto())
        .child(time(when, t));
    let body = open.then(|| {
        let el = div().child(heading("Input", true, t));
        let el = el.child(pre(input_id, input, t));
        let el = el.when_some(result, |el, r| {
            let output = (!r.text.is_empty()).then(|| pre(output_id, &r.text, t));
            el.child(heading("Output", false, t)).children(output)
        });
        detail(el, t)
    });
    div().child(summary).children(body).into_any_element()
}

/// The entry cards (spec §1 "Operator / prompt card and delivery card"): another agent's message, an
/// operator's note, the owner's prompt.
#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Card {
    Hcom,
    Operator,
    Human,
}

impl Card {
    pub fn of(item: &Item) -> Option<Card> {
        match item {
            Item::Delivery { operator: true, .. } => Some(Card::Operator),
            Item::Delivery { .. } => Some(Card::Hcom),
            Item::Prompt(_) => Some(Card::Human),
            _ => None,
        }
    }
}

/// A header's parts before its time, left to right.
#[derive(Clone, Debug, PartialEq)]
pub enum Bit {
    Name(String),
    Operator,
    To(String),
    Intent(String),
    Id(String),
    Thread(String),
}

/// What an item's header says, as web's: an answer its agent, a prompt the owner, a delivery its
/// sender, the operator badge, its recipient, intent, `#id` and thread.
pub fn bits(item: &Item, agent: &str) -> Vec<Bit> {
    let or = |s: &str, none: &str| if s.is_empty() { none } else { s }.to_string();
    match item {
        Item::Assistant(_) => vec![Bit::Name(agent.into())],
        Item::Prompt(_) => vec![Bit::Name("owner (terminal)".into())],
        Item::Delivery {
            sender,
            operator,
            head,
            ..
        } => {
            let mut bits = vec![Bit::Name(or(sender, "unknown sender"))];
            bits.extend(operator.then_some(Bit::Operator));
            bits.push(Bit::To(or(&head.to, "unknown recipient")));
            let given =
                |s: &String, bit: fn(String) -> Bit| (!s.is_empty()).then(|| bit(s.clone()));
            bits.extend(given(&head.intent, Bit::Intent));
            bits.extend(given(&head.id, Bit::Id));
            bits.extend(given(&head.thread, Bit::Thread));
            bits
        }
        _ => Vec::new(),
    }
}

/// SF Mono at `size` on its normal line (web's `font: <size> var(--mono)`; Chromium measures 9 → 10,
/// 11 → 13).
pub(super) fn mono<E: Styled>(el: E, size: f32, t: TypeScale) -> E {
    let line = if size < 10. { size + 1. } else { size + 2. };
    el.font_family(MONO_T)
        .text_size(t.css(size))
        .line_height(t.css(line))
}

pub(super) fn time(when: String, t: TypeScale) -> Div {
    let el = mono(div(), 9., t).flex_none().whitespace_nowrap();
    el.text_color(rgb(pal::DIMMER)).child(when)
}

/// A header badge (`.operator-badge`, `.intent-badge`, `.thread-chip`).
pub(super) fn badge(
    (border, ground, ink): (u32, u32, u32),
    pad: f32,
    text: String,
    t: TypeScale,
) -> Div {
    let el = mono(div(), 9., t).flex_none().px(t.css(pad)).border_1();
    el.border_color(rgb(border))
        .bg(rgb(ground))
        .text_color(rgb(ink))
        .rounded(t.css(4.))
        .child(text)
}

/// A header line (spec §1 "Assistant block", "Operator / prompt card"): the name in mono 600 (an
/// operator's green), the badges, the time at the right; 6 above what follows.
pub(super) fn header(bits: Vec<Bit>, when: String, operator: bool, t: TypeScale) -> Div {
    let name = if operator {
        pal::OPERATOR_NAME
    } else {
        pal::INK
    };
    let part = |bit: Bit| match bit {
        Bit::Name(s) => {
            let el = mono(div(), 11., t).min_w_0().truncate();
            el.font_weight(FontWeight::SEMIBOLD)
                .text_color(rgb(name))
                .child(s)
        }
        Bit::Operator => badge(pal::BADGE_OPERATOR, 5., "web operator".into(), t),
        Bit::To(s) => div().flex_none().child(format!("→ {s}")),
        Bit::Intent(s) if s == "request" => badge(pal::BADGE_REQUEST, 6., s, t),
        Bit::Intent(s) => badge(pal::BADGE_INTENT, 6., s, t),
        Bit::Id(s) => time(format!("#{s}"), t),
        Bit::Thread(s) => badge(pal::BADGE_THREAD, 6., s, t),
    };
    let el = div().flex().items_center().gap(t.css(8.)).mb(t.css(6.));
    el.text_size(t.css(11.))
        .line_height(relative(1.5))
        .text_color(rgb(pal::SLATE))
        .children(bits.into_iter().map(part))
        .child(time(when, t).ml_auto())
}

/// An answer (spec §1 "Assistant block"): a 3px rule on the left, the header, then the text with its
/// last paragraph's margin.
pub(super) fn answer(head: Div, body: Div, t: TypeScale) -> AnyElement {
    let el = div().border_l(t.css(3.)).border_color(rgb(pal::EDGE));
    el.pl(t.css(12.))
        .py(t.css(3.))
        .child(head)
        .child(body)
        .into_any_element()
}

/// One entry card, for every kind (spec §1 "Operator / prompt card and delivery card"): radius 8, a
/// 1px rule, padding 9 12, and a 3px edge, the owner's on the right; an operator's and the owner's are
/// tinted, indented and narrower. GPUI draws one border colour, so the edge is the outer box's
/// ground showing past the inner one (web's coloured border side, nearest). An item that is no card
/// draws as another agent's message.
pub(super) fn card(item: &Item, width: Pixels, head: Div, body: Div, t: TypeScale) -> AnyElement {
    let look = Card::of(item).unwrap_or(Card::Hcom);
    let (edge, ground, indent) = match look {
        Card::Hcom => (pal::BLUE, pal::PANEL, None),
        Card::Operator => (pal::OPERATOR, pal::OPERATOR_GROUND, Some((0.06, 54., 820.))),
        Card::Human => (pal::OPERATOR, pal::HUMAN_GROUND, Some((0.1, 90., 790.))),
    };
    let inner = div()
        .flex_1()
        .min_w_0()
        .border_1()
        .border_color(rgb(pal::RULE));
    let inner = inner.bg(rgb(ground)).px(t.css(12.)).py(t.css(9.));
    let inner = inner.child(head).child(body);
    let (outer, inner_r) = (t.css(8.), t.css(5.));
    let el = div().flex().bg(rgb(edge)).rounded(outer);
    // Web's margin-left min(6%, 54px) and max-width, of the row's width.
    let el = el.when_some(indent, |el, (share, most, max)| {
        el.ml(t.css(most).min(width * share)).max_w(t.css(max))
    });
    let el = match look {
        Card::Human => el
            .pr(t.css(3.))
            .child(inner.border_r_0().rounded_l(outer).rounded_r(inner_r)),
        _ => el
            .pl(t.css(3.))
            .child(inner.border_l_0().rounded_r(outer).rounded_l(inner_r)),
    };
    el.into_any_element()
}

/// A model switch (spec §1 "Other kinds"): a fitted chip in the status tone, `summary · 3h ago`.
pub(super) fn system(s: &str, when: String, t: TypeScale) -> AnyElement {
    let (border, ground, ink) = pal::chip(Tone::Status);
    let el = div()
        .flex()
        .items_center()
        .max_w(t.css(900.))
        .px(t.css(9.))
        .py(t.css(2.));
    let el = el
        .border_1()
        .border_color(rgb(border))
        .bg(rgb(ground))
        .text_color(rgb(ink));
    let el = el
        .rounded(t.css(10.))
        .text_size(t.css(10.))
        .line_height(relative(1.5));
    let el = el.child(format!("{s} · ")).child(time(when, t));
    div().flex().child(el).into_any_element()
}

/// A fold's summary (web's `.entry-expander`): a chevron, then the caller's children, 8 apart; a
/// ruled panel under the pointer or while open.
pub(super) fn expander(el: Stateful<Div>, open: bool, t: TypeScale) -> Stateful<Div> {
    let el = el.flex().items_center().gap(t.css(8.)).min_h(t.css(27.));
    let el = el
        .px(t.css(9.))
        .py(t.css(4.))
        .border_1()
        .text_size(t.css(10.));
    let el = el.line_height(relative(1.5)).rounded(t.css(6.));
    let lit = |el: Stateful<Div>| el.border_color(rgb(pal::RULE)).bg(rgb(pal::PANEL));
    let el = match open {
        true => lit(el).rounded_b(px(0.)),
        false => el
            .border_color(transparent_black())
            .hover(|s| s.border_color(rgb(pal::RULE)).bg(rgb(pal::PANEL))),
    };
    el.child(chevron(open, t))
}

/// A fold's chevron (web's `summary::before`): `›` 14 in the dimmer ink, `⌄` open (web turns it), on
/// the `›`'s width (6.1, web's box) so that what follows stays put, and raised 4 to the turned `›`'s centre (px.md).
pub(super) fn chevron(open: bool, t: TypeScale) -> Div {
    let el = div().flex_none().flex().justify_center().w(t.css(6.1));
    let el = el.text_size(t.css(14.)).line_height(t.css(14.));
    let el = el.text_color(rgb(pal::DIMMER));
    match open {
        true => el.relative().top(t.css(-4.)).child("⌄"),
        false => el.child("›"),
    }
}

/// An opened fold's body (web's `.entry-detail`): the code ground and ink, ruled but for its top.
/// Markdown keeps its paragraphs' margins inside (`md_detail`).
pub(super) fn detail(body: Div, t: TypeScale) -> Div {
    let el = div()
        .bg(rgb(pal::CODE))
        .text_color(rgb(pal::CODE_INK))
        .border_1()
        .border_t_0()
        .border_color(rgb(pal::RULE));
    el.rounded_b(t.css(6.))
        .px(t.css(12.))
        .py(t.css(8.))
        .child(body)
}

/// `detail` around markdown, whose first and last paragraphs keep their 6 margins.
pub(super) fn md_detail(body: Div, t: TypeScale) -> Div {
    detail(body.py(t.css(6.)), t)
}

/// A detail's section heading (`.entry-detail h4`): mono 9 capitals, 3 above its text and 8 above
/// any but the first (no letter spacing in GPUI).
fn heading(text: &str, first: bool, t: TypeScale) -> Div {
    let el = mono(div(), 9., t).text_color(rgb(pal::SLATE)).mb(t.css(3.));
    el.when(!first, |el| el.mt(t.css(8.)))
        .child(text.to_uppercase())
}

/// A detail's preformatted text (`.entry-detail pre`): mono 11, its lines unwrapped, scrolling
/// sideways, 8 under it.
fn pre(id: ElementId, text: &str, t: TypeScale) -> Stateful<Div> {
    let lines = mono(div(), 11., t)
        .whitespace_nowrap()
        .child(text.to_string());
    let el = div().id(id).w_full().overflow_x_scroll().mb(t.css(8.));
    sideways(el.child(lines))
}

/// A pill's border, ground, ink and type in `tone` (web's `.activity-pill`).
pub(super) fn toned<E: Styled>(el: E, tone: Tone, t: TypeScale) -> E {
    let (border, ground, ink) = pal::chip(tone);
    el.px(t.css(7.))
        .py(t.css(1.))
        .border_1()
        .border_color(rgb(border))
        .bg(rgb(ground))
        .text_color(rgb(ink))
        .rounded(t.css(10.))
        .font_family(MONO_T)
        .text_size(t.css(9.))
}

/// Messages waiting for the agent's next turn (spec §1 "Queued box"): an amber box under the
/// transcript, a row each; an operator's on the green.
pub(super) fn queued(messages: &[Queued], t: TypeScale) -> Option<Div> {
    if messages.is_empty() {
        return None;
    }
    let now = now();
    let title = div().font_weight(FontWeight::BOLD).child("QUEUED");
    let sub = div().text_color(rgb(pal::QUEUE_SUB));
    let head = div()
        .flex()
        .items_baseline()
        .gap(t.css(8.))
        .px(t.css(3.))
        .pb(t.css(5.));
    let head = head.text_color(rgb(pal::QUEUE_TITLE)).child(title);
    let head = head.child(sub.child("waiting for the agent’s next turn"));
    let row = |q: &Queued| {
        let age = queued_age(&q.sent_at, now);
        let meta = div()
            .flex()
            .items_center()
            .gap(t.css(7.))
            .text_color(rgb(pal::SLATE));
        let meta = meta.child(div().text_color(rgb(pal::INK)).child(q.sender.clone()));
        let meta = meta.when(q.operator, |m| {
            m.child(badge(pal::BADGE_OPERATOR, 5., "operator".into(), t))
        });
        let intent = match q.intent.as_str() {
            "request" => pal::BADGE_REQUEST,
            _ => pal::BADGE_INTENT,
        };
        let meta = meta.when(!q.intent.is_empty(), |m| {
            m.child(badge(intent, 6., q.intent.clone(), t))
        });
        let id = div()
            .text_color(rgb(pal::DIMMER))
            .child(format!("#{}", q.id));
        let meta = meta
            .child(id)
            .child(div().ml_auto().text_color(rgb(pal::DIMMER)).child(age));
        let preview = div()
            .mt(t.css(3.))
            .text_color(rgb(pal::INK))
            .child(q.preview.clone());
        let ground = if q.operator {
            pal::QUEUE_OPERATOR
        } else {
            pal::QUEUE_ROW
        };
        let inner = div().flex_1().min_w_0().px(t.css(8.)).py(t.css(6.));
        let inner = inner.bg(rgb(ground)).child(meta).child(preview);
        // The operator's edge as `card`'s.
        let el = div().flex().border_t_1().border_color(rgb(pal::QUEUE_RULE));
        el.when(q.operator, |el| el.pl(t.css(3.)).bg(rgb(pal::OPERATOR)))
            .child(inner)
    };
    let el = div()
        .max_w(t.css(900.))
        .mb(t.css(7.))
        .p(t.css(7.))
        .border_1();
    let el = el.border_color(rgb(pal::QUEUE_EDGE)).rounded(t.css(7.));
    let el = el
        .bg(rgb(pal::QUEUE_GROUND))
        .font_family(MONO_T)
        .text_size(t.css(10.));
    let el = el
        .line_height(t.css(12.))
        .child(head)
        .children(messages.iter().map(row));
    Some(div().px(t.css(PAD)).child(el))
}
