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

use crate::store::notes::{Stamp, Step};
use crate::store::{Event, Store};
use crate::views::lens::Ui;
use crate::views::theme::{TypeScale, pal};
use crate::views::{Host, dim};
use gpui_kit::component::input::{InputEvent, Textarea, TextareaState};
use gpui_kit::prelude::FluentBuilder as _;
use gpui_kit::*;
use std::sync::atomic::{AtomicU64, Ordering};

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
    /// The transcript selection when the pointer last let go, trimmed; what `c` captures.
    pub(super) selection: Option<String>,
    armed: Option<SharedString>,
    open: bool,
    /// The last action's focus request: `Some(true)` into the editor, `Some(false)` out of it.
    pub(super) want: Option<bool>,
}

struct Editing {
    agent: String,
    /// A note being edited, or a new one (with its captured quote).
    note: Option<SharedString>,
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
            armed: None,
            open: false,
            want: None,
        }
    }

    pub fn focus_handle(&self, cx: &App) -> FocusHandle {
        self.editor.read(cx).focus_handle(cx)
    }

    fn begin(
        &mut self,
        agent: &str,
        note: Option<SharedString>,
        quote: Option<String>,
        text: &str,
    ) {
        let agent = agent.to_string();
        self.editing = Some(Editing { agent, note, quote });
        self.load = Some(text.to_string());
        self.armed = None;
        self.want = Some(true);
    }

    fn close(&mut self) {
        self.editing = None;
        self.want = Some(false);
    }
}

/// The time and fresh ids for a note edit.
pub fn stamp() -> Stamp {
    let since = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH);
    let now = since.map_or(0, |d| d.as_millis() as i64);
    Stamp {
        now,
        id: uuid(),
        write: uuid(),
    }
}

/// A random UUID v4, as web's ids: std's randomly keyed hasher over a counter (unique, not secret).
fn uuid() -> String {
    use std::hash::BuildHasher;
    static N: AtomicU64 = AtomicU64::new(0);
    let (s, n) = (
        std::hash::RandomState::new(),
        N.fetch_add(1, Ordering::Relaxed),
    );
    let x = (u128::from(s.hash_one((n, 0))) << 64) | u128::from(s.hash_one((n, 1)));
    let x = (x & !(0xf << 76) | 0x4 << 76) & !(0x3 << 62) | 0x2 << 62;
    let h = format!("{x:032x}");
    format!(
        "{}-{}-{}-{}-{}",
        &h[..8],
        &h[8..12],
        &h[12..16],
        &h[16..20],
        &h[20..]
    )
}

/// Close what belonged to another agent, before a frame is drawn; load the editor's text.
pub fn sync(ui: &mut Ui, window: &mut Window, cx: &mut App) {
    let agent = ui.zoom.as_ref().and_then(|z| z.agent.clone());
    let notes = &mut ui.notes;
    if notes
        .editing
        .as_ref()
        .is_some_and(|e| Some(&e.agent) != agent.as_ref())
    {
        notes.editing = None;
        notes.selection = None;
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
    let agent = ui
        .zoom
        .as_ref()
        .and_then(|z| z.agent.as_deref())
        .unwrap_or("");
    let n = store.notes_of(agent).count();
    let editor = match &ui.notes.editing {
        None => "closed".to_string(),
        Some(_) if ui.notes.focus_handle(cx).is_focused(window) => {
            format!("focused:{}", ui.notes.text)
        }
        Some(_) => format!("idle:{}", ui.notes.text),
    };
    format!("{n}:{editor}")
}

/// What the pointer selected in the transcript, for the harness's `select:` (it cannot drag).
pub fn select(ui: &mut Ui, text: &str) {
    ui.notes.selection = Some(text.to_string());
}

pub fn act(store: &Store, ui: &mut Ui, key: &Notes) -> Vec<Event> {
    let Some(agent) = ui.zoom.as_ref().and_then(|z| z.agent.clone()) else {
        return Vec::new();
    };
    let notes = &mut ui.notes;
    let event = |step| vec![Event::Note(step)];
    match key {
        Notes::Add => notes.begin(&agent, None, None, ""),
        Notes::Capture => match notes.selection.take() {
            Some(quote) => notes.begin(&agent, None, Some(quote), ""),
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
            let Some(editing) = notes.editing.take() else {
                return Vec::new();
            };
            notes.want = Some(false);
            let (text, stamp) = (notes.text.clone(), stamp());
            return event(match editing.note {
                Some(id) => Step::Edit {
                    id: id.into(),
                    text,
                    stamp,
                },
                None => Step::Add {
                    group: agent,
                    text,
                    quote: editing.quote,
                    stamp,
                },
            });
        }
        Notes::Cancel => notes.close(),
        Notes::Toggle => notes.open = !notes.open,
        Notes::Edit(id) => {
            let note = store.notes.iter().find(|n| n.id == id.as_ref());
            if let Some(n) = note {
                notes.begin(&agent, Some(id.clone()), n.quote.clone(), &n.text);
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

/// The strip for `agent`: nothing while it has no notes, no editor open and no selection to capture.
pub fn render(store: &Store, ui: &Ui, agent: &str, t: TypeScale) -> Option<Div> {
    let v = &ui.notes;
    let notes: Vec<_> = store.notes_of(agent).collect();
    let editing = v.editing.as_ref().filter(|e| e.agent == agent);
    if notes.is_empty() && editing.is_none() && v.selection.is_none() {
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
        .when(n > 0, |h| {
            h.child(chip("handoff", "→ composer  p".into(), Notes::HandOff, t))
        })
        .children(v.selection.as_ref().map(|s| {
            chip("capture", format!("❝ {}  c", line(s)), Notes::Capture, t)
                .max_w(t.px(420.))
                .truncate()
        }));
    let rows = notes.into_iter().filter(|_| open).map(|note| {
        let id = SharedString::from(note.id.clone());
        let armed = v.armed.as_ref() == Some(&id);
        let on = editing.is_some_and(|e| e.note.as_ref() == Some(&id));
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
            .children(editor),
    )
}
