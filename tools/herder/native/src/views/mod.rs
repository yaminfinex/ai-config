//! GPUI views. They render from `&Store` and dispatch `Event`s through the shell; domain state never
//! changes here. Each view owns the GPUI widget entities it renders with (`ListState`, `TextareaState`,
//! `EditorState`); those are widget state, not domain state, and never go through the store. Every size
//! comes from `theme::type_scale`.
//!
//! One file per surface, added by the unit that needs it: `lens` (U2, the home rows and cards), `space`
//! (U2, the zoom shell and tabs), `transcript` (U3), `composer` (U4), `notes` (U5). `theme` holds the
//! palette and the type scale.

pub mod lens;
pub mod space;
pub mod theme;

use crate::store::{Event, Store};
use gpui_kit::{Action, App, Context, Window};

/// What views need from the shell that owns them, so they depend on this trait and not on the shell:
/// the store to read beside the lens's view state, and the one path to a state change.
pub trait Host: Sized + 'static {
    fn parts(&mut self) -> (&Store, &mut lens::Ui);
    fn dispatch(&mut self, event: Event, cx: &mut Context<Self>);
}

/// An action handler: `f` reads the store, moves the view state and returns the events to dispatch;
/// then focus follows the zoom (the `Space` context is live only while its element has focus).
pub fn on<A: Action, H: Host>(
    cx: &mut Context<H>,
    f: impl Fn(&Store, &mut lens::Ui, &A) -> Vec<Event> + 'static,
) -> impl Fn(&A, &mut Window, &mut App) + 'static {
    cx.listener(move |host: &mut H, action: &A, window, cx| {
        let (store, ui) = host.parts();
        let events = f(store, ui, action);
        let target = ui.focus_target().clone();
        if !target.is_focused(window) {
            window.focus(&target, cx);
        }
        for event in events {
            host.dispatch(event, cx);
        }
        cx.notify();
    })
}
