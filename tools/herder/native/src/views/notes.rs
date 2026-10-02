//! The notes strip (U5, F6): the zoomed agent's notes, right above the composer: a header (the count,
//! "Send all" and add), the list (`views::notes_list`, web's
//! keyboard list), the editor, the last action's confirmation and why anything was not saved. More than a
//! few notes collapse to their count until the list is entered. Notes are added, captured and edited in
//! one small editor (`Notes > Input`, so none of the composer's chords fire there), in the card of the
//! note it edits, and handed into the composer draft, the chosen ones or all. The records and their sync
//! are `store::notes`; the editor and the confirmation are view state here. A transcript selection is
//! noted where it was made (`views::capture`, F7).
//!
//! Keys (ARCHITECTURE §4). In the zoom: `a` add, `p` hand every note to the composer; the list's are in
//! `notes_list`. In the editor: `enter` or `cmd-enter` save, `shift-enter` a new line, `escape` cancel.
//! `alt-enter` in the composer queues its draft as a note (`views::composer`).

use crate::store::notes::{Note, Stamp, Step};
use crate::store::sync::{Hold, Ns};
use crate::store::{Event, Store};
use crate::views::lens::{Focus, Ui};
use crate::views::notes_list;
use crate::views::theme::{TypeScale, pal};
use crate::views::{Host, dim};
use gpui_kit::component::input::{InputEvent, Textarea, TextareaState};
use gpui_kit::prelude::FluentBuilder as _;
use gpui_kit::*;
use std::time::Duration;

#[derive(Clone, Debug, PartialEq, Action)]
#[action(namespace = notes, no_json)]
pub enum Notes {
    Add,
    /// `p` or "Send all": every note into the composer.
    HandOff,
    Save,
    Cancel,
    /// The count clicked: show every note, or collapse again.
    Toggle,
}

/// Where the editor's keys bind: its own Input, not the composer's.
pub const EDITOR: &str = "Notes > Input";
/// More notes than this collapse to their count.
const SHOWN: usize = 3;
/// How long a confirmation stays.
const SAID_FOR: Duration = Duration::from_secs(4);

pub struct View {
    editor: Entity<TextareaState>,
    /// The editor's text, mirrored as it changes so a save needs no window.
    pub(super) text: String,
    pub(super) editing: Option<Editing>,
    /// Text for the editor at the next render.
    load: Option<String>,
    /// Why the editor's text was not saved.
    problem: Option<&'static str>,
    pub(super) open: bool,
    pub(super) list: notes_list::State,
    /// The agent the strip (and its list) is on.
    agent: Option<String>,
    /// The last action's confirmation, numbered; it fades `SAID_FOR` later (`fade_later`).
    pub(super) said: Option<(u64, String)>,
    says: u64,
}

