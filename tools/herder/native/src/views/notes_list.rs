//! The notes list (F6): the zoomed agent's notes as web's keyboard list (`notesListModel`, `NotesList`),
//! under the strip's header (`views::notes`). It owns the list's selection (`Picked`: the chosen notes,
//! the anchor a range extends from, and the cursor), its focus and scroll, its keys and its cards. A card
//! shows its source, its quote beside an accent bar and its full text (or the notes editor, editing it),
//! chosen, under the cursor or armed for deletion.
//!
//! Keys (ARCHITECTURE §4). `up` from an empty box (or its caret at the start) enters the list with every
//! note selected. In the list (`NotesList`): `up` `down` move, with `shift` extending; `cmd-a` all;
//! `enter` the selection into the composer; `backspace` twice deletes it (any other key, a click or
//! focus leaving the list disarms it); `e` edits the cursor's note in place; `cmd-c` copies; `escape`
//! clears the selection, then leaves for the box. A click picks a note (`cmd` toggles, `shift` a range),
//! a double-click edits it; a click on its ✕ or in its editor stays there.

use crate::store::notes::{Note, Step, source_label};
use crate::store::{Event, Store};
use crate::views::lens::{Focus, Ui};
use crate::views::notes::{self, Editing, count, ids};
use crate::views::theme::{TypeScale, pal};
use crate::views::{Host, dim, on};
use gpui_kit::prelude::FluentBuilder as _;
use gpui_kit::*;
use std::collections::BTreeSet;

/// The list's keys (`NotesList`).
#[derive(Clone, Copy, Debug, PartialEq, Action)]
#[action(namespace = notes, no_json)]
pub enum List {
    /// `up` / `down`; with `shift`, extending the selection from its anchor.
    Move(isize, bool),
    All,
    HandOff,
    Delete,
    Edit,
    Leave,
}

/// The pointer on a card.
#[derive(Clone, Debug, PartialEq, Action)]
#[action(namespace = notes, no_json)]
pub enum Card {
    /// A click, `command` toggling the note, `shift` picking the range from the anchor.
    Pick {
        id: SharedString,
        command: bool,
        shift: bool,
    },
    /// A double-click: edit it in place.
    Edit(SharedString),
    /// A click on its ✕: the first arms it, the second deletes it.
    Delete(SharedString),
}

// `Up`: `up` in the composer's box, into the list when the box is empty or its caret at the start (web's
// `composerArrowUpAction`), else the caret's move. `Copy`: `cmd-c` in the list. Each is its own action
// so nothing else handles it (`Up` falls through to the kit's caret move; `Copy` needs the clipboard).
actions!(notes, [Up, Copy]);

/// Where the list's keys bind: the list, not the editor open in one of its cards.
pub const LIST: &str = "NotesList && !Input";
/// The list's height before it scrolls, web's 184 px in design pixels.
const CAP: f32 = 204.;

pub struct State {
    pub(super) focus: FocusHandle,
    /// Whether the list held focus at the last render (`sync`), for the cursor.
    focused: bool,
    pub(super) picked: Picked,
    scroll: ScrollHandle,
    /// What a second delete removes: a note's ✕, or the selection on `backspace`.
    armed: Vec<String>,
}

/// The list's selection, as web's `NoteSelection`: the chosen notes, the anchor a range extends from,
/// and the cursor. `ids` is always the zoomed agent's notes in list order (newest-updated first).
#[derive(Clone, Debug, Default, PartialEq)]
pub struct Picked {
    pub selected: BTreeSet<String>,
    anchor: Option<String>,
    pub cursor: Option<String>,
}

impl Picked {
    fn one(id: &str) -> Self {
        let id = id.to_string();
        Picked {
            selected: BTreeSet::from([id.clone()]),
            anchor: Some(id.clone()),
            cursor: Some(id),
        }
    }

