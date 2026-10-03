//! Type-to-capture (F7), web's `NoteCaptureChip`: text the pointer selects in an agent panel's
//! transcript gets a focused "＋ Add note" chip just under it. While it is up, no lens or zoom key fires
//! (their predicates exclude `Capture`): a printable key opens the popover with that key typed, `⏎` or
//! space opens it empty, `⌘⏎` sends the quote alone, `esc` cancels and a click on the chip saves the
//! quote as a note. In the popover (`Capture > Input`) `⏎` saves the quote and comment as a note on the
//! agent (`store::notes`), `⇧⏎` is a new line, `⌘⏎` sends them to the agent instead (web's quick send,
//! `composer::Step::Quick`) and `esc` cancels. A click anywhere else cancels, typed text and all, as
//! web's does, and so does focus leaving it any other way (Tab). Closing clears the selection, so the
//! keys come back.
//!
//! The chip sits 6 under the selection's last line at its left, as web's `capturePosition`; the kit
//! does not say where a selection's lines are, so that line is the one under the lower end of the drag
//! (its bottom taken as half a line under the pointer), and a selection over more than one line starts
//! at the text column.

use crate::store::composer::Step as SendStep;
use crate::store::notes::{Note, Step, transfer_text};
use crate::store::{Event, Store};
use crate::views::lens::{Focus, State, Ui};
use crate::views::notes::{self, stamp};
use crate::views::panel::Panel;
use crate::views::theme::{MONO_T, SANS_T, TypeScale, pal};
use crate::views::{Host, dim};
use gpui_kit::base::TextSelection;
use gpui_kit::component::input::{InputEvent, Textarea, TextareaState};
use gpui_kit::component::{Sizable as _, Size};
use gpui_kit::*;
use serde_json::json;

#[derive(Clone, Copy, Debug, PartialEq, Action)]
#[action(namespace = capture, no_json)]
pub enum Capture {
    /// `⏎` or space on the chip: the popover, empty.
    Open,
    /// `⏎` in the popover, or a click on the chip or "Add": a note.
    Save,
    /// `⌘⏎`: to the agent.
    Send,
    Cancel,
}

/// The chip's keys; the popover's bind on its Input.
pub const CHIP: &str = "Capture && !Input";
pub const EDITOR: &str = "Capture > Input";

pub struct View {
    chip: FocusHandle,
    input: Entity<TextareaState>,
    /// The popover's text, mirrored as it changes.
    pub(super) text: String,
    pub(super) draft: Option<Draft>,
    /// Text for the popover's box at the next render.
    load: Option<String>,
    /// Clear the transcript's selection at the next render.
    clear: bool,
    _blurs: [Subscription; 2],
}

pub(super) struct Draft {
    pub(super) agent: String,
    pub(super) quote: String,
    /// The chip's top left, in the window.
    pub(super) at: Point<Pixels>,
    pub(super) open: bool,
}

impl View {
    /// `agent`'s capture.
    pub fn new<H: Host>(agent: &str, window: &mut Window, cx: &mut Context<H>) -> Self {
        let input = cx.new(|cx| {
            let s = TextareaState::new(window, cx).auto_grow(2, 8);
            s.placeholder("Add a comment… ⌘↵ send · ↵ queue")
        });
        let agent = agent.to_string();
        let mine = agent.clone();
        cx.subscribe(
            &input,
            move |host: &mut H, state, event: &InputEvent, cx| {
                let panel = host.parts().1.panels.get_mut(&mine);
                if let (InputEvent::Change, Some(p)) = (event, panel) {
                    p.capture.text = state.read(cx).value().to_string();
                }
            },
        )
        .detach();
        // Focus leaving it (Tab to the composer, anything) cancels it and clears the selection: the
        // zoom's keys never come back while a selection is live. Its own chip-to-box move is not leaving.
        let left = move |host: &mut H, window: &mut Window, cx: &mut Context<H>| {
            let Some(v) = host
                .parts()
                .1
                .panels
                .get_mut(&agent)
                .map(|p| &mut p.capture)
            else {
                return;
            };
            if v.draft.is_some() && !v.focused(window, cx) {
                v.draft = None;
                TextSelection::clear(window, cx);
                cx.notify();
            }
        };
        let chip = cx.focus_handle();
        let boxed = input.read(cx).focus_handle(cx);
        let blurs = [
            cx.on_blur(&chip, window, left.clone()),
            cx.on_blur(&boxed, window, left),
        ];
        View {
            chip,
            input,
            text: String::new(),
            draft: None,
            load: None,
            clear: false,
            _blurs: blurs,
        }
    }

