//! The notes strip (U5): the zoomed agent's notes, right above the composer. More than a few collapse
//! to a count. Notes are added, captured and edited in one small editor (`Notes > Input`, so none of
//! the composer's chords fire there), deleted with a second click, and handed into the composer draft
//! whole. The records and their sync are `store::notes`; the editor, the open note and the last
//! transcript selection are view state here.
//!
//! Keys (ARCHITECTURE §4), in the zoom: `a` add, `c` capture the transcript selection (made with the
//! pointer; the strip offers it as a chip too), `p` hand every note to the composer. In the editor:
//! `enter` or `cmd-enter` save, `shift-enter` a new line, `escape` cancel. `alt-enter` in the composer
//! queues its draft as a note (`views::composer`).

use crate::store::notes::{Note, Stamp, Step};
use crate::store::sync::{Hold, Ns};
use crate::store::{Event, Store};
use crate::views::lens::Ui;
use crate::views::theme::{TypeScale, pal};
use crate::views::{Host, dim};
use gpui_kit::component::input::{InputEvent, Textarea, TextareaState};
use gpui_kit::prelude::FluentBuilder as _;
use gpui_kit::*;

#[derive(Clone, Debug, PartialEq, Action)]
#[action(namespace = notes, no_json)]
pub enum Notes {
    Add,
    Capture,
    HandOff,
    Save,
    Cancel,
    /// The count clicked: show every note, or collapse again.
    Toggle,
    Edit(SharedString),
    /// The first press arms the note's ✕; the second deletes it.
    Delete(SharedString),
}

/// Where the editor's keys bind: its own Input, not the composer's.
pub const EDITOR: &str = "Notes > Input";
/// More notes than this collapse to their count.
const SHOWN: usize = 3;

pub struct View {
    editor: Entity<TextareaState>,
    /// The editor's text, mirrored as it changes so a save needs no window.
    text: String,
    editing: Option<Editing>,
    /// Text for the editor at the next render.
    load: Option<String>,
    /// The transcript selection when the pointer last let go, trimmed, with the agent it was made on;
    /// what `c` captures. Gone once the zoom leaves that agent.
    selection: Option<(String, String)>,
    /// Why the editor's text was not saved.
    problem: Option<&'static str>,
    armed: Option<SharedString>,
    open: bool,
    /// The last action's focus request: `Some(true)` into the editor, `Some(false)` out of it.
    pub(super) want: Option<bool>,
}