    /// From `anchor` (if still listed, else `to`) through `to`, the cursor on `to`.
    fn range(ids: &[String], anchor: Option<&String>, to: &str) -> Self {
        let anchor = anchor
            .filter(|a| ids.contains(a))
            .map_or(to, String::as_str);
        let at = |id: &str| ids.iter().position(|i| i == id);
        let selected = match (at(anchor), at(to)) {
            (Some(a), Some(b)) => ids[a.min(b)..=a.max(b)].iter().cloned().collect(),
            _ => BTreeSet::from([to.to_string()]),
        };
        let (anchor, cursor) = (Some(anchor.to_string()), Some(to.to_string()));
        Picked {
            selected,
            anchor,
            cursor,
        }
    }

    /// A click: alone, toggled (`command`), or the range from the anchor (`shift`).
    pub fn click(&mut self, ids: &[String], id: &str, command: bool, shift: bool) {
        *self = match (shift, command) {
            (true, _) => Self::range(ids, self.anchor.as_ref(), id),
            (false, true) => {
                let mut selected = std::mem::take(&mut self.selected);
                if !selected.remove(id) {
                    selected.insert(id.to_string());
                }
                Picked {
                    selected,
                    ..Self::one(id)
                }
            }
            (false, false) => Self::one(id),
        };
    }

    /// `up` / `down` (`by`), extending from the anchor with `shift`. With no cursor, `down` lands on
    /// the first note and `up` on the last.
    pub fn step(&mut self, ids: &[String], by: isize, extend: bool) {
        let Some(last) = ids.len().checked_sub(1) else {
            return;
        };
        let at = self
            .cursor
            .as_ref()
            .and_then(|c| ids.iter().position(|i| i == c));
        let from = at.map_or(if by > 0 { -1 } else { ids.len() as isize }, |a| a as isize);
        let cursor = &ids[(from + by).clamp(0, last as isize) as usize];
        *self = match extend {
            true => Self::range(ids, self.anchor.as_ref(), cursor),
            false => Self::one(cursor),
        };
    }

    /// Every note; the cursor stays where it is, else lands on the first (`cmd-a`, and `up` from the box).
    pub fn all(&mut self, ids: &[String]) {
        let cursor = self.cursor.take().filter(|c| ids.contains(c));
        *self = Picked {
            selected: ids.iter().cloned().collect(),
            anchor: ids.first().cloned(),
            cursor: cursor.or_else(|| ids.first().cloned()),
        };
    }

    /// The notes `removed` left `ids` (deleted, or handed off): if any was selected, the note after the
    /// last of them (else the one before) is selected alone. Otherwise `sync` prunes them.
    pub fn removed(&mut self, ids: &[String], removed: &[String]) {
        let gone = |id: &String| removed.contains(id);
        if let (Some(last), true) = (ids.iter().rposition(gone), self.selected.iter().any(gone)) {
            let after = ids[last + 1..].iter().find(|i| !gone(i));
            let before = ids[..last].iter().rev().find(|i| !gone(i));
            *self = after
                .or(before)
                .map_or_else(Self::default, |id| Self::one(id));
        }
    }

    /// Drop what is no longer listed.
    pub fn prune(&mut self, ids: &[String]) {
        self.selected.retain(|id| ids.contains(id));
        self.anchor.take_if(|a| !ids.contains(a));
        self.cursor.take_if(|c| !ids.contains(c));
    }

    /// The chosen notes, in list order.
    pub fn chosen(&self, ids: &[String]) -> Vec<String> {
        let chosen = ids.iter().filter(|id| self.selected.contains(*id));
        chosen.cloned().collect()
    }

    /// The note `e` edits, as web's `selection.cursor ?? selectedNotes[0]`: the cursor's, chosen or
    /// not, else the first chosen; none with nothing chosen.
    pub fn editing(&self, ids: &[String]) -> Option<String> {
        let chosen = self.chosen(ids);
        let cursor = self.cursor.clone().filter(|c| ids.contains(c));
        let first = chosen.first().cloned();
        (!chosen.is_empty()).then(|| cursor.or(first)).flatten()
    }
}

impl State {
    pub(super) fn new(cx: &mut App) -> Self {
        State {
            focus: cx.focus_handle(),
            focused: false,
            picked: Picked::default(),
            scroll: ScrollHandle::new(),
            armed: Vec::new(),
        }
    }

