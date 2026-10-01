//! Notes (U5): per agent, the things the owner wants to say to it or remember about it.
//!
//! Notes are the `notes` namespace rows web writes: per agent (`group`, or `general`, which the strip
//! leaves to web's rail), with an optional quote and source. Every edit here becomes a full row version
//! for `store::sync` (its outbox, LWW and tombstones), in web's record shape (`storedNoteToStateRow`), so
//! both clients read each other's notes. A version is `max(now, previous + 1)`, as web's.
//!
//! The hand-off follows web's `noteHandOff`: the chosen notes (the list's selection, or all of them) are
//! appended to the composer draft (`\n\n` between them, each as web's `noteTransferText`) and deleted,
//! not when a message lands; what the
//! owner then sends from it is theirs to edit. A hand-off or a queued draft is a `Transfer`: the
//! destination (the draft's prefs, the note's outbox) is saved first, and only then is the source
//! deleted or cleared. Adds, edits and queues over web's 8 KiB are refused in web's words.

use super::sync::{Ns, Step as SyncStep};
use super::{Effect, Persist, Store};
use crate::api::{NoteValue, StateRow};
use serde_json::{Value, json};
use std::collections::BTreeMap;

#[derive(Clone, Debug, PartialEq)]
pub struct Note {
    pub id: String,
    /// An agent name, or `general`.
    pub group: String,
    pub text: String,
    pub quote: Option<String>,
    pub source: Option<Value>,
    pub created: i64,
    pub updated: i64,
}

/// The live notes, oldest first. A row whose value does not decode is skipped.
pub fn derive(rows: &BTreeMap<String, StateRow>) -> Vec<Note> {
    let mut out: Vec<Note> = rows
        .values()
        .filter(|r| !r.deleted)
        .filter_map(|r| {
            let v: NoteValue = serde_json::from_value(r.value.clone()).ok()?;
            Some(Note {
                id: r.key.clone(),
                group: v.group,
                text: v.text,
                quote: v.quote,
                source: v.source,
                created: v.created,
                updated: r.updated,
            })
        })
        .collect();
    out.sort_by(|a, b| (a.created, &a.id).cmp(&(b.created, &b.id)));
    out
}

/// The time of day and fresh ids for a local edit; the caller makes them, as the store reads no clock.
#[derive(Clone, Debug, PartialEq)]
pub struct Stamp {
    /// Milliseconds since the epoch.
    pub now: i64,
    /// A new note's id.
    pub id: String,
    /// The `writeID` of every row this edit writes.
    pub write: String,
}

#[derive(Clone, Debug)]
pub enum Step {
    /// A new note on agent `group`: typed text, or a quote captured from its transcript with an
    /// optional comment (`source` is then that transcript, as web's capture).
    Add {
        group: String,
        text: String,
        quote: Option<String>,
        stamp: Stamp,
    },
    /// New text for the note `original` was when the editor opened. Deleted meanwhile (by web), it is
    /// written again from `original`, newer than the tombstone, as web's edit does: the text survives.
    Edit {
        original: Note,
        text: String,
        stamp: Stamp,
    },
    /// The notes `ids`, as the list's selection.
    Delete { ids: Vec<String>, stamp: Stamp },
    /// The notes `ids` of `agent` into its composer draft, in list order, deleted once the draft is
    /// saved; the others stay.
    HandOff {
        agent: String,
        ids: Vec<String>,
        stamp: Stamp,
    },
    /// `alt-enter` in the box: `agent`'s draft becomes a note on it; the box clears once it is saved.
    Queue { agent: String, stamp: Stamp },
    /// The destination of `agent`'s transfer was saved, or the disk refused (`Effect::Transfer`).
    Landed {
        agent: String,
        saved: Result<(), String>,
    },
}

/// A move between the notes and a draft, waiting on the save of its destination. The source changes
/// only once that save landed, so a crash or a refused write can duplicate a note, never lose one.
#[derive(Clone, Debug)]
pub enum Transfer {
    /// The draft before and after the hand-off, and the notes it took with the row version it saw
    /// (`updated`, `writeID`).
    HandOff {
        before: String,
        after: String,
        taken: Vec<(String, i64, String)>,
        stamp: Stamp,
    },
    /// The draft that became a note.
    Queue { draft: String },
}

/// Where a transfer's destination is saved: a hand-off's draft (`prefs.json`), a queued draft's note
/// (`outbox.json`).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Dest {
    Draft,
    Note,
}

/// Web's limit on a note's text and quote together (`maxNoteBytes`), in UTF-8 bytes.
pub const MAX_BYTES: usize = 8 * 1024;
const TOO_LONG: &str = "This note is too long to save. Shorten it and try again.";

impl Store {
    /// `agent`'s notes, oldest first.
    pub fn notes_of<'a>(&'a self, agent: &'a str) -> impl Iterator<Item = &'a Note> {
        self.notes.iter().filter(move |n| n.group == agent)
    }

    /// Whether `agent`'s notes cannot be handed over now: its box is read-only, or a send or another
    /// transfer is in flight.
    pub fn hand_off_blocked(&self, agent: &str) -> bool {
        self.can_send(agent).is_err() || self.busy(agent)
    }

