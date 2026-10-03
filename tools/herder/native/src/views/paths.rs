//! The choice of where a clicked path lives (G3), web's file popover: when a path matches more than
//! one place and none of them is the obvious one (`store::transcript`'s pick rules), up to eight
//! candidates, each `root · path` (the root's last segment), under the press that clicked it. `↑` `↓`
//! move, `⏎` or a click opens one in VS Code, `esc` or a click anywhere else closes. It takes focus
//! when the choices land, so its keys (`Paths`) win over the zoom's; focus leaving it closes it. What is
//! offered and what a pick opens are the store's (`Transcript::choices`, `Step::Choose`).

use crate::store::Event;
use crate::store::Store;
use crate::store::transcript::{Choices, Step};
use crate::views::lens::{Focus, Ui};
use crate::views::theme::{MONO_T, TypeScale, pal};
use crate::views::{Host, dim, on};
use gpui_kit::prelude::FluentBuilder as _;
use gpui_kit::*;

#[derive(Clone, Copy, Debug, PartialEq, Action)]
#[action(namespace = paths, no_json)]
pub enum Paths {
    Up,
    Down,
    /// `⏎`: the candidate under the cursor.
    Open,
    /// A click on candidate `i`.
    Choose(usize),
    Close,
}

pub const PICKER: &str = "Paths";

pub struct View {
    pub(super) focus: FocusHandle,
    pub(super) cursor: usize,
    /// Choices were up at the last frame: new ones take focus once.
    up: bool,
    _blur: Subscription,
}

impl View {
    pub fn new<H: Host>(agent: &str, window: &mut Window, cx: &mut Context<H>) -> Self {
        let focus = cx.focus_handle();
        let agent = agent.to_string();
        let left = move |h: &mut H, _: &mut Window, cx: &mut Context<H>| {
            h.dispatch(choose(&agent, None), cx)
        };
        View {
            _blur: cx.on_blur(&focus, window, left),
            focus,
            cursor: 0,
            up: false,
        }
    }
}

fn choose(agent: &str, pick: Option<usize>) -> Event {
    let agent = agent.to_string();
    Event::Transcript(Step::Choose { agent, pick })
}

fn choices<'a>(store: &'a Store, agent: &str) -> Option<&'a Choices> {
    store.transcript.open.get(agent)?.choices.as_ref()
}

/// Before a frame: choices that just landed for the zoomed agent take focus, the cursor on the first.
pub fn sync(ui: &mut Ui, store: &Store, window: &mut Window, cx: &mut App) {
    let Some(agent) = ui.zoomed_agent().map(str::to_string) else {
        return;
    };
    let up = choices(store, &agent).is_some();
    let Some(v) = ui.panel_mut().map(|p| &mut p.paths) else {
        return;
    };
    if up && !std::mem::replace(&mut v.up, up) {
        v.cursor = 0;
        window.focus(&v.focus, cx);
    }
    v.up = up;
}

pub fn act(store: &Store, ui: &mut Ui, key: &Paths) -> Vec<Event> {
    let Some(agent) = ui.zoomed_agent().map(str::to_string) else {
        return Vec::new();
    };
    let n = choices(store, &agent).map_or(0, |c| c.candidates.len());
    let Some(v) = ui.panel_mut().map(|p| &mut p.paths) else {
        return Vec::new();
    };
    let pick = match *key {
        Paths::Up | Paths::Down => {
            let down = *key == Paths::Down;
            v.cursor = if down {
                v.cursor + 1
            } else {
                v.cursor.saturating_sub(1)
            };
            v.cursor = v.cursor.min(n.saturating_sub(1));
            return Vec::new();
        }
        Paths::Open => Some(v.cursor),
        Paths::Choose(i) => Some(i),
        Paths::Close => None,
    };
    ui.focus = Some(Focus::Out);
    vec![choose(&agent, pick)]
}

/// The root's last segment, as web's `rootLabel`.
fn label(root: &str) -> &str {
    let root = root.trim_end_matches('/');
    root.rsplit('/')
        .next()
        .filter(|s| !s.is_empty())
        .unwrap_or(root)
}

/// The picker over everything, under the press at `at` (web's `.selection-file-popover`: 420 wide,
/// a #3a3c45 rule rounded 7 on the panel, the mention in mono 10 above the candidates).
pub fn render<H: Host>(
    store: &Store,
    ui: &Ui,
    agent: &str,
    at: Point<Pixels>,
    t: TypeScale,
    cx: &mut Context<H>,
) -> Option<impl IntoElement> {
    let c = choices(store, agent)?;
    let v = &ui.panels.get(agent)?.paths;
    // A press anywhere else closes it, focus back on the panel if it was here.
    let mine = agent.to_string();
    let out = cx.listener(move |h: &mut H, _: &MouseDownEvent, window, cx| {
        let panel = h.parts().1.panels.get(&mine);
        if let Some(p) = panel.filter(|p| p.paths.focus.is_focused(window)) {
            window.focus(&p.focus, cx);
        }
        h.dispatch(choose(&mine, None), cx);
    });
    let row = |(i, c): (usize, &crate::api::Candidate)| {
        let open = move |_: &ClickEvent, window: &mut Window, cx: &mut App| {
            window.dispatch_action(Box::new(Paths::Choose(i)), cx)
        };
        let el = div().id(("path", i)).cursor_pointer().px(t.css(10.));
        el.py(t.css(4.))
            .flex()
            .gap(t.css(6.))
            .whitespace_nowrap()
            .overflow_hidden()
            .when(i == v.cursor, |el| el.bg(rgb(pal::SELECT)))
            .hover(|el| el.bg(rgb(pal::WASH)))
            .child(dim(format!("{} ·", label(&c.root))))
            .child(div().text_ellipsis().child(c.path.clone()))
            .on_click(open)
    };
    let shown = c.candidates.len();
    let more = (c.total > shown).then(|| {
        let s = format!("Showing {shown} of {} matches.", c.total);
        dim(s).px(t.css(10.)).py(t.css(4.))
    });
    let head = div().px(t.css(10.)).pt(t.css(6.)).pb(t.css(4.));
    let head = head
        .border_b_1()
        .border_color(rgb(pal::RULE))
        .text_size(t.css(10.))
        .text_color(rgb(pal::DIMMER))
        .child(format!("{} · ↑↓ ⏎ esc", c.query));
    let shadow = BoxShadow {
        color: hsla(0., 0., 0., 0.4),
        offset: point(px(0.), t.css(8.)),
        blur_radius: t.css(22.),
        spread_radius: px(0.),
        inset: false,
    };
    let card = div()
        .id("paths")
        .key_context(PICKER)
        .track_focus(&v.focus)
        .on_action(on(cx, |store, ui, p: &Paths| act(store, ui, p)))
        .occlude()
        .on_mouse_down_out(out)
        .w(t.css(420.))
        .pb(t.css(4.))
        .bg(rgb(pal::PANEL))
        .border_1()
        .border_color(rgb(pal::EDGE))
        .rounded(t.css(7.))
        .shadow(vec![shadow])
        .font_family(MONO_T)
        .text_size(t.css(11.))
        .text_color(rgb(pal::INK));
    let card = card
        .child(head)
        .children(c.candidates.iter().enumerate().map(row))
        .children(more);
    let at = anchored()
        .position(at + point(px(0.), t.css(18.)))
        .snap_to_window_with_margin(t.css(8.));
    Some(deferred(at.child(card)).with_priority(1))
}