    /// Nothing chosen or armed: another agent's list.
    pub(super) fn clear(&mut self) {
        (self.picked, self.armed) = (Picked::default(), Vec::new());
    }

    pub(super) fn clear_armed(&mut self) {
        self.armed.clear();
    }

    /// Keep the cursor's card in view.
    fn reveal(&self, ids: &[String]) {
        let at = self.picked.cursor.as_ref();
        if let Some(i) = at.and_then(|c| ids.iter().position(|id| id == c)) {
            self.scroll.scroll_to_item(i);
        }
    }

    /// The selection after `removed` left `ids`, kept in view.
    fn remove(&mut self, ids: &[String], removed: &[String]) {
        self.picked.removed(ids, removed);
        let left = ids.iter().filter(|i| !removed.contains(i)).cloned();
        self.reveal(&left.collect::<Vec<_>>());
    }
}

/// Any key but a delete disarms one, bound or not: watched once each keystroke has resolved, since GPUI
/// runs an element's key listeners only for a key no binding took.
pub(super) fn disarm_on_keys<H: Host>(cx: &mut Context<H>) {
    cx.observe_keystrokes(|host: &mut H, e, _, cx| {
        let delete = e
            .action
            .as_ref()
            .is_some_and(|a| a.partial_eq(&List::Delete));
        let list = &mut host.parts().1.notes.list;
        if !delete && !list.armed.is_empty() {
            list.armed.clear();
            cx.notify();
        }
    })
    .detach();
}

/// Before a frame: drop what is no longer listed; focus gone from the list disarms its delete. Whether
/// the list holds focus.
pub(super) fn sync(list: &mut State, ids: &[String], window: &Window) -> bool {
    list.picked.prune(ids);
    list.focused = list.focus.is_focused(window);
    if !list.focused {
        list.armed.clear();
    }
    list.focused
}

/// What the strip says while a delete is armed, in place of its last confirmation.
pub fn armed(list: &State) -> Option<String> {
    let n = list.armed.len();
    (n > 0).then(|| format!("⌫ again to delete {}", count(n)))
}

/// `up` from the box: into the list, every note selected and the cursor where it was (else first).
pub fn enter(store: &Store, ui: &mut Ui) -> Vec<Event> {
    let Some(agent) = ui.zoomed_agent() else {
        return Vec::new();
    };
    let ids = ids(store, agent);
    if !ids.is_empty() {
        ui.notes.open = true;
        let list = &mut ui.notes.list;
        list.armed.clear();
        list.picked.all(&ids);
        list.reveal(&ids);
        ui.focus = Some(Focus::List);
    }
    Vec::new()
}

/// A list key, as web's `NotesList` handles it (any but a delete then disarms: `disarm_on_keys`).
pub fn keys(store: &Store, ui: &mut Ui, key: List) -> Vec<Event> {
    let Some(agent) = ui.zoomed_agent().map(String::from) else {
        return Vec::new();
    };
    let ids = ids(store, &agent);
    let list = &mut ui.notes.list;
    let chosen = list.picked.chosen(&ids);
    match key {
        List::Move(by, extend) => list.picked.step(&ids, by, extend),
        List::All => list.picked.all(&ids),
        // With nothing selected, `escape` leaves for the box (the zoom, when the box is read-only).
        List::Leave if chosen.is_empty() => ui.focus = Some(Focus::Box),
        List::Leave => list.picked = Picked::default(),
        _ if chosen.is_empty() => {}
        List::HandOff => return notes::hand_off(store, ui, agent, chosen),
        List::Delete => return delete(ui, &ids, chosen),
        List::Edit => {
            if let Some(id) = list.picked.editing(&ids) {
                return notes::edit(store, ui, &agent, &id);
            }
        }
    }
    ui.notes.list.reveal(&ids);
    Vec::new()
}

/// A click on a card, which takes focus into the list (as the pointer does).
pub fn act(store: &Store, ui: &mut Ui, card: &Card) -> Vec<Event> {
    let Some(agent) = ui.zoomed_agent().map(String::from) else {
        return Vec::new();
    };
    let ids = ids(store, &agent);
    ui.focus = Some(Focus::List);
    match card {
        Card::Pick { id, command, shift } => {
            let list = &mut ui.notes.list;
            list.armed.clear();
            list.picked.click(&ids, id, *command, *shift);
            Vec::new()
        }
        Card::Edit(id) => notes::edit(store, ui, &agent, id),
        Card::Delete(id) => delete(ui, &ids, vec![id.to_string()]),
    }
}

