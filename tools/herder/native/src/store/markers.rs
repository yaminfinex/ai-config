//! Read markers (RM): what the owner has read of each agent, shared with herder web through the
//! `read.markers` namespace. Web's code is the spec: `readMarkerModel.ts` (the row and its merge),
//! `readPositionModel.ts` (reading through), `spaceAttentionModel.ts` (unread and seeding) and
//! `viewingModel.ts` (the dwell).
//!
//! - Unread is a deliberate mark unread, or a listening/active agent's turn ended after its marker's;
//!   no marker is no baseline, so nothing is unread until seeding records one.
//! - Reading only moves forward: rows merge newest-wins, but turn, position and time never go back
//!   (except a newer mark unread, which stands as written), and a merge ahead of the winning row is
//!   republished (`Sync::repairs`).
//! - The dwell: the focused panel's agent, watched (frontmost, following its tail) for `DWELL_MS`, reads
//!   through to its transcript's end; then what lands is read as it arrives, a position creeping inside
//!   a turn already read at most every `CREEP_MS`.
//! - Seeding waits for the first pull and a live board: every board agent with no marker gets a weak
//!   baseline (`WEAK`, so any real row wins), the pre-RM `Prefs::seen` turns first.

use super::fleet::Agent;
use super::sync::{Ns, Step};
use super::{Effect, Persist, Store, Wake};
use crate::api::StateRow;
use serde::{Deserialize, Serialize};
use std::cmp::Ordering;
use std::collections::BTreeSet;

/// Viewing counts as reading after this long (web's `viewDwellMs`).
pub const DWELL_MS: u64 = 1000;
/// A position creeping inside a turn already read is written at most this often (`positionWriteMs`).
pub const CREEP_MS: i64 = 5000;
/// A weak write's version (web's `weakUpdated`): seeding and the migration lose to any real row.
pub const WEAK: i64 = 1;

/// The last transcript entry read: its session, byte offset and timestamp (`""` without one).
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Pos {
    pub session: String,
    pub offset: u64,
    pub ts: String,
}

/// One agent's marker (web's `ReadMarker`): the newest turn end read (0 while unknown), where reading
/// reached, when (epoch ms, 0 for never), and a deliberate mark unread.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Marker {
    pub turn: u64,
    pub pos: Option<Pos>,
    pub at: i64,
    pub unread: bool,
}

/// A seed's marker: the turn as read, nothing else known.
pub fn baseline(turn: u64) -> Marker {
    Marker {
        turn,
        pos: None,
        at: 0,
        unread: false,
    }
}

/// A live row's marker; `None` for a tombstone or a value web would refuse (`parseMarker`).
pub fn parse(row: &StateRow) -> Option<Marker> {
    #[derive(Deserialize)]
    struct Wire {
        turn: u64,
        pos: Option<Pos>,
        at: u64,
        unread: bool,
    }
    let w = Wire::deserialize(&row.value)
        .ok()
        .filter(|_| !row.deleted)?;
    let pos_ok = w.pos.as_ref().is_none_or(|p| !p.session.is_empty());
    let at = i64::try_from(w.at).ok().filter(|_| pos_ok)?;
    Some(Marker {
        turn: w.turn,
        pos: w.pos,
        at,
        unread: w.unread,
    })
}

/// A marker's row value, `updated` inside as web writes it.
pub fn value(m: &Marker, updated: i64) -> serde_json::Value {
    let Marker {
        turn,
        pos,
        at,
        unread,
    } = m;
    serde_json::json!({ "turn": turn, "pos": pos, "at": at, "unread": unread, "updated": updated })
}

/// Whether `right` reads further than `left` (web's `comparePositions` > 0): in one session by offset,
/// across sessions the one read later.
fn ahead(left: Option<&Pos>, left_at: i64, right: Option<&Pos>, right_at: i64) -> bool {
    match (left, right) {
        (Some(l), Some(r)) if l.session == r.session => r.offset > l.offset,
        (Some(_), Some(_)) => right_at > left_at,
        (l, r) => r.is_some() && l.is_none(),
    }
}

