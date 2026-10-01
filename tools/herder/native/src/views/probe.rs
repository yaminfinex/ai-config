//! What the harness asks of the views (`harness::Probe`, built by the shell): every string a script
//! compares against is spelled here, so the view files hold no harness code.

use crate::store::Store;
use crate::views::composer;
use crate::views::lens::{self, State, Ui};
use crate::views::notes::Notes;
use crate::views::space::{Summon, zoomed};
use crate::views::transcript::OpenLink;
use gpui_kit::*;

/// What the app shows, for `expect`, `box` and `has`, `says`, `notes`, `header` and `start`; `None` for any other
/// step, and for `start` until the open transcript holds every entry back to its start.
pub fn ask(store: &Store, ui: &Ui, op: &str, window: &Window, cx: &App) -> Option<String> {
    let agent = ui.zoomed_agent();
    let focused = |handle: FocusHandle| match handle.is_focused(window) {
        true => "focused",
        false => "idle",
    };
    Some(match op {
        "expect" => shown(store, ui),
        // The composer's focus and text.
        "box" | "has" => {
            let text = ui.composer.state.read(cx).value();
            format!("{}:{text}", focused(ui.composer.focus_handle(cx)))
        }
        // The line under the composer.
        "says" => agent.map_or_else(String::new, |a| composer::status(store, a).0),
        // The zoomed agent's note count, then the editor: `closed`, or its focus and text.
        "notes" => {
            let n = store.notes_of(agent.unwrap_or("")).count();
            let focus = focused(ui.notes.focus_handle(cx));
            match &ui.notes.editing {
                None => format!("{n}:closed"),
                Some(_) => format!("{n}:{focus}:{}", ui.notes.text),
            }
        }
        "header" => lens::header_line(store),
        "start" => {
            let t = store.transcript.open.as_ref().filter(|t| t.at_start())?;
            format!("{}: start reached, {} rows", t.agent, t.items.len())
        }
        _ => return None,
    })
}

/// What a click dispatches: `link:<url>` on a transcript link, `summon:<tag>` on a notification, and
/// `click:<capture|handoff|edit:i|delete:i>` on the notes strip (`i` the zoomed agent's note, oldest
/// first); `None` where there is no such thing to click.
pub fn action(store: &Store, ui: &Ui, op: &str, arg: &str) -> Option<Box<dyn Action>> {
    let notes = |what: Notes| Some(Box::new(what) as Box<dyn Action>);
    match op {
        "link" => return Some(Box::new(OpenLink(arg.to_string().into()))),
        "summon" => return Some(Box::new(Summon(arg.to_string().into()))),
        "click" => {}
        _ => return None,
    }
    let agent = ui.zoomed_agent()?;
    let note = |i: &str| {
        let note = store.notes_of(agent).nth(i.parse().ok()?)?;
        Some(SharedString::from(note.id.clone()))
    };
    match arg.split_once(':').unwrap_or((arg, "")) {
        ("capture", _) => {
            ui.notes.selection.as_ref().filter(|(a, _)| a == agent)?;
            notes(Notes::Capture)
        }
        ("handoff", _) => {
            let shown = note("0").is_some() && !store.hand_off_blocked(agent);
            shown.then_some(Notes::HandOff).and_then(notes)
        }
        ("edit", i) => notes(Notes::Edit(note(i)?)),
        ("delete", i) => notes(Notes::Delete(note(i)?)),
        _ => None,
    }
}

/// Stand in for a pointer selection of `text` in the zoomed transcript (the harness cannot drag).
pub fn select(ui: &mut Ui, text: &str) {
    let agent = ui.zoomed_agent().map(String::from);
    ui.notes.selected(agent, text);
}

/// What the zoom shows: `name`, or `name preview` for an outsider.
pub(super) fn shown(store: &Store, ui: &State) -> String {
    let Some(zoom) = ui.zoom.as_ref() else {
        return String::new();
    };
    let agent = zoom.agent.as_deref().unwrap_or("");
    let member = zoomed(store, zoom).is_some_and(|s| s.agents().any(|a| a == agent));
    let outsider = !member && !agent.is_empty();
    format!("{agent}{}", if outsider { " preview" } else { "" })
}
