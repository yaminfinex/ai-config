//! What one transcript entry looks like, as web's compact view draws it (spec §1, A2): the header of
//! an answer or a card, the entry cards (another agent's message, an operator's note, the owner's
//! prompt), the fold summary and its body, the system chip and the queued box. The list, its rows and
//! what is open are `transcript`'s; these only build elements.

use crate::api::Queued;
use crate::store::condense;
use crate::store::transcript::{Item, Tone};
use crate::views::theme::{MONO_T, TypeScale, pal};
use crate::views::transcript::{PAD, ago};
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
    let chevron = div().text_size(t.css(14.)).line_height(t.css(14.));
    let chevron = chevron
        .text_color(rgb(pal::DIMMER))
        .child(if open { "⌄" } else { "›" });
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
    el.child(chevron)
}

/// An opened fold's body (web's `.entry-detail`): the code ground, ruled but for its top, its
/// markdown's paragraph margins inside its padding.
pub(super) fn detail(body: Div, t: TypeScale) -> Div {
    let el = div()
        .bg(rgb(pal::CODE))
        .border_1()
        .border_t_0()
        .border_color(rgb(pal::RULE));
    el.rounded_b(t.css(6.))
        .px(t.css(12.))
        .py(t.css(8. + 6.))
        .child(body)
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
        let age = condense::epoch(&q.sent_at).map(|at| waited(now.saturating_sub(at)));
        let age = age.unwrap_or_else(|| q.sent_at.clone());
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
