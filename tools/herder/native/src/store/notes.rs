//! Notes (U5): per agent, the things the owner wants to say to it or remember about it.
//!
//! Notes are the `notes` namespace rows web writes: per agent (`group`, or `general`, which the strip
//! leaves to web's rail), with an optional quote and source. Every edit here becomes a full row version
//! for `store::sync` (its outbox, LWW and tombstones), in web's record shape (`storedNoteToStateRow`), so
//! both clients read each other's notes. A version is `max(now, previous + 1)`, as web's.
//!
//! The hand-off follows web's `noteHandOff`: the notes are appended to the composer draft (`\n\n`
//! between them, each as web's `noteTransferText`) and deleted in the same step, not when a message
//! lands; the draft is saved like any draft, and what the owner then sends from it is theirs to edit.

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
    Edit {
        id: String,
        text: String,
        stamp: Stamp,
    },
    Delete {
        id: String,
        stamp: Stamp,
    },
    /// Every note of `agent` into its composer draft, then deleted.
    HandOff {
        agent: String,
        stamp: Stamp,
    },
    /// `alt-enter` in the box: `agent`'s draft becomes a note on it and the box clears.
    Queue {
        agent: String,
        stamp: Stamp,
    },
}

impl Store {
    /// `agent`'s notes, oldest first.
    pub fn notes_of<'a>(&'a self, agent: &'a str) -> impl Iterator<Item = &'a Note> {
        self.notes.iter().filter(move |n| n.group == agent)
    }

    pub(super) fn note(&mut self, step: Step, out: &mut Vec<Effect>) {
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
                let text = text.trim().to_string();
                if text.is_empty() && quote.is_none() {
                    return;
                }
                let source = quote
                    .as_ref()
                    .map(|_| json!({"kind": "transcript", "agent": group}));
                let (id, created) = (stamp.id.clone(), stamp.now);
                let value = NoteValue {
                    id,
                    group,
                    text,
                    quote,
                    source,
                    created,
                };
                vec![row(value, 0, &stamp)]
            }
            Step::Edit { id, text, stamp } => {
                let Some(note) = self.notes.iter().find(|n| n.id == id) else {
                    return;
                };
                let text = text.trim().to_string();
                if text == note.text || text.is_empty() && note.quote.is_none() {
                    return;
                }
                let value = NoteValue {
                    text,
                    ..value(note)
                };
                vec![row(value, note.updated, &stamp)]
            }
            Step::Delete { id, stamp } => {
                let note = self.notes.iter().find(|n| n.id == id);
                note.map(|n| tombstone(n, &stamp)).into_iter().collect()
            }
            Step::HandOff { agent, stamp } => {
                if self.can_send(&agent).is_err() || self.in_flight(&agent) {
                    return;
                }
                let notes: Vec<&Note> = self.notes_of(&agent).collect();
                let texts: Vec<String> = notes.iter().map(|n| transfer_text(n)).collect();
                let addition = texts.into_iter().filter(|t| !t.trim().is_empty());
                let addition = addition.collect::<Vec<_>>().join("\n\n");
                if addition.is_empty() {
                    return;
                }
                let rows = notes.iter().map(|n| tombstone(n, &stamp)).collect();
                let draft = self.prefs.drafts.entry(agent.clone()).or_default();
                *draft = match draft.is_empty() {
                    true => addition,
                    false => format!("{draft}\n\n{addition}"),
                };
                self.sends.remove(&agent);
                out.push(Effect::Persist(Persist::Prefs));
                rows
            }
            Step::Queue { agent, stamp } => {
                let draft = self.prefs.drafts.get(&agent);
                if self.in_flight(&agent) || draft.is_none_or(|d| d.trim().is_empty()) {
                    return;
                }
                let text = self.prefs.drafts.remove(&agent).unwrap_or_default();
                self.sends.remove(&agent);
                out.push(Effect::Persist(Persist::Prefs));
                let quote = None;
                let add = Step::Add {
                    group: agent,
                    text,
                    quote,
                    stamp,
                };
                return self.note(add, out);
            }
        };
        if !rows.is_empty() {
            self.sync_step(Ns::Notes, SyncStep::Edit(rows), out);
        }
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
    let label = match transcript {
        true => format!(
            "from {}'s transcript:",
            source["agent"].as_str().unwrap_or("")
        ),
        false => source_label(source),
    };
    let Some(quote) = &n.quote else {
        return match n.text.is_empty() {
            true => label,
            false => format!("{label}\n{}", n.text),
        };
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
    format!("{label}\n{quote}{text}")
}

/// Web's `noteSourceLabel` for a file or diff source: `path:start-end`, and `(vs base)` for a diff.
pub fn source_label(source: &Value) -> String {
    let num = |k: &str| {
        source
            .get(k)
            .filter(|v| v.is_number())
            .map(Value::to_string)
    };
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