    /// What holds focus while it is up: the chip, or the popover's box.
    pub fn focus_handle(&self, cx: &App) -> FocusHandle {
        match self.draft.as_ref().is_some_and(|d| d.open) {
            true => self.input.read(cx).focus_handle(cx),
            false => self.chip.clone(),
        }
    }

    fn focused(&self, window: &Window, cx: &App) -> bool {
        self.chip.is_focused(window) || self.input.read(cx).focus_handle(cx).is_focused(window)
    }
}

/// Where the chip goes for a selection dragged from `from` to `to` (window points): see the module.
pub fn anchor(
    from: Point<Pixels>,
    to: Point<Pixels>,
    column: Pixels,
    line: Pixels,
) -> Point<Pixels> {
    let (top, bottom) = if from.y <= to.y {
        (from, to)
    } else {
        (to, from)
    };
    let left = match bottom.y - top.y < line * 0.5 {
        true => from.x.min(to.x),
        false => column,
    };
    point(left, bottom.y + line * 0.5 + px(6.))
}

/// The pointer let go on `agent`'s transcript (in panel `p`) with `text` selected, the chip at `at`:
/// offer it, with focus. A release with nothing selected leaves an open one as it is (a click in it).
pub fn offer(
    p: &mut Panel,
    agent: &str,
    text: &str,
    at: Point<Pixels>,
    w: &mut Window,
    cx: &mut App,
) {
    let quote = text.trim();
    if quote.is_empty() {
        return;
    }
    let (agent, quote) = (agent.to_string(), quote.to_string());
    p.capture.draft = Some(Draft {
        agent,
        quote,
        at,
        open: false,
    });
    w.focus(&p.capture.chip, cx);
}

/// Before a frame: load the zoomed agent's popover box, and clear the selection when asked.
pub fn sync(ui: &mut Ui, window: &mut Window, cx: &mut App) {
    let Some(v) = capture(ui) else {
        return;
    };
    if let Some(text) = v.load.take() {
        v.text = text.clone();
        // Typed, as web's `placeCaretAtEnd`: the caret after the first key.
        v.input.update(cx, |s, cx| {
            s.set_value("", window, cx);
            s.replace(text, window, cx)
        });
    }
    if std::mem::take(&mut v.clear) {
        TextSelection::clear(window, cx);
    }
}

/// The zoomed agent's capture.
fn capture(ui: &mut State) -> Option<&mut View> {
    ui.panel_mut().map(|p| &mut p.capture)
}

/// Open the popover with `first` typed (web's `expandWith`); it takes focus.
fn open(ui: &mut Ui, first: String) {
    let Some(v) = capture(ui) else {
        return;
    };
    if let Some(d) = v.draft.as_mut() {
        d.open = true;
        (v.load, v.clear) = (Some(first), true);
        ui.focus = Some(Focus::Capture);
    }
}

fn close(ui: &mut Ui) {
    if let Some(v) = capture(ui) {
        (v.draft, v.clear) = (None, true);
    }
    ui.focus = Some(Focus::Out);
}

/// Something to say on the zoomed agent's notes strip.
fn say(ui: &mut Ui, what: impl Into<String>) {
    if let Some(v) = notes::strip(ui) {
        v.say(what);
    }
}

