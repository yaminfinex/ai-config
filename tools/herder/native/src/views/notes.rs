//! The notes strip (U5, F6): the zoomed agent's notes, right above the composer, as web's keyboard list
//! (`notesListModel`): bordered cards with their full text, a source line and an accent bar on a quote,
//! a cursor and a selection, the list capped in height. More than a few collapse to a count until the
//! list is entered. Notes are added, captured and edited in one small editor (`Notes > Input`, so none
//! of the composer's chords fire there), deleted on a second press, and handed into the composer draft,
//! the chosen ones or all. The records and their sync are `store::notes`; the editor, the list's
//! selection, the last transcript selection and the confirmation line are view state here.
//!
//! Keys (ARCHITECTURE §4). In the zoom: `a` add (the transcript selection, if any, as its quote), `c`
//! capture the transcript selection, `p` hand every note to the composer. `up` from an empty box (or
//! its caret at the start) enters the list with every note selected. In the list (`NotesList`): `up`
//! `down` move, with `shift` extending; `cmd-a` all; `enter` the selection into the composer;
//! `backspace` twice deletes it; `e` edits the cursor's note in place; `cmd-c` copies; `escape` clears
//! the selection, then leaves for the box. A click picks a note (`cmd` toggles, `shift` a range), a
//! double-click edits it. In the editor: `enter` or `cmd-enter` save, `shift-enter` a new line,
//! `escape` cancel. `alt-enter` in the composer queues its draft as a note (`views::composer`).

use crate::store::notes::{Note, Stamp, Step, source_label};
use crate::store::sync::{Hold, Ns};
use crate::store::{Event, Store};
use crate::views::lens::{Focus, Ui};
use crate::views::theme::{TypeScale, pal};
use crate::views::{Host, dim, on};
use gpui_kit::component::input::{InputEvent, Textarea, TextareaState};
use gpui_kit::prelude::FluentBuilder as _;
use gpui_kit::*;
use std::collections::BTreeSet;
use std::time::Duration;

#[derive(Clone, Debug, PartialEq, Action)]
#[action(namespace = notes, no_json)]
pub enum Notes {
    Add,
    Capture,
    /// `p` or "Send all": every note into the composer.
    HandOff,
    Save,
    Cancel,
    /// The count clicked: show every note, or collapse again.
    Toggle,
    /// A double-click on a note: edit it in place.
    Edit(SharedString),
    /// The first press arms the note's ✕; the second deletes it.
    Delete(SharedString),
    /// A click on a note, `command` toggling it, `shift` picking the range from the anchor.
    Pick {
        id: SharedString,
        command: bool,
        shift: bool,
    },
}

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

// `Up`: `up` in the composer's box, into the list when the box is empty or its caret at the start (web's
// `composerArrowUpAction`), else the caret's move. `Copy`: `cmd-c` in the list. Each is its own action
// so nothing else handles it (`Up` falls through to the kit's caret move; `Copy` needs the clipboard).
actions!(notes, [Up, Copy]);

/// Where the editor's keys bind: its own Input, not the composer's.
pub const EDITOR: &str = "Notes > Input";
/// Where the list's bind: the list, not the editor open in one of its cards.
pub const LIST: &str = "NotesList && !Input";
/// More notes than this collapse to their count.
const SHOWN: usize = 3;
/// The list's height before it scrolls, web's 184 px in design pixels.
const CAP: f32 = 204.;
/// How long a confirmation stays.
const SAID_FOR: Duration = Duration::from_secs(4);

