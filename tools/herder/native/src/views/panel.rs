//! An agent panel (DK1): one open agent's own views, its transcript (`transcript::View`, the list and
//! where it was read), notes strip (`notes`), composer (`composer`) and capture at a selection
//! (`capture`), under a focus of its own where its keys land. It is a tab in the zoom's dock (`dock`,
//! DK2), which keeps one for each tab and says which are shown: a tab behind another is hidden, its
//! rows let go and where it was read kept, to come back to when shown (`transcript::View::hide`); what
//! it was in the middle of (an open editor, a capture, a confirmation) ends. Each is drawn by its own
//! GPUI view (`AgentPanel`), cached, so it re-renders only when notified: it notifies itself whenever
//! its host does. Focus moving into it makes its agent the zoom's (`dock::sync`).

use crate::views::capture::{self, Capture};
use crate::views::composer::{self, Compose};
use crate::views::notes::{self, Notes};
use crate::views::notes_list::{self, Card};
use crate::views::theme::type_scale;
use crate::views::transcript::{self, Fold, OpenLink, Scroll, ToggleRun};
use crate::views::{Host, on, space};
use gpui_kit::component::dock::{
    BasePanel, BasePanelView, PanelEvent, PanelId, PanelInfo, PanelState,
};
use gpui_kit::*;
use std::sync::Arc;

pub struct Panel {
    pub(super) focus: FocusHandle,
    pub(super) transcript: transcript::View,
    pub(super) composer: composer::View,
    pub(super) notes: notes::View,
    pub(super) capture: capture::View,
    /// Its view in the dock (`AgentPanel`), and the dock's name for it.
    pub(super) view: Arc<dyn BasePanelView>,
    pub(super) id: PanelId,
    pub(super) shown: bool,
}

impl Panel {
    pub(super) fn new<H: Host>(
        agent: &str,
        web: &str,
        window: &mut Window,
        cx: &mut Context<H>,
    ) -> Self {
        let (host, focus) = (cx.entity(), cx.focus_handle());
        let view = cx.new(|cx| AgentPanel {
            agent: agent.to_string(),
            focus: focus.clone(),
            host: host.downgrade(),
            _host: cx.observe(&host, |_, _, cx| cx.notify()),
        });
        let id = PanelId::from(view.entity_id());
        Panel {
            focus,
            transcript: transcript::View::new(web),
            composer: composer::View::new(agent, window, cx),
            notes: notes::View::new(agent, window, cx),
            capture: capture::View::new(agent, window, cx),
            view: Arc::new(view),
            id,
            shown: true,
        }
    }

    /// Out of sight (a tab behind another, DK1): its rows go, where it was read stays, and what it was
    /// in the middle of ends.
    pub(super) fn hide(&mut self) {
        self.transcript.hide();
        self.notes.hide();
        self.capture.draft = None;
        self.shown = false;
    }
}

/// An agent panel as a GPUI view: drawn through its host, from the store and the lens's view state; a
/// panel of the dock (`dock`), named in a saved layout by its agent.
pub struct AgentPanel<H> {
    agent: String,
    focus: FocusHandle,
    host: WeakEntity<H>,
    _host: Subscription,
}

impl<H: Host> BasePanel for AgentPanel<H> {
    fn panel_name(&self) -> &'static str {
        "agent"
    }

    fn dump(&self, _: &App) -> PanelState {
        let mut state = PanelState::new(self.panel_name());
        state.info = PanelInfo::panel(serde_json::json!({ "agent": self.agent }));
        state
    }
}

impl<H: 'static> Focusable for AgentPanel<H> {
    fn focus_handle(&self, _: &App) -> FocusHandle {
        self.focus.clone()
    }
}

impl<H: 'static> EventEmitter<PanelEvent> for AgentPanel<H> {}

impl<H: Host> Render for AgentPanel<H> {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let Some(host) = self.host.upgrade() else {
            return div().into_any_element();
        };
        let agent = self.agent.clone();
        host.update(cx, |h, cx| render(h, &agent, cx))
    }
}

/// The panel: the transcript, the notes strip, the composer and any capture, with the keys that act on
/// them.
fn render<H: Host>(h: &mut H, agent: &str, cx: &mut Context<H>) -> AnyElement {
    let (store, ui) = h.view();
    let Some(panel) = ui.panels.get(agent) else {
        return div().into_any_element();
    };
    let t = type_scale(store.prefs.text_scale);
    // A press anywhere in it, the transcript's text too (its selection takes the press), focuses the
    // panel first; what is under it (a box) may take focus from there.
    let focus = panel.focus.clone();
    let press = move |_: &MouseDownEvent, window: &mut Window, cx: &mut App| {
        if !focus.contains_focused(window, cx) {
            window.focus(&focus, cx);
        }
    };
    div()
        .track_focus(&panel.focus)
        .capture_any_mouse_down(press)
        .on_action(on(cx, |store, ui, s: &Scroll| match ui.panel() {
            Some(p) => transcript::scroll(store, &p.transcript, *s),
            None => Vec::new(),
        }))
        .on_action(on(cx, |_, ui, _: &ToggleRun| match ui.panel() {
            Some(p) => transcript::toggle_lowest(&p.transcript),
            None => Vec::new(),
        }))
        .on_action(on(cx, |_, ui, f: &Fold| {
            if let Some(p) = ui.panel() {
                p.transcript.fold(*f);
            }
            Vec::new()
        }))
        .on_action(on(cx, |_, ui, l: &OpenLink| space::open(ui, &l.0, l.1)))
        .on_action(on(cx, |_, ui, c: &Compose| match c {
            Compose::Focus => composer::act(ui, *c),
            _ => Vec::new(),
        }))
        .on_action(on(cx, |store, ui, n: &Notes| notes::act(store, ui, n)))
        .on_action(on(cx, |store, ui, c: &Capture| capture::act(store, ui, c)))
        .on_action(on(cx, |store, ui, c: &Card| notes_list::act(store, ui, c)))
        .size_full()
        .flex()
        .flex_col()
        .child(transcript::render(store, ui, agent, t, cx))
        .children(notes::render(store, ui, agent, t, cx))
        .child(composer::render(store, ui, agent, t, cx))
        .children(capture::render(ui, agent, t, cx))
        .into_any_element()
}
