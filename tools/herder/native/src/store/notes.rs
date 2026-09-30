//! Notes (records in U1; the strip is U5) and drafts.
//!
//! Notes are the `notes` namespace rows web writes: per agent (`group`, or `general`), with an optional
//! quote and source. They sync through `store::sync` like every namespace. Drafts are one string per
//! agent and stay on this Mac (`Prefs::drafts`).

use crate::api::{NoteValue, StateRow};
use serde_json::Value;
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