pub(super) struct Editing {
    pub(super) agent: String,
    /// A note being edited, as it was when the editor opened, or a new one (with its captured quote).
    pub(super) note: Option<Note>,
    quote: Option<String>,
    /// Where focus goes when it closes: the list it was opened from, or the zoom.
    back: Focus,
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
        notes_list::disarm_on_keys(cx);
        View {
            editor,
            text: String::new(),
            editing: None,
            load: None,
            problem: None,
            open: false,
            list: notes_list::State::new(cx),
            agent: None,
            said: None,
            says: 0,
        }
    }

    pub fn focus_handle(&self, cx: &App) -> FocusHandle {
        self.editor.read(cx).focus_handle(cx)
    }

    pub(super) fn say(&mut self, what: impl Into<String>) {
        self.says += 1;
        self.said = Some((self.says, what.into()));
    }

    /// The strip's confirmation line: an armed delete's prompt, else the last action's.
    pub fn said(&self) -> Option<String> {
        notes_list::armed(&self.list).or_else(|| self.said.as_ref().map(|s| s.1.clone()))
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
pub(super) fn ids(store: &Store, agent: &str) -> Vec<String> {
    store.notes_of(agent).map(|n| n.id.clone()).collect()
}

/// "1 note", "2 notes".
pub(super) fn count(n: usize) -> String {
    format!("{n} note{}", if n == 1 { "" } else { "s" })
}

/// Drop what belonged to another agent (the editor, the list's selection), before a frame is drawn; load the
/// editor's text. Focus left in a list that is no longer drawn goes back to the box (or the zoom).
pub fn sync(ui: &mut Ui, store: &Store, window: &mut Window, cx: &mut App) {
    let agent = ui.zoomed_agent().map(String::from);
    let ids = agent.as_deref().map_or_else(Vec::new, |a| ids(store, a));
    let notes = &mut ui.notes;
    if notes.agent != agent {
        (notes.agent, notes.said) = (agent.clone(), None);
        notes.list.clear();
    }
    let focused = notes_list::sync(&mut notes.list, &ids, window);
    let other = |a: &String| Some(a) != agent.as_ref();
    let mut leave = false;
    if notes.editing.as_ref().is_some_and(|e| other(&e.agent)) {
        notes.editing = None;
        notes.problem = None;
        leave = notes.focus_handle(cx).is_focused(window);
    }
    let shown = !ids.is_empty() && (ids.len() <= SHOWN || notes.open);
    if focused && !shown {
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
    notes.problem = None;
    notes.list.clear_armed();
    ui.focus = Some(Focus::Editor);
}

/// Edit `agent`'s note `id` in its card, from the list.
pub(super) fn edit(store: &Store, ui: &mut Ui, agent: &str, id: &str) -> Vec<Event> {
    if let Some(n) = store.notes.iter().find(|n| n.id == id) {
        begin(ui, agent, Some(n.clone()), n.quote.clone(), Focus::List);
    }
    Vec::new()
}

fn close(ui: &mut Ui) {
    let back = ui.notes.editing.take().map_or(Focus::Out, |e| e.back);
    ui.notes.problem = None;
    ui.focus = Some(back);
}

/// `agent`'s notes `ids` into its composer, which takes focus; refused while the box cannot take them.
pub(super) fn hand_off(store: &Store, ui: &mut Ui, agent: String, ids: Vec<String>) -> Vec<Event> {
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

pub fn act(store: &Store, ui: &mut Ui, key: &Notes) -> Vec<Event> {
    let Some(agent) = ui.zoomed_agent().map(String::from) else {
        return Vec::new();
    };
    let notes = &mut ui.notes;
    match key {
        Notes::Add => begin(ui, &agent, None, None, Focus::Out),
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
            return vec![Event::Note(step)];
        }
        Notes::Cancel => close(ui),
        Notes::Toggle => notes.open = !notes.open,
    }
    Vec::new()
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
pub(super) fn editor(v: &View, quote: Option<&str>, t: TypeScale) -> Div {
    let quote = quote.map(|q| dim(format!("❝ {}", line(q))).text_size(t.small));
    let input = div().key_context("Notes").child(Textarea::new(&v.editor));
    let hint = dim("⏎ save · ⇧⏎ new line · esc cancel").text_size(t.small);
    let el = div().flex().flex_col().gap(t.px(2.));
    el.children(quote).child(input).child(hint)
}

/// The strip for `agent`: nothing while it has no notes, no editor open and nothing to say.
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
    let said = v.said();
    let problems = problems(store, v, agent);
    let quiet = said.is_none() && problems.is_empty();
    if notes.is_empty() && editing.is_none() && quiet {
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
        .child(chip("add", "+ note  a".into(), Notes::Add, t));
    let list = notes_list::render(store, ui, agent, editing, open, t, cx);
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
            .children(list)
            .children(new.map(|e| editor(v, e.quote.as_deref(), t)))
            .children(said)
            .children(problems.into_iter().map(|why| {
                let line = div().text_size(t.small).text_color(rgb(pal::AMBER));
                line.child(why)
            })),
    )
}
