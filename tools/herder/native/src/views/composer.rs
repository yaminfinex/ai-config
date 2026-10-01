//! The composer (U4): a growing box under the transcript that sends to the zoomed agent through
//! herder's message endpoint. The text is the agent's draft in the store: every edit is dispatched,
//! and the box is set from the store whenever the two differ (another agent zoomed, a send landed).
//! It is disabled while a send is in flight and read-only, with the reason, when the store says the
//! agent cannot be written to. A send that failed keeps its text and says why under the box.
//!
//! Keys (ARCHITECTURE §4): `/` or `r` in the zoom focus it; in the box (`Composer > Input`, so no other
//! input sends) `cmd-enter` sends, `cmd-shift-enter` sends and, once it lands, files the agent back
//! into the lens (seen), `alt-enter` keeps the draft as a note instead (U5), `escape` leaves the box.
//! The wording under the box is here, the states in `store::composer`.

use crate::store::composer::{Failure, ReadOnly, Sending, Step};
use crate::store::notes::Step as NoteStep;
use crate::store::{Attribution, Event, Store};
use crate::views::lens::{Focus, Ui};
use crate::views::space::{self, Zoomed};
use crate::views::theme::{TypeScale, pal};
use crate::views::{Host, dim, on, settle_later};
use gpui_kit::component::input::{InputEvent, Textarea, TextareaState};
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
    /// The agent whose draft the box holds.
    agent: Option<String>,
}

impl View {
    pub fn new<H: Host>(window: &mut Window, cx: &mut Context<H>) -> Self {
        let state = cx.new(|cx| {
            let s = TextareaState::new(window, cx).auto_grow(ROWS.0, ROWS.1);
            s.placeholder("Message the agent…")
        });
        cx.subscribe(&state, |host: &mut H, state, event: &InputEvent, cx| {
            let agent = host.parts().1.composer.agent.clone();
            if let (InputEvent::Change, Some(agent)) = (event, agent) {
                let text = state.read(cx).value().to_string();
                host.dispatch(Event::Compose(Step::Edit { agent, text }), cx);
            }
        })
        .detach();
        View { state, agent: None }
    }

    pub fn focus_handle(&self, cx: &App) -> FocusHandle {
        self.state.read(cx).focus_handle(cx)
    }
}

/// Point the box at the zoomed agent's draft, before a frame is drawn. A landed file-back hands focus to
/// the lens wherever it was in the departing zoom, and a box left focused under another agent hands it
/// to the zoom.
pub fn sync(ui: &mut Ui, store: &Store, window: &mut Window, cx: &mut App) {
    let agent = ui.zoomed_agent().map(String::from);
    let stranded = ui.composer.agent != agent && ui.composer.focus_handle(cx).is_focused(window);
    if ui.focus.take() == Some(Focus::Out) || stranded {
        window.focus(ui.focus_target(), cx);
    }
    let view = &mut ui.composer;
    let drafts = &store.prefs.drafts;
    let draft = agent
        .as_ref()
        .and_then(|a| drafts.get(a))
        .map_or("", String::as_str);
    if view.agent != agent || view.state.read(cx).value() != draft {
        view.agent = agent;
        let draft = draft.to_string();
        view.state
            .update(cx, |s, cx| s.set_value(draft, window, cx));
    }
}

/// A composer key: `Focus` from the zoom, the rest from the box itself.
pub fn act(store: &Store, ui: &mut Ui, key: Compose) -> Vec<Event> {
    let Some(agent) = ui.zoomed_agent().map(String::from) else {
        return Vec::new();
    };
    let send = |file_back| {
        vec![Event::Compose(Step::Send {
            agent: agent.clone(),
            file_back,
        })]
    };
    match key {
        Compose::Focus => ui.focus = Some(into_box(store, &agent)),
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

/// Focus into `agent`'s box, or out where it is read-only.
pub(super) fn into_box(store: &Store, agent: &str) -> Focus {
    match store.can_send(agent) {
        Ok(()) => Focus::Box,
        Err(_) => Focus::Out,
    }
}

/// The box and its status line, under the transcript.
pub fn render<H: Host>(
    store: &Store,
    ui: &Ui,
    agent: &str,
    t: TypeScale,
    cx: &mut Context<H>,
) -> Div {
    let (line, color) = status(store, agent);
    let writable = store.can_send(agent).is_ok() && !store.busy(agent);
    let input = Textarea::new(&ui.composer.state).disabled(!writable);
    let input = div()
        .key_context("Composer")
        .on_action(on(cx, |store, ui, c: &Compose| act(store, ui, *c)))
        .child(input);
    div()
        .flex_none()
        .flex()
        .flex_col()
        .gap(t.px(4.))
        .px(t.px(20.))
        .py(t.px(8.))
        .border_t_1()
        .border_color(rgb(pal::RULE))
        .child(input)
        .child(dim(line).text_size(t.small).text_color(rgb(color)))
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
        (Ok(()), None) => (HINT.to_string(), pal::SLATE),
    }
}

fn say_read_only(viewer: &Attribution, why: ReadOnly) -> String {
    let refusal = match viewer {
        Attribution::Refused(Some(r)) => Some(r),
        _ => None,
    };
    match (why, refusal) {
        (ReadOnly::Refused, Some(r)) if r.error == "sender refused" => {
            format!(
                "read-only · sender collision: this Mac's sender name is taken ({})",
                r.detail
            )
        }
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
        Failure::NoAnswer(why) => {
            format!(
                "no answer ({why}): it may have been sent; check the transcript before retrying"
            )
        }
        Failure::NotSaved(why) => format!("not sent: the draft could not be saved ({why})"),
    }
}
