//! An agent panel (DK1): one open agent's own views, its transcript (`transcript::View`, the list and
//! where it was read), notes strip (`notes`), composer (`composer`) and capture at a selection
//! (`capture`), under a focus of its own where its keys land. The zoom shows its agent's panel, the
//! focused one, and keeps one for each of the zoomed space's agents opened since. A panel not shown is
//! hidden: its rows go and where it was read stays, to come back to when shown (`transcript::View::hide`);
//! what it was in the middle of (an open editor, a capture, a confirmation) ends, as it did when the zoom
//! had one set of views. Each is drawn by its own GPUI view (`AgentPanel`), cached, so it re-renders
//! only when notified: it notifies itself whenever its host does. The dock (DK2) lays these out.

use crate::store::Store;
use crate::store::spaces::Space;
use crate::views::capture::{self, Capture};
use crate::views::composer::{self, Compose};
use crate::views::lens::Ui;
use crate::views::notes::{self, Notes};
use crate::views::notes_list::{self, Card};
use crate::views::space::{self, Anim};
use crate::views::theme::type_scale;
use crate::views::transcript::{self, Fold, OpenLink, Scroll, ToggleRun};
use crate::views::{Host, on};
use gpui_kit::*;

pub struct Panel {
    pub(super) focus: FocusHandle,
    pub(super) transcript: transcript::View,
    pub(super) composer: composer::View,
    pub(super) notes: notes::View,
    pub(super) capture: capture::View,
    view: AnyView,
    shown: bool,
}

impl Panel {
    fn new<H: Host>(agent: &str, web: &str, window: &mut Window, cx: &mut Context<H>) -> Self {
        let host = cx.entity();
        let view = cx.new(|cx| AgentPanel {
            agent: agent.to_string(),
            host: host.downgrade(),
            _host: cx.observe(&host, |_, _, cx| cx.notify()),
        });
        Panel {
            focus: cx.focus_handle(),
            transcript: transcript::View::new(web),
            composer: composer::View::new(agent, window, cx),
            notes: notes::View::new(agent, window, cx),
            capture: capture::View::new(agent, window, cx),
            view: view.into(),
            shown: true,
        }
    }

    fn hide(&mut self) {
        self.transcript.hide();
        self.notes.hide();
        self.capture.draft = None;
        self.shown = false;
    }

    /// The panel's view, laid out in the zoom's column under the tabs.
    pub(super) fn view(&self) -> impl IntoElement + use<> {
        let style = StyleRefinement::default().w_full().flex_1().min_h_0();
        self.view.clone().cached(style)
    }
}

/// Before a frame, and after every action: a panel for the zoom's agent, shown, the space's others
/// hidden, and none for anyone else (another space, a preview left, the lens once the morph back has
/// landed). Focus left in a panel hidden or dropped goes to the zoom's.
pub fn sync<H: Host>(ui: &mut Ui, store: &Store, window: &mut Window, cx: &mut Context<H>) {
    let leaving = ui.anim.as_ref().and_then(Anim::leaving);
    let zoom = ui.zoom.as_ref().or(leaving).cloned();
    let shown = zoom.as_ref().and_then(|z| z.agent.clone());
    let space = zoom.as_ref().and_then(|z| space::zoomed(store, z));
    let wanted = |a: &str| shown.as_deref() == Some(a) || space.is_some_and(|s| holds(s, a));
    let focused = window.focused(cx);
    let held = |p: &Panel| {
        focused
            .as_ref()
            .is_some_and(|f| p.focus.contains(f, window))
    };
    let mut strand = false;
    ui.panels.retain(|a, p| {
        let keep = wanted(a);
        strand |= !keep && held(p);
        keep
    });
    for (a, p) in ui.panels.iter_mut() {
        if p.shown && Some(a) != shown.as_ref() {
            strand |= held(p);
            p.hide();
        }
    }
    if let Some(agent) = shown {
        match ui.panels.get_mut(&agent) {
            Some(p) => p.shown = true,
            None => {
                let p = Panel::new(&agent, &ui.web, window, cx);
                ui.panels.insert(agent, p);
            }
        }
    }
    if strand {
        window.focus(ui.focus_target(), cx);
    }
}

fn holds(space: &Space, agent: &str) -> bool {
    space.agents().any(|a| a == agent)
}

/// An agent panel as a GPUI view: drawn through its host, from the store and the lens's view state.
pub struct AgentPanel<H> {
    agent: String,
    host: WeakEntity<H>,
    _host: Subscription,
}

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
    div()
        .track_focus(&panel.focus)
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
        .on_action(on(cx, |_, ui, l: &OpenLink| space::open(ui, &l.0)))
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