pub struct View {
    editor: Entity<TextareaState>,
    /// The editor's text, mirrored as it changes so a save needs no window.
    pub(super) text: String,
    pub(super) editing: Option<Editing>,
    /// Text for the editor at the next render.
    load: Option<String>,
    /// The transcript selection when the pointer last let go, trimmed, with the agent it was made on;
    /// what `c` captures and `a` quotes. Gone once the zoom leaves that agent.
    pub(super) selection: Option<(String, String)>,
    /// Why the editor's text was not saved.
    problem: Option<&'static str>,
    /// What a second delete removes: a note's ✕, or the list's selection on `backspace`.
    armed: Vec<String>,
    open: bool,
    pub(super) list: FocusHandle,
    /// Whether the list held focus at the last render (`sync`), for the cursor.
    focused: bool,
    pub(super) picked: Picked,
    scroll: ScrollHandle,
    /// The agent the list's selection is on.
    agent: Option<String>,
    /// The last action's confirmation, numbered; it fades `SAID_FOR` later (`fade_later`).
    pub(super) said: Option<(u64, String)>,
    says: u64,
}

pub(super) struct Editing {
    agent: String,
    /// A note being edited, as it was when the editor opened, or a new one (with its captured quote).
    note: Option<Note>,
    quote: Option<String>,
    /// Where focus goes when it closes: the list it was opened from, or the zoom.
    back: Focus,
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

    /// The notes `removed` were deleted from `ids`: if any was selected, the note after the last of
    /// them (else the one before) is selected alone. Otherwise `sync` prunes them at the next render.
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
}

impl View {
    pub fn new<H: Host>(window: &mut Window, cx: &mut Context<H>) -> Self {
        let editor = cx.new(|cx| {
            let s = TextareaState::new(window, cx).auto_grow(1, 6);
            s.placeholder("A note on this agent…")
        });
        cx.subscribe(&editor, |host: &mut H, state, event: &InputEvent, cx| {
            if let InputEvent::Change = event {
                host.parts().1.notes.text = state.read(cx).value().to_string();
            }
        })
        .detach();
        View {
            editor,
            text: String::new(),
            editing: None,
            load: None,
            selection: None,
            problem: None,
            armed: Vec::new(),
            open: false,
            list: cx.focus_handle(),
            focused: false,
            picked: Picked::default(),
            scroll: ScrollHandle::new(),
            agent: None,
            said: None,
            says: 0,
        }
    }

    pub fn focus_handle(&self, cx: &App) -> FocusHandle {
        self.editor.read(cx).focus_handle(cx)
    }

    /// The pointer let go on `agent`'s transcript with `text` selected (blank: none). Whether it changed.
    pub fn selected(&mut self, agent: Option<String>, text: &str) -> bool {
        let text = Some(text.trim()).filter(|t| !t.is_empty());
        let next = agent.zip(text.map(str::to_string));
        let changed = self.selection != next;
        self.selection = next;
        changed
    }

    fn say(&mut self, what: impl Into<String>) {
        self.says += 1;
        self.said = Some((self.says, what.into()));
    }

    /// Keep the cursor's card in view.
    fn reveal(&self, ids: &[String]) {
        let at = self.picked.cursor.as_ref();
        if let Some(i) = at.and_then(|c| ids.iter().position(|id| id == c)) {
            self.scroll.scroll_to_item(i);
        }
    }
}

/// The time and fresh ids for a note edit.
pub fn stamp() -> Stamp {
    let since = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH);
    let now = since.map_or(0, |d| d.as_millis() as i64);
    let uuid = || uuid::Uuid::new_v4().to_string();
    Stamp {
        now,
        id: uuid(),
        write: uuid(),
    }
}

/// `agent`'s note ids, in list order.
fn ids(store: &Store, agent: &str) -> Vec<String> {
    store.notes_of(agent).map(|n| n.id.clone()).collect()
}

/// "1 note", "2 notes".
fn count(n: usize) -> String {
    format!("{n} note{}", if n == 1 { "" } else { "s" })
}