/// Delete `chosen` on the second press: the first arms it (`armed` says so).
fn delete(ui: &mut Ui, ids: &[String], chosen: Vec<String>) -> Vec<Event> {
    let list = &mut ui.notes.list;
    if list.armed != chosen {
        list.armed = chosen;
        return Vec::new();
    }
    list.armed.clear();
    list.remove(ids, &chosen);
    ui.notes.say(format!("Deleted {}.", count(chosen.len())));
    let stamp = notes::stamp();
    vec![Event::Note(Step::Delete { ids: chosen, stamp })]
}

/// `agent`'s hand-off landed and deleted `removed` from `order` (its list as the hand-off saw it): as
/// after a delete, the note after them is selected (web's `applyRemovalSelection`). A failed save
/// deletes nothing and never comes here; a note changed meanwhile stays, so is not among `removed`.
pub fn handed_off(ui: &mut Ui, agent: &str, order: &[String], removed: &[String]) {
    if ui.zoomed_agent() == Some(agent) {
        ui.notes.list.remove(order, removed);
    }
}

/// `agent`'s notes `picked` as web copies them (`noteTransferText`, a blank line apart), and the
/// confirmation; `None` with nothing picked.
pub fn copied(store: &Store, agent: &str, picked: &Picked) -> Option<(String, String)> {
    let notes: Vec<&Note> = store
        .notes_of(agent)
        .filter(|n| picked.selected.contains(&n.id))
        .collect();
    let text = notes.iter().map(|n| crate::store::notes::transfer_text(n));
    let text = text.collect::<Vec<_>>().join("\n\n");
    (!notes.is_empty()).then(|| (text, format!("Copied {}.", count(notes.len()))))
}

/// One note's card: its source, quote and text (or the editor, editing it), its ✕, and its state. A
/// card `kept` only for its open editor (its note left the list) takes no pick and has no ✕.
fn card(
    v: &notes::View,
    note: &Note,
    editing: Option<&Editing>,
    kept: bool,
    t: TypeScale,
) -> impl IntoElement {
    let list = &v.list;
    let id = SharedString::from(note.id.clone());
    let selected = list.picked.selected.contains(&note.id);
    let cursor = list.focused && list.picked.cursor.as_ref() == Some(&note.id);
    let armed = list.armed.contains(&note.id);
    let open = editing.is_some_and(|e| e.note.as_ref().is_some_and(|n| n.id == note.id));
    let border = match () {
        _ if armed => pal::AMBER,
        _ if cursor => pal::INK,
        _ if selected => pal::BLUE,
        _ => pal::EDGE,
    };
    let source = note.source.as_ref().map(|s| {
        let label = match s["kind"] == "transcript" {
            true => format!("Transcript: {}", s["agent"].as_str().unwrap_or("")),
            false => source_label(s),
        };
        dim(label).text_size(t.small).truncate()
    });
    let quote = note.quote.as_deref().map(|q| div().child(q.to_string()));
    let gap = note.quote.is_some() && (open || !note.text.is_empty());
    // The editor keeps the pointer: a click or a drag in it places the caret, never picks the card.
    let text = div().when(gap, |el| el.mt(t.px(6.)));
    let text = match open {
        true => text.child(
            div()
                .id("note-editor")
                .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                .child(notes::editor(v, None, t))
                .test_support(),
        ),
        false => text.child(note.text.clone()),
    };
    let content = div().flex_1().min_w_0().flex().flex_col();
    let content = content.children(source).children(quote).child(text);
    let delete = Card::Delete(id.clone());
    let x = div().id(ElementId::Name(format!("del-{id}").into()));
    let x = x
        .flex_none()
        .cursor_pointer()
        .text_size(t.small)
        .text_color(rgb(if armed { pal::AMBER } else { pal::SLATE }))
        .child(if armed { "delete?" } else { "✕" })
        // Its own click: the card under it does not pick (and so disarm) it too.
        .on_click(move |_, window, cx| {
            window.dispatch_action(delete.boxed_clone(), cx);
            cx.stop_propagation();
        })
        .test_support();
    let pick = move |e: &ClickEvent, window: &mut Window, cx: &mut App| {
        let (id, m) = (id.clone(), e.modifiers());
        let action = match e.click_count() {
            2.. => Card::Edit(id),
            _ => Card::Pick {
                id,
                command: m.platform,
                shift: m.shift,
            },
        };
        window.dispatch_action(action.boxed_clone(), cx);
    };
    // The accent bar of a note with a source (web's `anchored`).
    let bar = div().absolute().left_0().top_0().bottom_0().w(px(2.));
    let el = div().id(ElementId::Name(format!("note-{}", note.id).into()));
    el.relative()
        .flex_none()
        .flex()
        .gap(t.px(8.))
        .px(t.px(9.))
        .py(t.px(4.))
        .border_1()
        .border_color(rgb(border))
        .rounded(t.px(4.))
        .overflow_hidden()
        .bg(rgb(if selected { pal::SELECT } else { pal::PANEL }))
        .when(!selected && !armed && !cursor, |el| {
            el.hover(|s| s.border_color(rgb(pal::SLATE)))
        })
        .when(note.source.is_some(), |el| el.child(bar.bg(rgb(pal::BLUE))))
        .when(!kept, |el| el.cursor_pointer().on_click(pick))
        .child(content)
        .children((!kept).then_some(x))
        .test_support()
}

