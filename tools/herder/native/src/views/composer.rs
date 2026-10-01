//! The composer (U4): a growing box under the transcript that sends to the zoomed agent through
//! herder's message endpoint. The text is the agent's draft in the store: every edit is dispatched,
//! and the box is set from the store whenever the two differ (another agent zoomed, a send landed).
//! It is disabled while a send is in flight and read-only, with the reason, when the store says the
//! agent cannot be written to. A send that failed keeps its text and says why under the box.
//!
//! Keys (ARCHITECTURE §4): `/` or `r` in the zoom focus it; in the box `cmd-enter` sends,
//! `cmd-shift-enter` sends and files the agent back into the lens (seen), `escape` leaves the box.

use crate::store::composer::{Sending, Step};
use crate::store::spaces::Move;
use crate::store::{Event, Store};
use crate::views::lens::Ui;
use crate::views::space::{self, Zoomed};
use crate::views::theme::{TypeScale, pal};
use crate::views::{Host, dim};
use gpui_kit::component::input::{InputEvent, Textarea, TextareaState};
use gpui_kit::*;

#[derive(Clone, Copy, Debug, PartialEq, Action)]
#[action(namespace = composer, no_json)]
pub enum Compose {
    Focus,
    Send,
    FileBack,
    Leave,
}

/// Rows the box grows to before it scrolls.
const ROWS: (usize, usize) = (1, 8);
const HINT: &str = "⌘⏎ send · ⌘⇧⏎ send and back to the lens · esc leave";

pub struct View {
    state: Entity<TextareaState>,
    /// The agent whose draft the box holds.
    agent: Option<String>,
    /// The last action's focus request: `Some(true)` into the box, `Some(false)` out of it.
    pub(super) want: Option<bool>,
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
        View {
            state,
            agent: None,
            want: None,
        }
    }

    pub fn focus_handle(&self, cx: &App) -> FocusHandle {
        self.state.read(cx).focus_handle(cx)
    }
}

/// Point the box at the zoomed agent's draft, before a frame is drawn.
pub fn sync(ui: &mut Ui, store: &Store, window: &mut Window, cx: &mut App) {
    let agent = ui.zoom.as_ref().and_then(|z| z.agent.clone());
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

/// `focused:text` or `idle:text`, for the harness's `box:` step.
pub fn probe(ui: &Ui, window: &Window, cx: &App) -> String {
    let focused = ui.composer.focus_handle(cx).is_focused(window);
    let text = ui.composer.state.read(cx).value();
    format!("{}:{text}", if focused { "focused" } else { "idle" })
}

/// A composer key, handled on the zoom shell (an ancestor of both the zoom and the box).
pub fn act(store: &Store, ui: &mut Ui, key: Compose) -> Vec<Event> {
    let Some((zoom, agent)) = ui.zoom.clone().and_then(|z| Some((z.clone(), z.agent?))) else {
        return Vec::new();
    };
    let send = |file_back| {
        Event::Compose(Step::Send {
            agent: agent.clone(),
            file_back,
        })
    };
    match key {
        Compose::Focus => ui.composer.want = Some(store.can_send(&agent).is_ok()),
        Compose::Leave => ui.composer.want = Some(false),
        Compose::Send => return vec![send(None)],
        // Only a send that will go files the agent back; otherwise the box stays, saying why.
        Compose::FileBack if store.ready(&agent) => {
            let seen = Event::Lens(Move::View {
                space: zoom.space.clone(),
                agent: Some(agent.clone()),
            });
            let out = space::act(store, ui, Zoomed::Out);
            return [send(Some(zoom.space)), seen]
                .into_iter()
                .chain(out)
                .collect();
        }
        Compose::FileBack => {}
    }
    Vec::new()
}

/// The box and its status line, under the transcript.
pub fn render(store: &Store, ui: &Ui, agent: &str, t: TypeScale) -> Div {
    let read_only = store.can_send(agent).err();
    let sending = store.in_flight(agent);
    let (line, color) = match (read_only, store.sends.get(agent)) {
        (Some(why), _) => (why.say().to_string(), pal::AMBER),
        (None, _) if sending => ("sending…".to_string(), pal::SLATE),
        (None, Some(Sending::Failed(failure))) => (failure.say(), pal::AMBER),
        (None, _) => (HINT.to_string(), pal::SLATE),
    };
    let input = Textarea::new(&ui.composer.state).disabled(read_only.is_some() || sending);
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