/// Drop what belonged to another agent (the editor, the selections), before a frame is drawn; load the
/// editor's text. Focus left in a list that is no longer drawn goes back to the box (or the zoom).
pub fn sync(ui: &mut Ui, store: &Store, window: &mut Window, cx: &mut App) {
    let agent = ui.zoomed_agent().map(String::from);
    let ids = agent.as_deref().map_or_else(Vec::new, |a| ids(store, a));
    let notes = &mut ui.notes;
    if notes.agent != agent {
        (notes.agent, notes.picked) = (agent.clone(), Picked::default());
        (notes.armed, notes.said) = (Vec::new(), None);
    }
    notes.picked.prune(&ids);
    let other = |a: &String| Some(a) != agent.as_ref();
    if notes.selection.as_ref().is_some_and(|(a, _)| other(a)) {
        notes.selection = None;
    }
    let mut leave = false;
    if notes.editing.as_ref().is_some_and(|e| other(&e.agent)) {
        notes.editing = None;
        notes.problem = None;
        leave = notes.focus_handle(cx).is_focused(window);
    }
    let shown = !ids.is_empty() && (ids.len() <= SHOWN || notes.open);
    notes.focused = notes.list.is_focused(window);
    if notes.focused && !shown {
        let writable = agent.is_some_and(|a| store.can_send(&a).is_ok());
        match writable {
            true => window.focus(&ui.composer.focus_handle(cx), cx),
            false => leave = true,
        }
    }
    if leave {
        window.focus(&ui.zoom_focus, cx);
    }
    if let Some(text) = ui.notes.load.take() {
        ui.notes.text = text.clone();
        ui.notes
            .editor
            .update(cx, |s, cx| s.set_value(text, window, cx));
    }
}

/// A confirmation said since `before` (the previous one's number) fades `SAID_FOR` later.
pub(super) fn fade_later<H: Host>(ui: &Ui, before: Option<u64>, cx: &mut Context<H>) {
    let Some(seq) = ui
        .notes
        .said
        .as_ref()
        .map(|s| s.0)
        .filter(|s| Some(*s) != before)
    else {
        return;
    };
    cx.spawn(async move |host, cx| {
        cx.background_executor().timer(SAID_FOR).await;
        host.update(cx, |host, cx| {
            let notes = &mut host.parts().1.notes;
            if notes.said.take_if(|s| s.0 == seq).is_some() {
                cx.notify();
            }
        })
    })
    .detach();
}

/// Open the editor on `agent`'s `note` (a new one: `None`, with its captured `quote`), and focus it.
fn begin(ui: &mut Ui, agent: &str, note: Option<Note>, quote: Option<String>, back: Focus) {
    let text = note.as_ref().map_or(String::new(), |n| n.text.clone());
    let agent = agent.to_string();
    let notes = &mut ui.notes;
    notes.editing = Some(Editing {
        agent,
        note,
        quote,
        back,
    });
    notes.load = Some(text);
    (notes.armed, notes.problem) = (Vec::new(), None);
    ui.focus = Some(Focus::Editor);
}

fn close(ui: &mut Ui) {
    let back = ui.notes.editing.take().map_or(Focus::Out, |e| e.back);
    ui.notes.problem = None;
    ui.focus = Some(back);
}

/// `agent`'s notes `ids` into its composer, which takes focus; refused while the box cannot take them.
fn hand_off(store: &Store, ui: &mut Ui, agent: String, ids: Vec<String>) -> Vec<Event> {
    if ids.is_empty() {
        return Vec::new();
    }
    if store.hand_off_blocked(&agent) {
        ui.notes.say("The composer cannot take notes now.");
        return Vec::new();
    }
    let moved = format!("Moved {} to {agent}’s composer.", count(ids.len()));
    ui.notes.say(moved);
    ui.focus = Some(Focus::Box);
    let stamp = stamp();
    vec![Event::Note(Step::HandOff { agent, ids, stamp })]
}

/// Delete `chosen` on the second press: the first arms it and says so.
fn delete(ui: &mut Ui, ids: &[String], chosen: Vec<String>) -> Vec<Event> {
    let notes = &mut ui.notes;
    if notes.armed != chosen {
        notes.say(format!("⌫ again to delete {}", count(chosen.len())));
        notes.armed = chosen;
        return Vec::new();
    }
    notes.armed.clear();
    notes.picked.removed(ids, &chosen);
    notes.reveal(ids);
    notes.say(format!("Deleted {}.", count(chosen.len())));
    let stamp = stamp();
    vec![Event::Note(Step::Delete { ids: chosen, stamp })]
}

