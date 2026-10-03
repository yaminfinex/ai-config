//! The composer (U4): a growing box under an agent panel's transcript that sends to its agent through
//! herder's message endpoint. The text is the agent's draft in the store: every edit is dispatched,
//! and the box is set from the store whenever the two differ (a send landed, a hand-off).
//! It is disabled while a send is in flight and read-only, with the reason, when the store says the
//! agent cannot be written to. A send that failed keeps its text and says why under the box.
//!
//! Keys (ARCHITECTURE §4): `/` or `r` in the zoom focus it; in the box (`Composer > Input`, so no other
//! input sends) `cmd-enter` sends, `cmd-shift-enter` sends and, once it lands, files the agent back
//! into the lens (seen), `alt-enter` keeps the draft as a note instead (U5), `escape` leaves the box,
//! and `up` with the box empty or the caret at its start enters the notes list (F6).
//! The wording under the box is here, the states in `store::composer`.

use crate::store::composer::{Failure, ReadOnly, Sending, Step};
use crate::store::notes::Step as NoteStep;
use crate::store::{Attribution, Event, Store};
use crate::views::lens::{Focus, Ui};
use crate::views::space::{self, Zoomed};
use crate::views::theme::{SANS_T, TypeScale, pal};
use crate::views::{Host, on, settle_later};
use crate::views::{dock, notes_list};
use gpui_kit::component::input::{InputEvent, Textarea, TextareaState};
use gpui_kit::component::{Sizable as _, Size};
use gpui_kit::*;

#[derive(Clone, Copy, Debug, PartialEq, Action)]
#[action(namespace = composer, no_json)]
pub enum Compose {
    Focus,
    Send,
    FileBack,
    Leave,
    /// `alt-enter`: keep the draft as a note on the agent instead (U5).
    Queue,
}

/// Where the box's chords bind: its own Input, not any Input in the zoom.
pub const BOX: &str = "Composer > Input";
/// Rows the box grows to before it scrolls.
const ROWS: (usize, usize) = (1, 8);
const HINT: &str = "⌘⏎ send · ⌘⇧⏎ send and back to the lens · ⌥⏎ keep as a note · esc leave";

pub struct View {
    pub(super) state: Entity<TextareaState>,
}

impl View {
    /// The box for `agent`'s draft.
    pub fn new<H: Host>(agent: &str, window: &mut Window, cx: &mut Context<H>) -> Self {
        let state = cx.new(|cx| {
            let s = TextareaState::new(window, cx).auto_grow(ROWS.0, ROWS.1);
            s.placeholder("Message the agent…")
        });
        let agent = agent.to_string();
        cx.subscribe(
            &state,
            move |host: &mut H, state, event: &InputEvent, cx| {
                if let InputEvent::Change = event {
                    let (agent, text) = (agent.clone(), state.read(cx).value().to_string());
                    host.dispatch(Event::Compose(Step::Edit { agent, text }), cx);
                }
            },
        )
        .detach();
        View { state }
    }

    pub fn focus_handle(&self, cx: &App) -> FocusHandle {
        self.state.read(cx).focus_handle(cx)
    }
}

/// Before a frame is drawn: each shown panel's box shows its agent's draft. A landed file-back hands
/// focus to the lens wherever it was in the departing zoom.
pub fn sync(ui: &mut Ui, store: &Store, window: &mut Window, cx: &mut App) {
    if ui.focus.take() == Some(Focus::Out) {
        window.focus(ui.focus_target(), cx);
    }
    for (agent, p) in ui.panels.iter().filter(|(_, p)| p.shown) {
        let draft = store.prefs.drafts.get(agent).map_or("", String::as_str);
        let state = &p.composer.state;
        if state.read(cx).value() != draft {
            let draft = draft.to_string();
            state.update(cx, |s, cx| s.set_value(draft, window, cx));
        }
    }
}

/// A composer key: `Focus` from the zoom, the rest from the box itself. A send from a preview tab pins
/// it (DK2).
pub fn act(store: &Store, ui: &mut Ui, key: Compose) -> Vec<Event> {
    let Some(agent) = ui.zoomed_agent().map(String::from) else {
        return Vec::new();
    };
    let send = |file_back| {
        let send = Event::Compose(Step::Send {
            agent: agent.clone(),
            file_back,
        });
        let mut out = dock::pin_tab(store, ui, &agent);
        out.push(send);
        out
    };
    match key {
        Compose::Focus => ui.focus = Some(Focus::Box),
        Compose::Leave => ui.focus = Some(Focus::Out),
        Compose::Send => return send(false),
        // The zoom stays, "sending", until it lands (`Effect::FiledBack`); a failure stays to say why.
        Compose::FileBack => return send(true),
        Compose::Queue => {
            let stamp = crate::views::notes::stamp();
            return vec![Event::Note(NoteStep::Queue { agent, stamp })];
        }
    }
    Vec::new()
}

/// A filed-back send landed (`Effect::FiledBack`): leave for the lens if the zoom is still on `agent`.
pub fn filed_back<H: Host>(
    store: &Store,
    ui: &mut Ui,
    agent: &str,
    cx: &mut Context<H>,
) -> Vec<Event> {
    if ui.zoomed_agent() != Some(agent) {
        return Vec::new();
    }
    let before = ui.anim.as_ref().map(space::Anim::seq);
    let out = space::act(store, ui, Zoomed::Out);
    settle_later(ui, before, cx);
    ui.focus = Some(Focus::Out);
    out
}

