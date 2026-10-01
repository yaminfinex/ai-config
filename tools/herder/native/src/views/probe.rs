//! What the harness asks of the views (`harness::Probe`, built by the shell): every string a script
//! compares against is spelled here, so the view files hold no harness code.

use crate::store::Store;
use crate::views::composer;
use crate::views::lens::{self, Pick, State, Ui};
use crate::views::notes::Notes;
use crate::views::space::{Summon, Tab, Zoomed, zoomed};
use crate::views::transcript::OpenLink;
use gpui_kit::*;

/// What the app shows, for `expect`, `box` and `has`, `says`, `notes`, `list`, `said`, `header` and `start`; `None` for any other
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
        // The notes list's focus, its selection (indexes, newest-updated first) and cursor: `focused:0,1@1`.
        "list" => {
            let ids: Vec<&str> = store
                .notes_of(agent.unwrap_or(""))
                .map(|n| n.id.as_str())
                .collect();
            let at = |id: &String| ids.iter().position(|i| i == id);
            let picked = &ui.notes.picked;
            let selected = ids.iter().enumerate();
            let selected = selected.filter(|(_, id)| picked.selected.contains(**id));
            let selected: Vec<String> = selected.map(|(i, _)| i.to_string()).collect();
            let cursor = picked.cursor.as_ref().and_then(at);
            let cursor = cursor.map_or("-".to_string(), |c| c.to_string());
            format!(
                "{}:{}@{cursor}",
                focused(ui.notes.list.clone()),
                selected.join(",")
            )
        }
        // The strip's confirmation line.
        "said" => ui
            .notes
            .said
            .as_ref()
            .map_or_else(String::new, |s| s.1.clone()),
        "header" => lens::header_line(store),
        // The selected card's space, by name.
        "selected" => ui
            .selected(store)
            .map_or_else(String::new, |s| s.name.clone()),
        "start" => {
            let t = store.transcript.open.as_ref().filter(|t| t.at_start())?;
            format!("{}: start reached, {} rows", t.agent, t.items.len())
        }
        _ => return None,
    })
}

/// What a click dispatches: `link:<url>` on a transcript link, `summon:<tag>` on a notification,
/// `click:<capture|sendall|add|note:i[:cmd|:shift]|edit:i|delete:i>` on the notes strip (`i` the zoomed
/// agent's note, newest-updated first; `note` a click on its card, with ⌘ or ⇧ held; `edit` a double-click), and on the lens and the zoom `click:card:i` (`card2:i` a double-click; `i` the card in lens
/// order), `click:tab:i` (the zoom's tab, from the left) and `click:crumb` (`lens ›`); `None` where
/// there is no such thing to click.
pub fn action(store: &Store, ui: &Ui, op: &str, arg: &str) -> Option<Box<dyn Action>> {
    let notes = |what: Notes| Some(Box::new(what) as Box<dyn Action>);
    match op {
        "link" => return Some(Box::new(OpenLink(arg.to_string().into()))),
        "summon" => return Some(Box::new(Summon(arg.to_string().into()))),
        "click" => {}
        _ => return None,
    }
    let (what, i) = arg.split_once(':').unwrap_or((arg, ""));
    let nth = |i: &str| i.parse::<usize>().ok();
    match (what, &ui.zoom) {
        ("card" | "card2", None) => {
            let space = *store.lens().get(nth(i)?)?;
            let space = space.id.clone().into();
            return Some(Box::new(Pick {
                space,
                zoom: what == "card2",
            }));
        }
        ("tab", Some(zoom)) => {
            let mut tabs: Vec<&str> = zoomed(store, zoom)
                .into_iter()
                .flat_map(|s| s.agents())
                .collect();
            tabs.extend(zoom.agent.as_deref().filter(|a| !tabs.contains(a)));
            return Some(Box::new(Tab(tabs.get(nth(i)?)?.to_string().into())));
        }
        ("crumb", Some(_)) => return Some(Box::new(Zoomed::Out)),
        ("card" | "card2" | "tab" | "crumb", _) => return None,
        _ => {}
    }
    let agent = ui.zoomed_agent()?;
    let note = |i: &str| {
        let note = store.notes_of(agent).nth(i.parse().ok()?)?;
        Some(SharedString::from(note.id.clone()))
    };
    match (what, i) {
        ("capture", _) => {
            ui.notes.selection.as_ref().filter(|(a, _)| a == agent)?;
            notes(Notes::Capture)
        }
        ("sendall", _) => {
            let shown = note("0").is_some() && !store.hand_off_blocked(agent);
            shown.then_some(Notes::HandOff).and_then(notes)
        }
        ("add", _) => notes(Notes::Add),
        ("note", i) => {
            let (i, held) = i.split_once(':').unwrap_or((i, ""));
            notes(Notes::Pick {
                id: note(i)?,
                command: held == "cmd",
                shift: held == "shift",
            })
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
