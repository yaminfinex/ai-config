//! What the harness asks of the views (`harness::Probe`, built by the shell): every string a script
//! compares against is spelled here, so the view files hold no harness code.

use crate::store::Store;
use crate::store::condense::Row;
use crate::store::condense::Seg;
use crate::store::transcript::Item;
use crate::views::capture::Capture;
use crate::views::lens::{self, Pick, State, Ui};
use crate::views::notes::Notes;
use crate::views::notes_list::Card;
use crate::views::paths::Paths;
use crate::views::space::{Summon, Tab, Zoomed, zoomed};
use crate::views::transcript::{self, Fold, OpenLink, Scroll, long};
use crate::views::{composer, dock};
use gpui_kit::*;

/// What the app shows, for `expect`, `box` and `has`, `says`, `notes`, `capture`, `list`, `said`, `header`, `rows`, `parts`, `paths`, `jump` and `start`; `None` for any other
/// step, and for `start` until the open transcript holds every entry back to its start.
pub fn ask(store: &Store, ui: &Ui, op: &str, window: &Window, cx: &App) -> Option<String> {
    let (agent, panel) = (ui.zoomed_agent(), ui.panel());
    let focused = |handle: Option<FocusHandle>| match handle.is_some_and(|h| h.is_focused(window)) {
        true => "focused",
        false => "idle",
    };
    let transcript = panel.map(|p| &p.transcript);
    let items = store.transcript.focused().map(|t| &t.items);
    Some(match op {
        "expect" => shown(store, ui),
        "dock" => dock::describe(store, ui, cx),
        // The zoomed agent's composer's focus and text.
        "box" | "has" => {
            let state = panel.map(|p| &p.composer.state);
            let text = state.map_or(String::new(), |s| s.read(cx).value().to_string());
            let focus = focused(panel.map(|p| p.composer.focus_handle(cx)));
            format!("{focus}:{text}")
        }
        // The capture chip or popover (F7): `none`, `chip:<focus>:<quote>`, `open:<focus>:<text>`.
        "capture" => match panel.and_then(|p| Some((p, p.capture.draft.as_ref()?))) {
            None => "none".into(),
            Some((p, d)) => {
                let focus = focused(Some(p.capture.focus_handle(cx)));
                match d.open {
                    false => format!("chip:{focus}:{}", d.quote),
                    true => format!("open:{focus}:{}", p.capture.text),
                }
            }
        },
        // The line under the composer.
        "says" => agent.map_or_else(String::new, |a| composer::status(store, a).0),
        // The zoomed agent's note count, then the editor: `closed`, or its focus and text.
        "notes" => {
            let n = store.notes_of(agent.unwrap_or("")).count();
            match panel.filter(|p| p.notes.editing.is_some()) {
                None => format!("{n}:closed"),
                Some(p) => {
                    let focus = focused(Some(p.notes.focus_handle(cx)));
                    format!("{n}:{focus}:{}", p.notes.text)
                }
            }
        }
        // The notes list's focus, its selection (indexes, newest-updated first) and cursor: `focused:0,1@1`.
        "list" => {
            let ids: Vec<&str> = store
                .notes_of(agent.unwrap_or(""))
                .map(|n| n.id.as_str())
                .collect();
            let at = |id: &String| ids.iter().position(|i| i == id);
            let list = panel.map(|p| &p.notes.list);
            let picked = list.map(|l| &l.picked);
            let selected = ids.iter().enumerate();
            let selected =
                selected.filter(|(_, id)| picked.is_some_and(|p| p.selected.contains(**id)));
            let selected: Vec<String> = selected.map(|(i, _)| i.to_string()).collect();
            let cursor = picked.and_then(|p| p.cursor.as_ref()).and_then(at);
            let cursor = cursor.map_or("-".to_string(), |c| c.to_string());
            let focus = focused(list.map(|l| l.focus.clone()));
            format!("{focus}:{}@{cursor}", selected.join(","))
        }
        // The strip's confirmation line.
        "said" => panel.and_then(|p| p.notes.said()).unwrap_or_default(),
        "header" => lens::header_line(store),
        // The selected card's space, by name.
        "selected" => ui
            .selected(store)
            .map_or_else(String::new, |s| s.name.clone()),
        // The zoomed transcript's list rows: how many, how many are runs, and how many runs are open.
        "rows" => {
            let (rows, runs, open) = transcript.map_or((0, 0, 0), |v| v.census());
            format!("{rows} rows, {runs} runs, {open} open")
        }
        // How many answers' status chips and internal notes are open.
        "parts" => {
            let parts = transcript.zip(items).map(|(v, i)| v.parts(i));
            let (status, notes) = parts.unwrap_or_default();
            format!("{status} status, {notes} notes")
        }
        // How many tools are open, each showing its input, and how many show an output too.
        "tools" => {
            let tools = transcript.zip(items).map(|(v, i)| v.tools(i));
            let (open, output) = tools.unwrap_or_default();
            format!("{open} open, {output} with output")
        }
        // A clicked path's choices (G3): `none`, or `<focus>:<count>@<cursor>`.
        "paths" => match agent.and_then(|a| store.transcript.open.get(a)?.choices.as_ref()) {
            None => "none".into(),
            Some(c) => {
                let v = panel.map(|p| &p.paths);
                let focus = focused(v.map(|v| v.focus.clone()));
                let cursor = v.map_or(0, |v| v.cursor);
                format!("{focus}:{}@{cursor}", c.candidates.len())
            }
        },
        // Whether jump-to-bottom shows.
        "jump" => match transcript.is_some_and(|v| v.jumps()) {
            true => "shown".into(),
            false => "hidden".into(),
        },
        "start" => {
            let t = store.transcript.focused().filter(|t| t.at_start())?;
            format!("{}: start reached, {} rows", t.agent, t.items.len())
        }
        _ => return None,
    })
}