    /// Why web would refuse to save this add, edit or queue, in its words.
    pub fn refusal(&self, step: &Step) -> Option<&'static str> {
        let (text, quote, empty) = match step {
            Step::Add { text, quote, .. } => {
                let empty = "Write something before saving this note.";
                (text.as_str(), quote.as_deref(), empty)
            }
            Step::Edit { original, text, .. } => {
                let live = self.notes.iter().find(|n| n.id == original.id);
                let quote = live.unwrap_or(original).quote.as_deref();
                (text.as_str(), quote, "A note cannot be empty.")
            }
            Step::Queue { agent, .. } => {
                let draft = self.prefs.drafts.get(agent).map_or("", String::as_str);
                (draft, None, "")
            }
            _ => return None,
        };
        let (text, quote) = (text.trim(), quote.map_or("", str::trim));
        match text.len() + quote.len() {
            0 if !empty.is_empty() => Some(empty),
            n if n > MAX_BYTES => Some(TOO_LONG),
            _ => None,
        }
    }

    pub(super) fn note(&mut self, step: Step, out: &mut Vec<Effect>) {
        if let Some(why) = self.refusal(&step) {
            if let Step::Queue { agent, .. } = &step {
                self.note_problems.insert(agent.clone(), why.into());
            }
            return;
        }
        let rows = match step {
            Step::Add {
                group,
                text,
                quote,
                stamp,
            } => {
                let quote = quote
                    .map(|q| q.trim().to_string())
                    .filter(|q| !q.is_empty());
                let source = quote
                    .as_ref()
                    .map(|_| json!({"kind": "transcript", "agent": group}));
                let (id, created) = (stamp.id.clone(), stamp.now);
                let value = NoteValue {
                    id,
                    group,
                    text: text.trim().to_string(),
                    quote,
                    source,
                    created,
                };
                vec![row(value, 0, &stamp)]
            }
            Step::Edit {
                original,
                text,
                stamp,
            } => {
                let live = self.notes.iter().find(|n| n.id == original.id);
                let text = text.trim().to_string();
                if live.is_some_and(|n| n.text == text) {
                    return;
                }
                let base = live.unwrap_or(&original);
                // The version to supersede: the row as it stands here, a tombstone included.
                let seen = self.sync[&Ns::Notes].rows.get(&original.id);
                let previous = seen.map_or(base.updated, |r| r.updated);
                let value = NoteValue {
                    text,
                    ..value(base)
                };
                vec![row(value, previous, &stamp)]
            }
            Step::Delete { ids, stamp } => {
                let notes = self.notes.iter().filter(|n| ids.contains(&n.id));
                notes.map(|n| tombstone(n, &stamp)).collect()
            }
            Step::HandOff { agent, ids, stamp } => {
                if self.hand_off_blocked(&agent) {
                    return;
                }
                let chosen = self.notes_of(&agent).filter(|n| ids.contains(&n.id));
                let notes: Vec<&Note> = chosen.collect();
                let texts: Vec<String> = notes.iter().map(|n| transfer_text(n)).collect();
                let addition = texts.into_iter().filter(|t| !t.trim().is_empty());
                let addition = addition.collect::<Vec<_>>().join("\n\n");
                if addition.is_empty() {
                    return;
                }
                let taken = notes.iter().filter_map(|n| self.version(&n.id)).collect();
                let draft = self.prefs.drafts.entry(agent.clone()).or_default();
                let before = draft.clone();
                *draft = match draft.is_empty() {
                    true => addition,
                    false => format!("{draft}\n\n{addition}"),
                };
                let after = draft.clone();
                let transfer = Transfer::HandOff {
                    before,
                    after,
                    taken,
                    stamp,
                };
                self.begin_transfer(agent, transfer, Dest::Draft, out);
                return;
            }
            Step::Queue { agent, stamp } => {
                let draft = self.prefs.drafts.get(&agent).cloned().unwrap_or_default();
                if draft.trim().is_empty() || self.busy(&agent) {
                    return;
                }
                let add = Step::Add {
                    group: agent.clone(),
                    text: draft.clone(),
                    quote: None,
                    stamp,
                };
                self.note(add, out);
                let transfer = Transfer::Queue { draft };
                return self.begin_transfer(agent, transfer, Dest::Note, out);
            }
            Step::Landed { agent, saved } => match self.land(&agent, saved, out) {
                Some(rows) => rows,
                None => return,
            },
        };
        if !rows.is_empty() {
            self.sync_step(Ns::Notes, SyncStep::Edit(rows), out);
        }
    }

    /// The note `id`'s row version as it stands here: its key, `updated` and `writeID`.
    fn version(&self, id: &str) -> Option<(String, i64, String)> {
        let row = self.sync[&Ns::Notes].rows.get(id)?;
        Some((row.key.clone(), row.updated, row.write_id.clone()))
    }

    fn begin_transfer(&mut self, agent: String, t: Transfer, to: Dest, out: &mut Vec<Effect>) {
        self.sends.remove(&agent);
        self.note_problems.remove(&agent);
        self.transfers.insert(agent.clone(), t);
        out.push(Effect::Transfer { to, agent });
    }

    /// Finish `agent`'s transfer once its destination is saved, or undo what was not; a hand-off's
    /// deletions are returned for the outbox. A refused save is said in the strip.
    fn land(
        &mut self,
        agent: &str,
        saved: Result<(), String>,
        out: &mut Vec<Effect>,
    ) -> Option<Vec<StateRow>> {
        let transfer = self.transfers.remove(agent)?;
        let draft = self.prefs.drafts.get(agent);
        match (transfer, saved) {
            (Transfer::HandOff { taken, stamp, .. }, Ok(())) => {
                // A note changed since it was taken (any newer version, here or from web) is not what
                // went into the draft: it stays.
                let notes = self
                    .notes
                    .iter()
                    .filter(|n| self.version(&n.id).is_some_and(|v| taken.contains(&v)));
                return Some(notes.map(|n| tombstone(n, &stamp)).collect());
            }
            (Transfer::HandOff { before, after, .. }, Err(e)) => {
                if draft == Some(&after) {
                    match before.is_empty() {
                        true => self.prefs.drafts.remove(agent),
                        false => self.prefs.drafts.insert(agent.into(), before),
                    };
                    out.push(Effect::Persist(Persist::Prefs));
                }
                let why = format!("The draft could not be saved, so the notes stay: {e}");
                self.note_problems.insert(agent.into(), why);
            }
            (Transfer::Queue { draft: queued }, Ok(())) => {
                if draft == Some(&queued) {
                    self.prefs.drafts.remove(agent);
                    out.push(Effect::Persist(Persist::Prefs));
                }
            }
            (Transfer::Queue { .. }, Err(e)) => {
                let why = format!("The note could not be saved yet, so the draft stays: {e}");
                self.note_problems.insert(agent.into(), why);
            }
        }
        None
    }
}