/// An incoming row against the one held (web's `mergeMarkerRow`): the newer version wins, but reading
/// never moves backward, except that a newer mark unread stands. `true` when the merge is ahead of the
/// winning row, to be republished. `None` for a row web would refuse.
pub fn merge(current: Option<&StateRow>, incoming: StateRow) -> Option<(StateRow, bool)> {
    if !incoming.deleted && parse(&incoming).is_none() {
        return None;
    }
    let Some(current) = current else {
        return Some((incoming, false));
    };
    let (winner, loser) = match incoming.version_cmp(current) {
        Ordering::Greater => (incoming, current.clone()),
        _ => (current.clone(), incoming),
    };
    let (Some(w), Some(l)) = (parse(&winner), parse(&loser)) else {
        return Some((winner, false));
    };
    if w.unread {
        return Some((winner, false));
    }
    let further = ahead(w.pos.as_ref(), w.at, l.pos.as_ref(), l.at);
    let merged = Marker {
        turn: w.turn.max(l.turn),
        pos: if further { l.pos } else { w.pos.clone() },
        at: w.at.max(l.at),
        unread: false,
    };
    if merged == w {
        return Some((winner, false));
    }
    let value = value(&merged, winner.updated);
    Some((StateRow { value, ..winner }, true))
}

/// What reading writes (web's `readThrough`): the turn end and the newest entry read, neither moving
/// back, the mark unread cleared. `None` when it changes nothing, or only creeps the position inside a
/// turn already read sooner than `CREEP_MS`. A `latest` of `None` keeps the position.
pub fn read_through(
    m: Option<&Marker>,
    turn: Option<u64>,
    latest: Option<&Pos>,
    now: i64,
) -> Option<Marker> {
    let next = m.map_or(0, |m| m.turn).max(turn.unwrap_or(0));
    let (current, at) = (m.and_then(|m| m.pos.as_ref()), m.map_or(0, |m| m.at));
    let pos = match latest {
        Some(l) if ahead(current, at, Some(l), now) => Some(l),
        _ => current,
    };
    if let Some(m) = m.filter(|m| !m.unread && next == m.turn) {
        let same_session = matches!((current, pos), (Some(c), Some(p)) if c.session == p.session);
        if pos == current || (same_session && now - m.at < CREEP_MS) {
            return None;
        }
    }
    Some(Marker {
        turn: next,
        pos: pos.cloned(),
        at: now,
        unread: false,
    })
}

/// A deliberate mark unread from `pos` (web's `markUnreadAt`); with none, the position is kept.
pub fn marked_unread(m: Option<&Marker>, pos: Option<Pos>) -> Marker {
    Marker {
        turn: m.map_or(0, |m| m.turn),
        pos: pos.or_else(|| m.and_then(|m| m.pos.clone())),
        at: m.map_or(0, |m| m.at),
        unread: true,
    }
}