/// `up` from the box: into the list, every note selected and the cursor where it was (else first).
pub fn enter(store: &Store, ui: &mut Ui) -> Vec<Event> {
    let Some(agent) = ui.zoomed_agent() else {
        return Vec::new();
    };
    let ids = ids(store, agent);
    if !ids.is_empty() {
        let notes = &mut ui.notes;
        (notes.open, notes.armed) = (true, Vec::new());
        notes.picked.all(&ids);
        notes.reveal(&ids);
        ui.focus = Some(Focus::List);
    }
    Vec::new()
}

pub fn act(store: &Store, ui: &mut Ui, key: &Notes) -> Vec<Event> {
    let Some(agent) = ui.zoomed_agent().map(String::from) else {
        return Vec::new();
    };
    let notes = &mut ui.notes;
    let event = |step| vec![Event::Note(step)];
    match key {
        Notes::Add => {
            let quote = notes.selection.take_if(|(a, _)| *a == agent).map(|s| s.1);
            begin(ui, &agent, None, quote, Focus::Out);
        }
        Notes::Capture => match notes.selection.take_if(|(a, _)| *a == agent) {
            Some((_, quote)) => begin(ui, &agent, None, Some(quote), Focus::Out),
            None => return Vec::new(),
        },
        Notes::HandOff => return hand_off(store, ui, agent.clone(), ids(store, &agent)),
        Notes::Save => {
            let Some(editing) = &notes.editing else {
                return Vec::new();
            };
            let (text, stamp) = (notes.text.clone(), stamp());
            let step = match &editing.note {
                Some(original) => Step::Edit {
                    original: original.clone(),
                    text,
                    stamp,
                },
                None => Step::Add {
                    group: agent,
                    text,
                    quote: editing.quote.clone(),
                    stamp,
                },
            };
            // Refused, the editor keeps its text and says why; saved, it closes.
            notes.problem = store.refusal(&step);
            if notes.problem.is_some() {
                return Vec::new();
            }
            notes.say("Saved.");
            close(ui);
            return event(step);
        }
        Notes::Cancel => close(ui),
        Notes::Toggle => notes.open = !notes.open,
        Notes::Edit(id) => {
            let note = store.notes.iter().find(|n| n.id == id.as_ref());
            if let Some(n) = note {
                begin(ui, &agent, Some(n.clone()), n.quote.clone(), Focus::List);
            }
        }
        Notes::Delete(id) => return delete(ui, &ids(store, &agent), vec![id.to_string()]),
        Notes::Pick { id, command, shift } => {
            notes
                .picked
                .click(&ids(store, &agent), id, *command, *shift);
            notes.armed.clear();
            ui.focus = Some(Focus::List);
        }
    }
    Vec::new()
}