struct Editing {
    agent: String,
    /// A note being edited, as it was when the editor opened, or a new one (with its captured quote).
    note: Option<Note>,
    quote: Option<String>,
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
            armed: None,
            open: false,
            want: None,
        }
    }

    pub fn focus_handle(&self, cx: &App) -> FocusHandle {
        self.editor.read(cx).focus_handle(cx)
    }

    fn begin(&mut self, agent: &str, note: Option<Note>, quote: Option<String>) {
        let text = note.as_ref().map_or(String::new(), |n| n.text.clone());
        let agent = agent.to_string();
        self.editing = Some(Editing { agent, note, quote });
        self.load = Some(text);
        self.armed = None;
        self.problem = None;
        self.want = Some(true);
    }

    fn close(&mut self) {
        self.editing = None;
        self.problem = None;
        self.want = Some(false);
    }

    /// The pointer let go on `agent`'s transcript with `text` selected (blank: none). Whether it changed.
    pub fn selected(&mut self, agent: Option<String>, text: &str) -> bool {
        let text = Some(text.trim()).filter(|t| !t.is_empty());
        let next = agent.zip(text.map(str::to_string));
        let changed = self.selection != next;
        self.selection = next;
        changed
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

/// Drop what belonged to another agent (the editor, the selection), before a frame is drawn; load the
/// editor's text.
pub fn sync(ui: &mut Ui, window: &mut Window, cx: &mut App) {
    let agent = ui.zoomed_agent().map(String::from);
    let notes = &mut ui.notes;
    let other = |a: &String| Some(a) != agent.as_ref();
    if notes.selection.as_ref().is_some_and(|(a, _)| other(a)) {
        notes.selection = None;
    }
    if notes.editing.as_ref().is_some_and(|e| other(&e.agent)) {
        notes.editing = None;
        notes.problem = None;
        if notes.focus_handle(cx).is_focused(window) {
            window.focus(&ui.zoom_focus, cx);
        }
    }
    if let Some(text) = ui.notes.load.take() {
        ui.notes.text = text.clone();
        ui.notes
            .editor
            .update(cx, |s, cx| s.set_value(text, window, cx));
    }
}

/// `notes:` for the harness: the zoomed agent's note count, then the editor (`closed`, or its focus and
/// text).
pub fn probe(store: &Store, ui: &Ui, window: &Window, cx: &App) -> String {
    let n = store.notes_of(ui.zoomed_agent().unwrap_or("")).count();
    let editor = match &ui.notes.editing {
        None => "closed".to_string(),
        Some(_) if ui.notes.focus_handle(cx).is_focused(window) => {
            format!("focused:{}", ui.notes.text)
        }
        Some(_) => format!("idle:{}", ui.notes.text),
    };
    format!("{n}:{editor}")
}

/// What the pointer selected in the zoomed transcript, for the harness's `select:` (it cannot drag).
pub fn select(ui: &mut Ui, text: &str) {
    let agent = ui.zoomed_agent().map(String::from);
    ui.notes.selected(agent, text);
}

/// What a click on the strip dispatches, for the harness's `click:` (`capture`, `handoff`, or `edit:i`
/// and `delete:i` on the zoomed agent's note `i`, oldest first); `None` where the strip has no such thing.
pub fn clicked(store: &Store, ui: &Ui, what: &str) -> Option<Notes> {
    let agent = ui.zoomed_agent()?;
    let note = |i: &str| {
        let note = store.notes_of(agent).nth(i.parse().ok()?)?;
        Some(SharedString::from(note.id.clone()))
    };
    match what.split_once(':').unwrap_or((what, "")) {
        ("capture", _) => {
            ui.notes.selection.as_ref().filter(|(a, _)| a == agent)?;
            Some(Notes::Capture)
        }
        ("handoff", _) => {
            let shown = note("0").is_some() && !store.hand_off_blocked(agent);
            shown.then_some(Notes::HandOff)
        }
        ("edit", i) => note(i).map(Notes::Edit),
        ("delete", i) => note(i).map(Notes::Delete),
        _ => None,
    }
}

pub fn act(store: &Store, ui: &mut Ui, key: &Notes) -> Vec<Event> {
    let Some(agent) = ui.zoomed_agent().map(String::from) else {
        return Vec::new();
    };
    let notes = &mut ui.notes;
    let event = |step| vec![Event::Note(step)];
    match key {
        Notes::Add => notes.begin(&agent, None, None),
        Notes::Capture => match notes.selection.take_if(|(a, _)| *a == agent) {
            Some((_, quote)) => notes.begin(&agent, None, Some(quote)),
            None => return Vec::new(),
        },
        Notes::HandOff => {
            ui.composer.want = Some(store.can_send(&agent).is_ok());
            return event(Step::HandOff {
                agent,
                stamp: stamp(),
            });
        }
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
            notes.close();
            return event(step);
        }
        Notes::Cancel => notes.close(),
        Notes::Toggle => notes.open = !notes.open,
        Notes::Edit(id) => {
            let note = store.notes.iter().find(|n| n.id == id.as_ref());
            if let Some(n) = note {
                notes.begin(&agent, Some(n.clone()), n.quote.clone());
            }
        }
        Notes::Delete(id) if notes.armed.as_ref() == Some(id) => {
            notes.armed = None;
            return event(Step::Delete {
                id: id.to_string(),
                stamp: stamp(),
            });
        }
        Notes::Delete(id) => notes.armed = Some(id.clone()),
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
        .bg(rgb(pal::WASH));
    click(el.text_color(rgb(pal::ACC)).child(label), action)
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
    let hold = match store.sync[&Ns::Notes].hold {
        Some(Hold::TooLarge) => Some(
            "The server refused the notes as too large (413); unsent notes wait for the next edit.",
        ),
        Some(Hold::LocalOnly) => {
            Some("Notes stay on this Mac: the server refused its attribution (409).")
        }
        None => None,
    };
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

/// The strip for `agent`: nothing while it has no notes, no editor open, no selection to capture and
/// nothing to say.
pub fn render(store: &Store, ui: &Ui, agent: &str, t: TypeScale) -> Option<Div> {
    let v = &ui.notes;
    let notes: Vec<_> = store.notes_of(agent).collect();
    let editing = v.editing.as_ref().filter(|e| e.agent == agent);
    let selection = v.selection.as_ref().filter(|(a, _)| a == agent);
    let problems = problems(store, v, agent);
    if notes.is_empty() && editing.is_none() && selection.is_none() && problems.is_empty() {
        return None;
    }
    let (n, open) = (notes.len(), notes.len() <= SHOWN || v.open);
    let count = format!(
        "{n} note{} {}",
        if n == 1 { "" } else { "s" },
        if open { "▾" } else { "▸" }
    );
    let count = match n > SHOWN {
        true => click(div().id("notes-count"), Notes::Toggle).child(count),
        false => div().id("notes-count").child(count),
    };
    let head = div()
        .flex()
        .items_center()
        .gap(t.px(10.))
        .text_size(t.small);
    let head = head
        .child(count.text_color(rgb(pal::SLATE)))
        .when(n > 0 && !store.hand_off_blocked(agent), |h| {
            h.child(chip("handoff", "→ composer  p".into(), Notes::HandOff, t))
        })
        // Unavailable (the box is read-only or busy; it says why), shown as such rather than hidden.
        .when(n > 0 && store.hand_off_blocked(agent), |h| {
            h.child(dim("→ composer unavailable"))
        })
        .children(selection.map(|(_, s)| {
            chip("capture", format!("❝ {}  c", line(s)), Notes::Capture, t)
                .max_w(t.px(420.))
                .truncate()
        }));
    let rows = notes.into_iter().filter(|_| open).map(|note| {
        let id = SharedString::from(note.id.clone());
        let armed = v.armed.as_ref() == Some(&id);
        let on = editing.is_some_and(|e| e.note.as_ref().is_some_and(|n| n.id == note.id));
        let quote = note.quote.as_deref().map(|q| {
            dim(format!("❝ {}", line(q)))
                .flex_none()
                .max_w(t.px(360.))
                .truncate()
        });
        let text = div()
            .id(ElementId::Name(format!("note-{id}").into()))
            .flex_1()
            .min_w_0()
            .truncate();
        let text = click(text, Notes::Edit(id.clone())).child(line(&note.text));
        let x = div()
            .id(ElementId::Name(format!("del-{id}").into()))
            .flex_none();
        let x = click(x, Notes::Delete(id)).text_color(rgb(if armed {
            pal::AMBER
        } else {
            pal::SLATE
        }));
        div()
            .flex()
            .items_center()
            .gap(t.px(8.))
            .px(t.px(6.))
            .rounded(t.px(4.))
            .when(on, |r| r.bg(rgb(pal::WASH)))
            .children(quote)
            .child(text)
            .child(x.child(if armed { "delete?" } else { "✕" }))
    });
    let editor = editing.map(|e| {
        let quote = e
            .quote
            .as_deref()
            .map(|q| dim(format!("❝ {}", line(q))).text_size(t.small));
        let input = div().key_context("Notes").child(Textarea::new(&v.editor));
        let hint = dim("⏎ save · ⇧⏎ new line · esc cancel").text_size(t.small);
        div()
            .flex()
            .flex_col()
            .gap(t.px(2.))
            .children(quote)
            .child(input)
            .child(hint)
    });
    let strip = div()
        .flex_none()
        .flex()
        .flex_col()
        .gap(t.px(3.))
        .px(t.px(20.))
        .pt(t.px(6.));
    Some(
        strip
            .border_t_1()
            .border_color(rgb(pal::RULE))
            .child(head)
            .children(rows)
            .children(editor)
            .children(problems.into_iter().map(|why| {
                let line = div().text_size(t.small).text_color(rgb(pal::AMBER));
                line.child(why)
            })),
    )
}