/// The turn-end signal (web's `turnEnd`): none for a retired or stopped agent.
pub fn turn_end(a: &Agent) -> Option<u64> {
    let gone = matches!(a.bus_status.as_str(), "retired" | "stopped");
    a.turn_end.filter(|t| *t > 0 && !gone)
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Attention {
    Blocked,
    Unread,
}

/// Web's `agentAttention`: a blocked agent (on the bus) is blocked; a mark unread is unread; else a
/// listening or active agent whose turn ended after its marker's.
pub fn attention(a: Option<&Agent>, m: Option<&Marker>) -> Option<Attention> {
    if a.is_some_and(|a| a.bus_status == "blocked") {
        return Some(Attention::Blocked);
    }
    if m.is_some_and(|m| m.unread) {
        return Some(Attention::Unread);
    }
    let a = a.filter(|a| matches!(a.bus_status.as_str(), "listening" | "active"))?;
    let newer = matches!((turn_end(a), m), (Some(t), Some(m)) if t > m.turn);
    newer.then_some(Attention::Unread)
}

/// Web's `agentUnread`: whether the agent can be marked read.
pub fn unread(a: Option<&Agent>, m: Option<&Marker>) -> bool {
    m.is_some_and(|m| m.unread) || attention(a, m) == Some(Attention::Unread)
}

/// The dwell and which manual unreads it may clear (web's `viewingModel` and `nextArmed`).
#[derive(Clone, Debug, Default)]
pub struct Reading {
    /// The agent watched now (`Store::watched`); a change restarts the dwell under a new token.
    agent: Option<String>,
    pub(super) token: u64,
    /// The watched agent has been watched for `DWELL_MS`.
    dwelled: bool,
    /// Manual unreads the dwell may clear: their agent has been seen not viewed since the mark.
    armed: BTreeSet<String>,
}

impl Store {
    /// After every event: seed, arm, and read through what the dwell has read. Nothing before the first
    /// pull, so a fresh Mac never writes over what another device has not read yet.
    pub(super) fn read(&mut self, out: &mut Vec<Effect>) {
        let watched = self.watched().map(String::from);
        let r = &mut self.reading;
        if watched != r.agent {
            (r.agent, r.token, r.dwelled) = (watched, r.token + 1, false);
            if r.agent.is_some() {
                let (after_ms, wake) = (DWELL_MS, Wake::Dwell(r.token));
                out.push(Effect::After { after_ms, wake });
            }
        }
        if !self.sync[&Ns::Markers].pulled {
            return;
        }
        self.seed(out);
        // A mark arms once its agent is not viewed (the agent looked at, as only the focused panel reads).
        let viewed = self.looking_at().map(String::from);
        let (markers, armed) = (&self.markers, &mut self.reading.armed);
        armed.retain(|a| markers.get(a).is_some_and(|m| m.unread));
        let marked = markers.iter().filter(|(_, m)| m.unread).map(|(a, _)| a);
        armed.extend(marked.filter(|a| viewed.as_ref() != Some(*a)).cloned());
        let Some(agent) = self.reading.agent.clone().filter(|_| self.reading.dwelled) else {
            return;
        };
        let marker = self.markers.get(&agent);
        let open = self.transcript.open.get(&agent).filter(|t| t.loaded());
        if marker.is_some_and(|m| m.unread) && !self.reading.armed.contains(&agent) {
            return;
        }
        let Some(t) = open else { return };
        let turn = self.fleet.agents.get(&agent).and_then(turn_end);
        if let Some(next) = read_through(marker, turn, t.end.as_ref(), self.clock.now) {
            self.write(vec![(agent, next)], false, out);
        }
    }

    /// The dwell's timer: its agent, still watched, has been read.
    pub(super) fn dwelled(&mut self, token: u64) {
        self.reading.dwelled |= token == self.reading.token && self.reading.agent.is_some();
    }

    /// The agent being read: its panel focused, following its tail, the app frontmost.
    pub(super) fn watched(&self) -> Option<&str> {
        let open = self
            .transcript
            .focused()
            .filter(|t| t.tail && self.alerts.front)?;
        Some(open.agent.as_str())
    }

    /// Weak baselines once a live board has arrived: the pre-RM seen turns first (once), then every
    /// agent on the board with a turn and no marker (web seeds the open agents; native also alerts the
    /// agents in no space).
    fn seed(&mut self, out: &mut Vec<Effect>) {
        if !self.alerts.live {
            return;
        }
        let legacy = std::mem::take(&mut self.prefs.seen);
        if !legacy.is_empty() {
            out.push(Effect::Persist(Persist::Prefs));
        }
        let mut seeds: Vec<(String, Marker)> = (legacy.into_iter())
            .filter(|(a, s)| s.turn_end > 0 && !self.markers.contains_key(a))
            .map(|(a, s)| (a, baseline(s.turn_end)))
            .collect();
        for a in self.fleet.agents.values() {
            let fresh = !self.markers.contains_key(&a.name) && !seeds.iter().any(|s| s.0 == a.name);
            if let Some(turn) = turn_end(a).filter(|_| fresh) {
                seeds.push((a.name.clone(), baseline(turn)));
            }
        }
        self.write(seeds, true, out);
    }

    /// Where reading `agent` has reached for a mark read: its transcript's end while open and following
    /// the tail, else `None` (the position kept).
    fn latest(&self, agent: &str) -> Option<Pos> {
        let t = self.transcript.open.get(agent).filter(|t| t.tail)?;
        t.end.clone()
    }

    /// Mark `agents` read now, as the dwell would and whether or not a mark unread is armed (web's
    /// `markReadUpdates`; only those unread), or up to `turn` (a file-back: what the owner saw).
    pub(super) fn mark_read(&mut self, agents: &[&str], turn: Option<u64>, out: &mut Vec<Effect>) {
        let updates = (agents.iter())
            .filter_map(|&name| {
                let (a, m) = (self.fleet.agents.get(name), self.markers.get(name));
                if turn.is_none() && !unread(a, m) {
                    return None;
                }
                let (turn, latest) = (turn.or_else(|| a.and_then(turn_end)), self.latest(name));
                let next = read_through(m, turn, latest.as_ref(), self.clock.now)?;
                Some((name.to_string(), next))
            })
            .collect();
        self.write(updates, false, out);
    }

    /// Mark `agents` unread from the start of their latest turn where it is loaded (web's
    /// `markLastTurnUnread`); each holds until its agent is left and come back to.
    pub(super) fn mark_unread(&mut self, agents: &[&str], out: &mut Vec<Effect>) {
        let updates = (agents.iter())
            .map(|&name| {
                let start = self.transcript.open.get(name).and_then(|t| t.turn_start());
                (
                    name.to_string(),
                    marked_unread(self.markers.get(name), start),
                )
            })
            .collect();
        for name in agents {
            self.reading.armed.remove(*name);
        }
        self.write(updates, false, out);
    }

    /// `alt-u` (web's toggle): an unread agent is marked read, a read one unread.
    pub(super) fn toggle_read(&mut self, agent: &str, out: &mut Vec<Effect>) {
        let (a, m) = (self.fleet.agents.get(agent), self.markers.get(agent));
        match unread(a, m) {
            true => self.mark_read(&[agent], None, out),
            false => self.mark_unread(&[agent], out),
        }
    }

    /// Write markers as web's store `apply` does: an unchanged one is skipped, a weak one only lands
    /// where no live row is (and then at version `WEAK`); every other version is `now`, past the row's.
    fn write(&mut self, updates: Vec<(String, Marker)>, weak: bool, out: &mut Vec<Effect>) {
        let held = &self.sync[&Ns::Markers].rows;
        let rows: Vec<StateRow> = (updates.into_iter())
            .filter_map(|(key, m)| {
                let current = held.get(&key);
                let live = current.and_then(parse);
                if (weak && live.is_some()) || live.as_ref() == Some(&m) {
                    return None;
                }
                let updated = match current {
                    None if weak => WEAK,
                    _ => self.clock.now.max(current.map_or(0, |c| c.updated) + 1),
                };
                let (value, write_id) = (value(&m, updated), self.clock.write.clone());
                Some(StateRow {
                    key,
                    value,
                    updated,
                    write_id,
                    deleted: false,
                })
            })
            .collect();
        if !rows.is_empty() {
            self.sync_step(Ns::Markers, Step::Edit(rows), out);
        }
    }

    /// Rows a pull merged ahead of the winners: republished at a new version, so the server converges.
    pub(super) fn repair(&mut self, rows: Vec<StateRow>, out: &mut Vec<Effect>) {
        let rows = (rows.into_iter())
            .filter_map(|r| {
                let (m, updated) = (parse(&r)?, self.clock.now.max(r.updated + 1));
                // The value carries its own `updated`, as web's rows do.
                let (value, write_id) = (value(&m, updated), self.clock.write.clone());
                Some(StateRow {
                    value,
                    updated,
                    write_id,
                    ..r
                })
            })
            .collect();
        self.sync_step(Ns::Markers, Step::Edit(rows), out);
    }
}