/// A list key, as web's `NotesList` handles it.
pub fn list(store: &Store, ui: &mut Ui, key: List) -> Vec<Event> {
    let Some(agent) = ui.zoomed_agent().map(String::from) else {
        return Vec::new();
    };
    let ids = ids(store, &agent);
    let notes = &mut ui.notes;
    let chosen = notes.picked.chosen(&ids);
    if key != List::Delete {
        notes.armed.clear();
    }
    match key {
        List::Move(by, extend) => notes.picked.step(&ids, by, extend),
        List::All => notes.picked.all(&ids),
        // With nothing selected, `escape` leaves for the box (the zoom, when the box is read-only).
        List::Leave if chosen.is_empty() => ui.focus = Some(Focus::Box),
        List::Leave => notes.picked = Picked::default(),
        _ if chosen.is_empty() => {}
        List::HandOff => return hand_off(store, ui, agent, chosen),
        List::Delete => return delete(ui, &ids, chosen),
        List::Edit => {
            let at = notes.picked.cursor.clone().filter(|c| chosen.contains(c));
            let id = at.unwrap_or_else(|| chosen[0].clone());
            return act(store, ui, &Notes::Edit(id.into()));
        }
    }
    ui.notes.reveal(&ids);
    Vec::new()
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

/// A click that dispatches `action` through the focused element, like a key.
fn click(el: Stateful<Div>, action: Notes) -> Stateful<Div> {
    el.cursor_pointer()
        .on_click(move |_, window, cx| window.dispatch_action(action.boxed_clone(), cx))
}

fn chip(id: impl Into<ElementId>, label: String, action: Notes, t: TypeScale) -> Stateful<Div> {
    let el = div()
        .id(id)
        .px(t.px(6.))
        .rounded(t.px(4.))
        .border_1()
        .border_color(rgb(pal::EDGE))
        .hover(|s| s.border_color(rgb(pal::SLATE)));
    click(el.text_color(rgb(pal::SLATE)).child(label), action)
}

/// The first line of `s`, shortened for one row.
fn line(s: &str) -> String {
    let first = s.lines().next().unwrap_or("");
    let more = s.contains('\n') || first.chars().count() > 120;
    let cut: String = first.chars().take(120).collect();
    if more { format!("{cut}…") } else { cut }
}

/// Why notes are not being saved or synced, for `agent`'s strip: its editor's refusal, its last
/// transfer's, and a notes sync the server is holding back.
fn problems(store: &Store, v: &View, agent: &str) -> Vec<String> {
    let hold = store.sync[&Ns::Notes].hold.map(|h| match h {
        Hold::TooLarge => {
            "The server refused the notes as too large (413); unsent notes wait for the next edit."
        }
        Hold::LocalOnly => "Notes stay on this Mac: the server refused its attribution (409).",
    });
    let editor = v
        .problem
        .filter(|_| v.editing.as_ref().is_some_and(|e| e.agent == agent));
    let last = store.note_problems.get(agent).map(String::as_str);
    [editor, last, hold]
        .into_iter()
        .flatten()
        .map(str::to_string)
        .collect()
}

/// The editor: its quote, the box and its keys.
fn editor(v: &View, quote: Option<&str>, t: TypeScale) -> Div {
    let quote = quote.map(|q| dim(format!("❝ {}", line(q))).text_size(t.small));
    let input = div().key_context("Notes").child(Textarea::new(&v.editor));
    let hint = dim("⏎ save · ⇧⏎ new line · esc cancel").text_size(t.small);
    let el = div().flex().flex_col().gap(t.px(2.));
    el.children(quote).child(input).child(hint)
}

/// One note's card: its source, quote and text (or the editor, editing it), its ✕, and its state.
fn card(v: &View, note: &Note, editing: Option<&Editing>, t: TypeScale) -> Stateful<Div> {
    let id = SharedString::from(note.id.clone());
    let selected = v.picked.selected.contains(&note.id);
    let cursor = v.focused && v.picked.cursor.as_ref() == Some(&note.id);
    let armed = v.armed.contains(&note.id);
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
    let text = match open {
        true => editor(v, None, t),
        false => div().child(note.text.clone()),
    };
    let content = div().flex_1().min_w_0().flex().flex_col();
    let content = content
        .children(source)
        .children(quote)
        .child(text.when(gap, |el| el.mt(t.px(6.))));
    let x = div().id(ElementId::Name(format!("del-{id}").into()));
    let x = click(x.flex_none(), Notes::Delete(id.clone()))
        .text_size(t.small)
        .text_color(rgb(if armed { pal::AMBER } else { pal::SLATE }))
        .child(if armed { "delete?" } else { "✕" });
    let pick = move |e: &ClickEvent, window: &mut Window, cx: &mut App| {
        let (id, m) = (id.clone(), e.modifiers());
        let action = match e.click_count() {
            2.. => Notes::Edit(id),
            _ => Notes::Pick {
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
        .on_click(pick)
        .child(content)
        .child(x)
}

/// The strip for `agent`: nothing while it has no notes, no editor open, no selection to capture and
/// nothing to say.
pub fn render<H: Host>(
    store: &Store,
    ui: &Ui,
    agent: &str,
    t: TypeScale,
    cx: &mut Context<H>,
) -> Option<Div> {
    let v = &ui.notes;
    let notes: Vec<_> = store.notes_of(agent).collect();
    let editing = v.editing.as_ref().filter(|e| e.agent == agent);
    let selection = v.selection.as_ref().filter(|(a, _)| a == agent);
    let said = v.said.as_ref().map(|s| s.1.clone());
    let problems = problems(store, v, agent);
    let quiet = said.is_none() && problems.is_empty();
    if notes.is_empty() && editing.is_none() && selection.is_none() && quiet {
        return None;
    }
    let (n, open) = (notes.len(), notes.len() <= SHOWN || v.open);
    let fold = if open { "▾" } else { "▸" };
    let pill = div().px(t.px(5.)).rounded(t.px(6.)).bg(rgb(pal::CHIP));
    let count = div().id("notes-count").flex().gap(t.px(6.));
    let count = count
        .child(format!("{fold} Notes"))
        .child(pill.child(n.to_string()));
    let count = match n > SHOWN {
        true => click(count, Notes::Toggle),
        false => count,
    };
    let head = div().flex().items_center().gap(t.px(8.)).text_size(t.small);
    let head = head
        .child(count.text_color(rgb(pal::SLATE)))
        // Unavailable (the box is read-only or busy; it says why), shown as such rather than hidden.
        .when(n > 0, |h| match store.hand_off_blocked(agent) {
            true => h.child(dim("Send all unavailable")),
            false => h.child(chip("sendall", "Send all  p".into(), Notes::HandOff, t)),
        })
        .child(chip("add", "+ note  a".into(), Notes::Add, t))
        .children(selection.map(|(_, s)| {
            chip("capture", format!("❝ {}  c", line(s)), Notes::Capture, t)
                .max_w(t.px(420.))
                .truncate()
        }));
    let cards = notes
        .iter()
        .filter(|_| open)
        .map(|n| card(v, n, editing, t));
    let list = div()
        .id("notes-list")
        .key_context("NotesList")
        .track_focus(&v.list)
        .on_action(on(cx, |store, ui, key: &List| list(store, ui, *key)))
        .on_action(cx.listener(|host: &mut H, _: &Copy, _, cx| {
            let (store, ui) = host.view();
            let agent = ui.zoomed_agent().unwrap_or("");
            if let Some((text, said)) = copied(store, agent, &ui.notes.picked) {
                host.copy(text, cx);
                let ui = host.parts().1;
                let before = ui.notes.said.as_ref().map(|s| s.0);
                ui.notes.say(said);
                fade_later(ui, before, cx);
                cx.notify();
            }
        }))
        .flex()
        .flex_col()
        .gap(t.px(4.))
        .max_h(t.px(CAP))
        .overflow_y_scroll()
        .track_scroll(&v.scroll)
        .children(cards);
    let chosen = v.picked.selected.len();
    let keys = "⌘C copy · ⏎ composer · E edit · ⌫ delete · esc";
    let hint = (chosen > 0).then(|| {
        let n = div().text_color(rgb(pal::ACC)).child(chosen.to_string());
        let el = div().flex().gap(t.px(10.)).px(t.px(6.)).py(t.px(2.));
        let el = el.rounded(t.px(4.)).bg(rgb(pal::WASH)).text_size(t.small);
        el.child(n).child(dim(keys))
    });
    let new = editing.filter(|e| e.note.is_none());
    let said = said.map(|s| dim(s).text_size(t.small).text_color(rgb(pal::INK)));
    let strip = div()
        .flex_none()
        .flex()
        .flex_col()
        .gap(t.px(4.))
        .px(t.px(20.))
        .py(t.px(6.))
        .bg(rgb(pal::SIDEBAR));
    Some(
        strip
            .border_t_1()
            .border_color(rgb(pal::RULE))
            .child(head)
            .when(open && n > 0, |s| s.child(list))
            .children(hint)
            .children(new.map(|e| editor(v, e.quote.as_deref(), t)))
            .children(said)
            .children(problems.into_iter().map(|why| {
                let line = div().text_size(t.small).text_color(rgb(pal::AMBER));
                line.child(why)
            })),
    )
}