pub fn act(store: &Store, ui: &mut Ui, key: &Capture) -> Vec<Event> {
    let Some(v) = capture(ui) else {
        return Vec::new();
    };
    let Some(d) = v.draft.as_ref() else {
        return Vec::new();
    };
    let (agent, quote) = (d.agent.clone(), d.quote.clone());
    let text = match d.open {
        true => v.text.trim().to_string(),
        false => String::new(),
    };
    let add = Step::Add {
        group: agent.clone(),
        text: text.clone(),
        quote: Some(quote.clone()),
        stamp: stamp(),
    };
    match key {
        Capture::Open => open(ui, String::new()),
        Capture::Cancel => close(ui),
        Capture::Send if store.can_send(&agent).is_ok() && !store.busy(&agent) => {
            let note = Note {
                id: String::new(),
                group: agent.clone(),
                text,
                quote: Some(quote),
                source: Some(json!({"kind": "transcript", "agent": agent})),
                created: 0,
                updated: 0,
            };
            let text = transfer_text(&note);
            // The box says how it went (sending…, or why not: the text is then in the draft).
            say(ui, format!("Sending a note to {agent}…"));
            close(ui);
            return vec![Event::Compose(SendStep::Quick { agent, text })];
        }
        // Saved. `⌘⏎` too while the agent cannot take a message now (native's rule: read-only, or a send
        // of its in flight, which web would not wait for; web adds the text to a read-only agent's
        // prompt instead): kept, and said. Refused (too long), the popover stays and the strip says why.
        Capture::Save | Capture::Send => {
            if let Some(why) = store.refusal(&add) {
                say(ui, why);
                return Vec::new();
            }
            match key {
                Capture::Send => say(ui, format!("{agent} cannot take a message now: saved.")),
                _ => say(ui, "Saved."),
            }
            close(ui);
            return vec![Event::Note(add)];
        }
    }
    Vec::new()
}

/// A click that dispatches `action` through the focused element, like a key.
fn click(el: Stateful<Div>, action: Capture) -> Stateful<Div> {
    el.cursor_pointer()
        .on_click(move |_, window, cx| window.dispatch_action(action.boxed_clone(), cx))
}

/// The chip or the popover for `agent`, over everything at the selection: web's `.note-capture-popover`
/// (measured, F7): system UI 13 on a 1.5 line, a #3a3c45 rule rounded 8 on the panel, shadowed.
pub fn render<H: Host>(
    ui: &Ui,
    agent: &str,
    t: TypeScale,
    cx: &mut Context<H>,
) -> Option<impl IntoElement> {
    let v = &ui.panels.get(agent)?.capture;
    let d = v.draft.as_ref()?;
    // Anywhere else: gone, typed text and all (web). Focus goes back to the panel only if it was here:
    // what was clicked may have taken it already, or take it next (the composer).
    let mine = agent.to_string();
    let out = cx.listener(move |h: &mut H, _: &MouseDownEvent, window, cx| {
        let Some(p) = h.parts().1.panels.get_mut(&mine) else {
            return;
        };
        if p.capture.focused(window, cx) {
            window.focus(&p.focus, cx);
        }
        p.capture.draft = None;
        cx.notify();
    });
    let shadow = BoxShadow {
        color: hsla(0., 0., 0., 0.4),
        offset: point(px(0.), t.css(8.)),
        blur_radius: t.css(24.),
        spread_radius: px(0.),
        inset: false,
    };
    let card = div()
        .id("capture")
        .key_context("Capture")
        .occlude()
        .on_mouse_down_out(out)
        .bg(rgb(pal::PANEL))
        .border_1()
        .border_color(rgb(pal::EDGE))
        .rounded(t.css(8.))
        .shadow(vec![shadow])
        .font_family(SANS_T)
        .text_size(t.css(13.))
        .line_height(relative(1.5))
        .text_color(rgb(pal::INK));
    let card = match d.open {
        false => card.child(chip(v, agent, t, cx)),
        true => card.w(t.css(332.)).p(t.css(8.)).child(popover(v, d, t)),
    };
    let at = anchored()
        .position(d.at)
        .snap_to_window_with_margin(t.css(8.));
    Some(deferred(at.child(card)).with_priority(1))
}