/// `agent`'s list, its `notes` shown when `open`, and the hint under it; the note being edited stays,
/// even once web deleted it or moved it to another agent (web's `noteEditDisplay`), so its text can
/// still be saved. `None` with nothing to list.
pub(super) fn render<H: Host>(
    store: &Store,
    ui: &Ui,
    agent: &str,
    editing: Option<&Editing>,
    open: bool,
    t: TypeScale,
    cx: &mut Context<H>,
) -> Option<Div> {
    let v = &ui.notes;
    let notes: Vec<&Note> = store.notes_of(agent).collect();
    let edited = editing.and_then(|e| e.note.as_ref());
    let kept = edited.filter(|n| !notes.iter().any(|m| m.id == n.id));
    if (!open || notes.is_empty()) && edited.is_none() {
        return None;
    }
    let cards = notes.iter().map(|n| card(v, n, editing, false, t));
    let cards = cards.chain(kept.map(|n| card(v, n, editing, true, t)));
    let list = div()
        .id("notes-list")
        .key_context("NotesList")
        .track_focus(&v.list.focus)
        .on_action(on(cx, |store, ui, key: &List| keys(store, ui, *key)))
        .on_action(cx.listener(|host: &mut H, _: &Copy, _, cx| {
            let (store, ui) = host.view();
            let agent = ui.zoomed_agent().unwrap_or("");
            if let Some((text, said)) = copied(store, agent, &ui.notes.list.picked) {
                host.copy(text, cx);
                let ui = host.parts().1;
                let before = ui.notes.said.as_ref().map(|s| s.0);
                ui.notes.say(said);
                notes::fade_later(ui, before, cx);
            }
            cx.notify();
        }))
        .flex()
        .flex_col()
        .gap(t.px(4.))
        .max_h(t.px(CAP))
        .overflow_y_scroll()
        .track_scroll(&v.list.scroll)
        .children(cards);
    let chosen = v.list.picked.selected.len();
    let keys = "⌘C copy · ⏎ composer · E edit · ⌫ delete · esc";
    let hint = (chosen > 0).then(|| {
        let n = div().text_color(rgb(pal::ACC)).child(chosen.to_string());
        let el = div().flex().gap(t.px(10.)).px(t.px(6.)).py(t.px(2.));
        let el = el.rounded(t.px(4.)).bg(rgb(pal::WASH)).text_size(t.small);
        el.child(n).child(dim(keys))
    });
    Some(
        div()
            .flex()
            .flex_col()
            .gap(t.px(4.))
            .child(list)
            .children(hint),
    )
}