/// What a click dispatches: `link:<url>` on a transcript link, `summon:<tag>` on a notification,
/// `click:<capture|sendall|add|note:i[:cmd|:shift]|edit:i|delete:i>` on the notes strip (`i` the zoomed
/// agent's note, newest-updated first; `note` a click on its card, with ⌘ or ⇧ held; `edit` a double-click), and on the lens and the zoom `click:card:i` (`card2:i` a double-click; `i` the card in lens
/// order), `click:tab:i` / `click:close:i` / `click:pin:i` (a click on the dock's tab `i`, group by group from
/// the left; its ×; a double-click on it), `click:max` (the focused group's □), `click:path:i` (a clicked path's candidate), `click:crumb` (`lens ›`), `click:jump`
/// (jump-to-bottom, while it shows), `click:status` / `click:internal` (the last standalone answer's
/// status chip that opens, or internal note) and `click:member` / `click:failed` (the first tool, or failed tool, in an open run); `None` where
/// there is no such thing to click.
pub fn action(store: &Store, ui: &Ui, op: &str, arg: &str, cx: &App) -> Option<Box<dyn Action>> {
    let notes = |what: Notes| Some(Box::new(what) as Box<dyn Action>);
    let card = |what: Card| Some(Box::new(what) as Box<dyn Action>);
    match op {
        "link" | "beside" => {
            let link = OpenLink(arg.to_string().into(), op == "beside");
            return Some(Box::new(link));
        }
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
        ("tab" | "close" | "pin", Some(_)) => {
            let tab: SharedString = dock::tab_names(ui, cx).get(nth(i)?)?.clone().into();
            return Some(match what {
                "tab" => Box::new(Tab(tab)),
                "close" => Box::new(dock::Close(Some(tab))),
                _ => Box::new(dock::Pin(tab)),
            });
        }
        ("max", Some(_)) => return Some(Box::new(dock::Maximize)),
        // A clicked path's candidate `i` (G3).
        ("path", Some(_)) => {
            let choices = store.transcript.focused()?.choices.as_ref()?;
            let i = nth(i).filter(|&i| i < choices.candidates.len())?;
            return Some(Box::new(Paths::Choose(i)));
        }
        ("crumb", Some(_)) => return Some(Box::new(Zoomed::Out)),
        ("jump", Some(_)) if ui.panel().is_some_and(|p| p.transcript.jumps()) => {
            return Some(Box::new(Scroll::Bottom));
        }
        // The last answer's status chip that a click opens, or its internal note.
        ("status" | "internal", Some(_)) => {
            let items = &store.transcript.focused()?.items;
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
            let items = &store.transcript.focused()?.items;
            let key = ui.panel()?.transcript.first_tool(items, what == "failed")?;
            return Some(Box::new(Fold(key, 0)));
        }
        (
            "card" | "card2" | "tab" | "close" | "pin" | "max" | "path" | "crumb" | "jump"
            | "status" | "internal" | "member" | "failed",
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
            let chip = ui.panel()?.capture.draft.as_ref().filter(|d| !d.open);
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
    let (Some(tr), Some(view)) = (
        store.transcript.focused(),
        ui.panel().map(|p| &p.transcript),
    ) else {
        return false;
    };
    let rows = view.rows.borrow();
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
        view.toggle(run, at);
    }
    let member = items(&rows.2[ix]).find(|(_, item)| format!("{item:?}").contains(text));
    if let (Row::Run(..), Some((&key, _))) = (rows.2[ix], member) {
        return view.reveal(key) || scroll_to(view, ix);
    }
    scroll_to(view, ix)
}

fn scroll_to(view: &transcript::View, ix: usize) -> bool {
    view.list.scroll_to(ListOffset {
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