/// Web's `.note-capture-minimal-button`: padding 5 9, an accent rule rounded 5. A printable key opens
/// the popover with it typed (space and `⏎` are bound: empty).
fn chip<H: Host>(v: &View, agent: &str, t: TypeScale, cx: &mut Context<H>) -> Stateful<Div> {
    let agent = agent.to_string();
    let typed = cx.listener(move |h: &mut H, e: &KeyDownEvent, window, cx| {
        let k = &e.keystroke;
        let m = k.modifiers;
        let plain = !(m.platform || m.control || m.alt || m.function);
        let printable = |c: &&String| c.chars().count() == 1 && !c.trim().is_empty();
        let Some(first) = k.key_char.as_ref().filter(printable).filter(|_| plain) else {
            return;
        };
        let ui = h.parts().1;
        open(ui, first.clone());
        ui.focus = None;
        if let Some(p) = ui.panels.get(&agent) {
            window.focus(&p.capture.focus_handle(cx), cx);
        }
        cx.stop_propagation();
        cx.notify();
    });
    let el = div()
        .id("capture-chip")
        .track_focus(&v.chip)
        .on_key_down(typed)
        .px(t.css(9.))
        .py(t.css(5.))
        .border_1()
        .border_color(rgb(pal::BLUE))
        .rounded(t.css(5.))
        .child("＋ Add note");
    click(el, Capture::Save)
}

/// Web's expanded popover: the source and quote (mono 10, one line each, under a 2px rule), the box
/// (min 64, padding 8, an accent rule rounded 4, on the ground), and the footer: the agent it goes to,
/// `esc` and "Add ↵".
fn popover(v: &View, d: &Draft, t: TypeScale) -> Div {
    let one = |s: String| {
        div()
            .overflow_hidden()
            .whitespace_nowrap()
            .text_ellipsis()
            .child(s)
    };
    let quote = div()
        .pl(t.css(8.))
        .border_l(t.css(2.))
        .border_color(rgb(pal::EDGE))
        .font_family(MONO_T)
        .text_color(rgb(pal::DIMMER))
        .child(
            one(format!("Transcript: {}", d.agent))
                .text_size(t.css(8.33))
                .line_height(t.css(10.)),
        )
        .child(
            one(d.quote.split_whitespace().collect::<Vec<_>>().join(" "))
                .text_size(t.css(10.))
                .line_height(t.css(11.)),
        );
    // Web's box has no focus ring: the kit's input without its own chrome (it still pads the text by its
    // size, small: 8 across, 2 down), in web's rule and ground. Two rows and 6.5 more under them are web's
    // 64 (the kit centres one row in a taller minimum).
    let input = Textarea::new(&v.input)
        .with_size(Size::Small)
        .appearance(false);
    let input = input
        .font_family(SANS_T)
        .text_size(t.css(13.))
        .line_height(relative(1.5));
    let input = div()
        .mt(t.css(8.))
        .pt(t.css(8.) - px(2.))
        .pb(t.css(14.5) - px(2.))
        .border_1()
        .border_color(rgb(pal::BLUE))
        .rounded(t.css(4.))
        .bg(rgb(pal::GROUND))
        .child(input);
    let target = div()
        .px(t.css(8.))
        .py(t.css(4.))
        .border_1()
        .border_color(rgb(pal::EDGE))
        .rounded(t.css(5.))
        .bg(rgb(pal::WASH))
        .text_color(rgb(pal::LINK))
        .child(format!("→ {}", d.agent));
    let add = div()
        .id("capture-add")
        .px(t.css(12.))
        .py(t.css(4.))
        .border_1()
        .border_color(rgb(pal::BLUE))
        .rounded(t.css(5.))
        .bg(rgb(pal::BLUE))
        .text_color(rgb(pal::GROUND))
        .font_weight(FontWeight::SEMIBOLD)
        .child("Add ↵");
    let esc = dim("esc").font_family(MONO_T).text_size(t.css(9.));
    let confirm = div().ml_auto().flex().items_center().gap(t.css(8.));
    let footer = div().mt(t.css(8.)).flex().items_center().gap(t.css(8.));
    let footer = footer
        .child(target)
        .child(confirm.child(esc).child(click(add, Capture::Save)));
    div().child(quote).child(input).child(footer)
}