fn value(n: &Note) -> NoteValue {
    NoteValue {
        id: n.id.clone(),
        group: n.group.clone(),
        text: n.text.clone(),
        quote: n.quote.clone(),
        source: n.source.clone(),
        created: n.created,
    }
}

/// The row for `v` that supersedes the version `previous` (0 for a new note).
fn row(v: NoteValue, previous: i64, stamp: &Stamp) -> StateRow {
    StateRow {
        key: v.id.clone(),
        value: serde_json::to_value(&v).unwrap_or_default(),
        updated: stamp.now.max(previous + 1),
        write_id: stamp.write.clone(),
        deleted: false,
    }
}

fn tombstone(n: &Note, stamp: &Stamp) -> StateRow {
    StateRow {
        key: n.id.clone(),
        value: json!({"id": n.id}),
        updated: stamp.now.max(n.updated + 1),
        write_id: stamp.write.clone(),
        deleted: true,
    }
}

/// A note as it goes into a message: web's `noteTransferText`.
pub fn transfer_text(n: &Note) -> String {
    let Some(source) = &n.source else {
        return n.text.clone();
    };
    let transcript = source["kind"] == "transcript";
    let agent = source["agent"].as_str().unwrap_or("");
    let label = match transcript {
        true => format!("from {agent}'s transcript:"),
        false => source_label(source),
    };
    // A source of an unknown kind with no path has no label: no empty line for it.
    let head = |body: &str| match (label.is_empty(), body.is_empty()) {
        (true, _) => body.to_string(),
        (false, true) => label.clone(),
        (false, false) => format!("{label}\n{body}"),
    };
    let Some(quote) = &n.quote else {
        return head(&n.text);
    };
    let quote = match transcript {
        true => quote
            .split('\n')
            .map(|l| format!("> {l}"))
            .collect::<Vec<_>>()
            .join("\n"),
        false => fenced(quote),
    };
    let text = match n.text.is_empty() {
        true => String::new(),
        false => format!("\n\n{}", n.text),
    };
    head(&format!("{quote}{text}"))
}

/// Web's `noteSourceLabel` for a file or diff source: `path:start-end`, and `(vs base)` for a diff.
pub fn source_label(source: &Value) -> String {
    let num = |k: &str| Some(source.get(k).filter(|v| v.is_number())?.to_string());
    let (start, end) = (num("start"), num("end"));
    let range = match (&start, &end) {
        (None, _) => String::new(),
        (Some(s), Some(e)) if e != s => format!(":{s}-{e}"),
        (Some(s), _) => format!(":{s}"),
    };
    let path = format!("{}{range}", source["path"].as_str().unwrap_or(""));
    match source["base"].as_str() {
        Some(base) if source["kind"] == "diff" => format!("{path} (vs {base})"),
        _ => path,
    }
}

/// A fence longer than any run of backticks in `quote`.
fn fenced(quote: &str) -> String {
    let longest = quote.split(|c| c != '`').map(str::len).max().unwrap_or(0);
    let fence = "`".repeat((longest + 1).max(3));
    format!("{fence}\n{quote}\n{fence}")
}
