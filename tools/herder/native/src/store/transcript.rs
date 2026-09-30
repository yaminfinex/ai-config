//! One agent's transcript as the compact view renders it (U3). Pages arrive in both directions, so this
//! is not an append-only fold.
//!
//! One wire entry can yield several items (an `hcom_delivery` entry carries every delivery in that
//! injection; the fixtures show three at one offset), so the row key is `(byte_offset, sub)` with `sub`
//! the item's index within its entry. `items: BTreeMap<(u64, u16), Item>` — row order is key order, so a
//! `before=` page is an insertion and no stored index ever moves. Tool pairing uses
//! `calls: HashMap<tool_use_id, (u64, u16)>` plus `orphans: HashMap<tool_use_id, ToolResult>`: a result
//! whose call is known fills it, otherwise it waits in `orphans` until an older page brings the call.
//! Cursors: `next_offset` is set only by tail and `from=` responses; `prev_offset` is seeded from the
//! tail's `window.from` and then from each `before=` page's `prevOffset` (`0` = start of file); a
//! `before=` response never touches `next_offset`. Every request carries `(session_id, generation)` and
//! a response with a stale tag is dropped; `rewindow`/`reset` bumps the generation, clears everything and
//! re-reads the tail. Assistant text has `<internal>…</internal>` stripped and `<status>` unwrapped.

use crate::api::{Entry, Kind};
use crate::store::{Effect, Fetch, Store};
use serde_json::Value;

/// The latest `<status>…</status>` line in these entries' assistant text, for a lens card. Walks the
/// payload's strings rather than one tool's shape, so claude and codex entries both work.
pub fn status_line(entries: &[Entry]) -> Option<String> {
    fn last(v: &Value) -> Option<&str> {
        match v {
            Value::String(s) => {
                let rest = &s[s.rfind("<status>")? + "<status>".len()..];
                Some(rest[..rest.find("</status>")?].trim())
            }
            Value::Array(items) => items.iter().rev().find_map(last),
            Value::Object(map) => map.values().rev().find_map(last),
            _ => None,
        }
    }
    let texts = entries
        .iter()
        .rev()
        .filter(|e| e.kind == Kind::AssistantText);
    texts
        .filter_map(|e| last(&e.payload))
        .find(|l| !l.is_empty())
        .map(str::to_string)
}

/// The lens cards' `<status>` lines: one short tail per visible agent per finished turn.
impl Store {
    /// Ask for the line of each visible agent whose latest turn has not been asked for yet, however
    /// many boards arrive meanwhile. Nothing is asked before live data (the boot snapshot runs no effects).
    pub(super) fn ask_status_lines(&mut self, out: &mut Vec<Effect>) {
        if !self.live {
            return;
        }
        let visible = self.spaces.iter().filter_map(|s| self.visible(s));
        let turns: Vec<_> = visible
            .filter_map(|a| Some((a.to_string(), self.fleet.agents[a].turn_end?)))
            .collect();
        for (agent, turn) in turns {
            let asked = self.status_lines.entry(agent.clone()).or_default();
            if asked.0 != turn {
                asked.0 = turn;
                out.push(Effect::Fetch(Fetch::StatusLine { agent, turn }));
            }
        }
    }

    /// A tail came back: keep its line if it is still the latest turn asked for and has one.
    pub(super) fn status_line(&mut self, agent: String, turn: u64, line: Option<String>) {
        let asked = self.status_lines.get_mut(&agent).filter(|k| k.0 == turn);
        if let (Some(asked), Some(line)) = (asked, line) {
            asked.1 = Some(line);
        }
    }
}
