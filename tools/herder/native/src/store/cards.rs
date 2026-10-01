//! The lens cards' text (F4): each text card's visible agent's last assistant answer, as the
//! prototypes showed it. Focus and watch cards carry text; background cards carry none, so their
//! agents are never read.
//!
//! One tail read (`CARD_TAIL` entries) per agent per turn: a read is keyed on the board's
//! `turn_end_id`, so an agent is read again only once a turn ends. At most `IN_FLIGHT` reads run at
//! once, one per agent; the rest wait for a slot. A result for an agent no longer on a text card, or
//! for a turn other than the agent's current one, is dropped, success or failure; the current turn is
//! then read. A failed read keeps the text it had and is not asked again for that turn until the next
//! `hello`; a newer turn reads at once.

use super::condense;
use super::spaces::Row;
use super::{Effect, Fetch, Store};
use crate::api::{Entry, Kind};
use serde_json::Value;
use std::collections::{BTreeMap, BTreeSet};

/// Entries per card read: enough to reach past a turn's closing tools to its answer.
pub const CARD_TAIL: u32 = 12;
/// Card reads in flight at once.
pub const IN_FLIGHT: usize = 4;

/// A card read's answer: the tail's entries, or why the read failed.
#[derive(Clone, Debug)]
pub struct Got {
    pub agent: String,
    pub turn: Option<u64>,
    pub result: Result<Vec<Entry>, String>,
}

#[derive(Clone, Debug, Default)]
pub struct Cards {
    /// Per agent, the turn last read and the last answer found (kept when a read finds none).
    read: BTreeMap<String, (Option<u64>, Option<String>)>,
    /// Per agent, the turn being read.
    pub(super) flight: BTreeMap<String, Option<u64>>,
    /// Per agent, the turn whose read failed: not asked again for it until the next `hello`.
    failed: BTreeMap<String, Option<u64>>,
}

impl Cards {
    /// The agent's last answer, cleaned; `None` until a read has found one.
    pub fn text(&self, agent: &str) -> Option<&str> {
        self.read.get(agent)?.1.as_deref()
    }
}

impl Store {
    /// The agents whose cards show text: each focus and watch space's visible agent.
    fn carded(&self) -> BTreeSet<&str> {
        let rows = self
            .spaces
            .iter()
            .filter(|s| self.row(s) != Row::Background);
        rows.filter_map(|s| self.visible(s)).collect()
    }

    /// Ask for each text card whose agent's turn has moved past its last read, while slots are free.
    /// Not before `Boot`: effects of the boot-time loads are not run. A turn whose read failed waits
    /// for the next `hello`.
    pub(super) fn card_reads(&mut self, out: &mut Vec<Effect>) {
        if self.stream == 0 {
            return;
        }
        let carded = self.carded();
        let due = carded.into_iter().filter_map(|agent| {
            let turn = self.fleet.agents.get(agent)?.turn_end;
            let stale = self.cards.read.get(agent).is_none_or(|(t, _)| *t != turn);
            let failed = self.cards.failed.get(agent) == Some(&turn);
            let free = !self.cards.flight.contains_key(agent) && !failed;
            (stale && free).then(|| (agent.to_string(), turn))
        });
        let room = IN_FLIGHT.saturating_sub(self.cards.flight.len());
        let due: Vec<_> = due.take(room).collect();
        for (agent, turn) in due {
            self.cards.flight.insert(agent.clone(), turn);
            out.push(Effect::Fetch(Fetch::Card { agent, turn }));
        }
    }

    /// A read landed. Dropped, success or failure, when the agent's turn has moved on or its card no
    /// longer carries text; `card_reads` then asks for what is current.
    pub(super) fn card_read(&mut self, got: Got) {
        let Got {
            agent,
            turn,
            result,
        } = got;
        self.cards.flight.remove(&agent);
        let current = self.fleet.agents.get(&agent).map(|a| a.turn_end);
        if current != Some(turn) || !self.carded().contains(agent.as_str()) {
            return;
        }
        let Ok(entries) = result else {
            self.cards.failed.insert(agent, turn);
            return;
        };
        let kept = self
            .cards
            .read
            .get(&agent)
            .and_then(|(_, text)| text.clone());
        let text = entries.iter().rev().find_map(answer).or(kept);
        self.cards.read.insert(agent, (turn, text));
    }

    /// A `hello`: failed reads are asked again, and answers for agents off the board let go.
    pub(super) fn cards_hello(&mut self) {
        let fleet = &self.fleet.agents;
        self.cards.read.retain(|a, _| fleet.contains_key(a));
        self.cards.failed.clear();
    }
}

/// An assistant answer's text, cleaned as the transcript cleans it, with `<status>` stripped too
/// (the owner's ruling: the status line is not the card text), as one plain paragraph (`flat`);
/// `None` when nothing is left.
pub fn answer(entry: &Entry) -> Option<String> {
    let p = &entry.payload;
    if entry.kind != Kind::AssistantText || p.is_api_error_message.as_bool() == Some(true) {
        return None;
    }
    let text = match &p.message["content"] {
        Value::String(s) => s.clone(),
        Value::Array(blocks) => {
            let texts: Vec<&str> = blocks.iter().filter_map(|b| b["text"].as_str()).collect();
            texts.join("\n")
        }
        _ => return None,
    };
    let text = flat(&condense::clean(&strip(&text, "<status>", "</status>")));
    (!text.is_empty()).then_some(text)
}

/// `text` without each `open…close` span; an unclosed one hides the rest.
fn strip(text: &str, open: &str, close: &str) -> String {
    let (mut out, mut rest) = (String::with_capacity(text.len()), text);
    while let Some(at) = rest.find(open) {
        out.push_str(&rest[..at]);
        let end = rest[at..].find(close).map(|e| at + e + close.len());
        rest = &rest[end.unwrap_or(rest.len())..];
    }
    out.push_str(rest);
    out
}

/// The answer as one paragraph for the card: markdown's emphasis, code ticks, heading marks, table rules
/// and link targets dropped (a link keeps its text), each table row its cells joined by `·` and closed
/// by `;`, lines and runs of space joined, so the card's line clamp wraps it.
fn flat(markdown: &str) -> String {
    let rule = |l: &&str| !l.chars().all(|c| matches!(c, '|' | '-' | ':' | ' '));
    let row = |l: &str| match l.starts_with('|') {
        true => {
            let cells: Vec<&str> = l
                .split('|')
                .map(str::trim)
                .filter(|c| !c.is_empty())
                .collect();
            format!("{};", cells.join(" · "))
        }
        false => l.to_string(),
    };
    let lines = markdown.lines().map(|l| l.trim().trim_start_matches('#'));
    let lines: Vec<String> = lines.filter(rule).map(row).collect();
    let words = lines.iter().flat_map(|l| l.split_whitespace());
    let text = words.collect::<Vec<_>>().join(" ");
    let (mut out, mut rest) = (String::with_capacity(text.len()), text.as_str());
    while let Some(at) = rest.find("](") {
        let Some(open) = rest[..at].rfind('[') else {
            break;
        };
        let Some(close) = rest[at..].find(')') else {
            break;
        };
        out.push_str(&rest[..open]);
        out.push_str(&rest[open + 1..at]);
        rest = &rest[at + close + 1..];
    }
    out.push_str(rest);
    out.replace("**", "").replace("__", "").replace('`', "")
}