/// The box and its status line, under the transcript.
pub fn render<H: Host>(
    store: &Store,
    ui: &Ui,
    agent: &str,
    t: TypeScale,
    cx: &mut Context<H>,
) -> Div {
    let (line_text, color) = status(store, agent);
    let writable = store.can_send(agent).is_ok() && !store.busy(agent);
    // Web's `.send-box textarea` (measured, G1): system UI 13 on a 1.45 line, padding 7 9, a #3a3c45
    // rule rounded 5, on the ground; at least 36 tall, at most 160.
    let Some(state) = ui.panels.get(agent).map(|p| &p.composer.state) else {
        return div();
    };
    let input = Textarea::new(state).disabled(!writable);
    let input = input
        .font_family(SANS_T)
        .text_size(t.css(13.))
        .line_height(relative(1.45));
    // The kit pads the text inside by its size (small: 8 across, 2 down); the rest of web's goes round it.
    let input = input.with_size(Size::Small);
    let input = input.px(t.css(9.) - px(8.)).py(t.css(7.) - px(2.));
    let input = input.min_h(t.css(36.)).max_h(t.css(160.));
    let input = input
        .rounded(t.css(5.))
        .border_color(rgb(pal::EDGE))
        .bg(rgb(pal::GROUND));
    let (state, notes) = (state.clone(), store.notes_of(agent).next().is_some());
    let enter = on(cx, |store, ui, _: &notes_list::Up| {
        notes_list::enter(store, ui)
    });
    let up = move |a: &notes_list::Up, window: &mut Window, cx: &mut App| {
        let s = state.read(cx);
        match notes && (s.value().is_empty() || s.selected_range().end == 0) {
            true => enter(a, window, cx),
            false => cx.propagate(),
        }
    };
    let input = div()
        .key_context("Composer")
        .on_action(on(cx, |store, ui, c: &Compose| act(store, ui, *c)))
        .on_action(up)
        .child(input);
    // Web's `.send-box`: padding 7 14 5 on the panel under a rule; its footer 4 below the box, the keys
    // in system UI 9 (here, or why it is read-only, or how a send went).
    let line = div()
        .font_family(SANS_T)
        .text_size(t.css(9.))
        .line_height(t.css(13.5));
    div()
        .flex_none()
        .flex()
        .flex_col()
        .gap(t.css(4.))
        .px(t.css(14.))
        .pt(t.css(7.))
        .pb(t.css(5.))
        .bg(rgb(pal::PANEL))
        .border_t_1()
        .border_color(rgb(pal::RULE))
        .child(input)
        .child(line.text_color(rgb(color)).child(line_text))
}

/// The line under the box and its colour: why it is read-only, "sending…", "saving the notes…" (a U5
/// transfer), the last failure or the keys.
pub(super) fn status(store: &Store, agent: &str) -> (String, u32) {
    match (store.can_send(agent), store.sends.get(agent)) {
        (Err(why), _) => (say_read_only(&store.viewer, why), pal::AMBER),
        (Ok(()), _) if store.transfers.contains_key(agent) => {
            ("saving the notes…".to_string(), pal::SLATE)
        }
        (Ok(()), Some(Sending::InFlight { .. })) => ("sending…".to_string(), pal::SLATE),
        (Ok(()), Some(Sending::Failed(failure))) => (say_failure(failure), pal::AMBER),
        (Ok(()), None) => (HINT.to_string(), pal::DIMMER),
    }
}

fn say_read_only(viewer: &Attribution, why: ReadOnly) -> String {
    let refusal = match viewer {
        Attribution::Refused(Some(r)) => Some(r),
        _ => None,
    };
    match (why, refusal) {
        (ReadOnly::Refused, Some(r)) if r.error == "sender refused" => format!(
            "read-only · sender collision: this Mac's sender name is taken ({})",
            r.detail
        ),
        (ReadOnly::Refused, Some(r)) => format!("read-only · attribution required: {}", r.detail),
        (ReadOnly::Refused, None) => "read-only: the server refused this Mac's attribution".into(),
        (ReadOnly::OffBoard, _) => "read-only: not on the board".into(),
        (ReadOnly::Pending, _) => "waiting for the agent's details…".into(),
        (ReadOnly::Retired, _) => "retired · read-only".into(),
    }
}

fn say_failure(failure: &Failure) -> String {
    match failure {
        Failure::Unattributed(r) => format!("refused: {}", r.detail),
        Failure::Refused(why) => format!("refused: {why}"),
        Failure::Unreachable(why) => format!("unreachable, not sent ({why}) · ⌘⏎ retry"),
        Failure::UnknownAgent => "the server knows no such agent".into(),
        Failure::Rejected(status, why) => format!("rejected ({status}): {why}"),
        Failure::NoAnswer(why) => format!(
            "no answer ({why}): it may have been sent; check the transcript before retrying"
        ),
        Failure::NotSaved(why) => format!("not sent: the draft could not be saved ({why})"),
    }
}
