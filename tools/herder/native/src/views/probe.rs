//! What the harness asks of the views (`harness::Probe`, built by the shell): every string a script
//! compares against is spelled here, so the view files hold no harness code.

use crate::store::Store;
use crate::store::condense::Row;
use crate::store::condense::Seg;
use crate::store::transcript::Item;
use crate::views::capture::Capture;
use crate::views::composer;
use crate::views::lens::{self, Pick, State, Ui};
use crate::views::notes::Notes;
use crate::views::notes_list::Card;
use crate::views::space::{Summon, Tab, Zoomed, zoomed};
use crate::views::transcript::{Fold, OpenLink, Scroll, long};
use gpui_kit::*;

/// What the app shows, for `expect`, `box` and `has`, `says`, `notes`, `capture`, `list`, `said`, `header`, `rows`, `parts`, `jump` and `start`; `None` for any other
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
        // The capture chip or popover (F7): `none`, `chip:<focus>:<quote>`, `open:<focus>:<text>`.
        "capture" => match &ui.capture.draft {
            None => "none".into(),
            Some(d) => {
                let focus = focused(ui.capture.focus_handle(cx));
                match d.open {
                    false => format!("chip:{focus}:{}", d.quote),
                    true => format!("open:{focus}:{}", ui.capture.text),
                }
            }
        },
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
            let picked = &ui.notes.list.picked;
            let selected = ids.iter().enumerate();
            let selected = selected.filter(|(_, id)| picked.selected.contains(**id));
            let selected: Vec<String> = selected.map(|(i, _)| i.to_string()).collect();
            let cursor = picked.cursor.as_ref().and_then(at);
            let cursor = cursor.map_or("-".to_string(), |c| c.to_string());
            format!(
                "{}:{}@{cursor}",
                focused(ui.notes.list.focus.clone()),
                selected.join(",")
            )
        }
        // The strip's confirmation line.
        "said" => ui.notes.said().unwrap_or_default(),
        "header" => lens::header_line(store),
        // The selected card's space, by name.
        "selected" => ui
            .selected(store)
            .map_or_else(String::new, |s| s.name.clone()),
        // The zoomed transcript's list rows: how many, how many are runs, and how many runs are open.
        "rows" => {
            let (rows, runs, open) = ui.transcript.census();
            format!("{rows} rows, {runs} runs, {open} open")
        }
        // How many answers' status chips and internal notes are open.
        "parts" => {
            let items = store.transcript.open.as_ref().map(|t| &t.items);
            let (status, notes) = items.map_or((0, 0), |i| ui.transcript.parts(i));
            format!("{status} status, {notes} notes")
        }
        // How many tools are open, each showing its input, and how many show an output too.
        "tools" => {
            let items = store.transcript.open.as_ref().map(|t| &t.items);
            let (open, output) = items.map_or((0, 0), |i| ui.transcript.tools(i));
            format!("{open} open, {output} with output")
        }
        // Whether jump-to-bottom shows.
        "jump" => match ui.transcript.jumps() {
            true => "shown".into(),
            false => "hidden".into(),
        },
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
/// order), `click:tab:i` (the zoom's tab, from the left), `click:crumb` (`lens ›`), `click:jump`
/// (jump-to-bottom, while it shows), `click:status` / `click:internal` (the last standalone answer's
/// status chip that opens, or internal note) and `click:member` / `click:failed` (the first tool, or failed tool, in an open run); `None` where
/// there is no such thing to click.
pub fn action(store: &Store, ui: &Ui, op: &str, arg: &str) -> Option<Box<dyn Action>> {
    let notes = |what: Notes| Some(Box::new(what) as Box<dyn Action>);
    let card = |what: Card| Some(Box::new(what) as Box<dyn Action>);
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
        ("jump", Some(_)) if ui.transcript.jumps() => return Some(Box::new(Scroll::Bottom)),
        // The last answer's status chip that a click opens, or its internal note.
        ("status" | "internal", Some(_)) => {
            let items = &store.transcript.open.as_ref()?.items;
            let part = |seg: &Seg| match seg {
                Seg::Status(s) => what == "status" && long(s),
                Seg::Internal(_) => what == "internal",
                Seg::Text(_) => false,
            };
            // A run's answer holds its notes open: a standalone one.
            let mut answers = items.iter().rev().filter_map(|(&key, item)| match item {
                Item::Assistant(segs) if !item.activity() => {
                    Some((key, segs.iter().position(part)?))
                }
                _ => None,
            });
            let (key, at) = answers.next()?;
            return Some(Box::new(Fold(key, at)));
        }
        // The first tool in an open run, or the first that failed.
        ("member" | "failed", Some(_)) => {
            let items = &store.transcript.open.as_ref()?.items;
            let key = ui.transcript.first_tool(items, what == "failed")?;
            return Some(Box::new(Fold(key, 0)));
        }
        (
            "card" | "card2" | "tab" | "crumb" | "jump" | "status" | "internal" | "member"
            | "failed",
            _,
        ) => {
            return None;
        }
        _ => {}
    }
    let agent = ui.zoomed_agent()?;
    let note = |i: &str| {
        let note = store.notes_of(agent).nth(i.parse().ok()?)?;
        Some(SharedString::from(note.id.clone()))
    };
    match (what, i) {
        // The capture chip, a click on which saves the quote.
        ("capture", _) => {
            let chip = ui
                .capture
                .draft
                .as_ref()
                .filter(|d| d.agent == agent && !d.open);
            chip.map(|_| Capture::Save.boxed_clone())
        }
        ("sendall", _) => {
            let shown = note("0").is_some() && !store.hand_off_blocked(agent);
            shown.then_some(Notes::HandOff).and_then(notes)
        }
        ("add", _) => notes(Notes::Add),
        ("note", i) => {
            let (i, held) = i.split_once(':').unwrap_or((i, ""));
            card(Card::Pick {
                id: note(i)?,
                command: held == "cmd",
                shift: held == "shift",
            })
        }
        ("edit", i) => card(Card::Edit(note(i)?)),
        ("delete", i) => card(Card::Delete(note(i)?)),
        _ => None,
    }
}

/// Scroll the zoomed transcript so the first row with `text` in an item (as debug-printed) is at the
/// top, or the member holding it when that row is an open run; with `open`, also open the first run
/// from that row on (A3's side-by-side shots).
pub fn find(store: &Store, ui: &Ui, text: &str, open: bool) -> bool {
    let Some(tr) = store.transcript.open.as_ref() else {
        return false;
    };
    let rows = ui.transcript.rows.borrow();
    let items = |r: &Row| tr.items.range(r.first()..=r.last());
    let holds = |r: &Row| items(r).any(|(_, item)| format!("{item:?}").contains(text));
    let Some(ix) = rows.2.iter().position(holds) else {
        return false;
    };
    let run = rows.2[ix..]
        .iter()
        .enumerate()
        .find_map(|(at, r)| match *r {
            Row::Run(first, last) => Some(((first, last), ix + at)),
            Row::One(_) => None,
        });
    if let Some((run, at)) = run.filter(|_| open) {
        ui.transcript.toggle(run, at);
    }
    let member = items(&rows.2[ix]).find(|(_, item)| format!("{item:?}").contains(text));
    if let (Row::Run(..), Some((&key, _))) = (rows.2[ix], member) {
        return ui.transcript.reveal(key) || scroll_to(ui, ix);
    }
    scroll_to(ui, ix)
}

fn scroll_to(ui: &Ui, ix: usize) -> bool {
    ui.transcript.list.scroll_to(ListOffset {
        item_ix: ix,
        offset_in_item: px(0.),
    });
    true
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
