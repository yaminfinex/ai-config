//! Attention (U2, U6): what needs the owner, and what has told them so.
//!
//! Two marks per agent, kept apart on purpose though they look alike:
//! - `Seen` (in `Prefs`, persisted) is the owner's view: the latest turn they saw and whether they saw
//!   the current block. Needs-you, the cards' counts, the header and the dock badge read it.
//! - `Mark` (in `Alerts`, this session only) is the causes already alerted: a turn or a block notifies
//!   once. Alerting never marks seen, so an agent notified and not yet looked at still needs you.

use super::composer::Sending;
use super::fleet::{Agent, Fleet, Status};
use super::spaces::Space;
use super::{Effect, Store};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;

/// The owner's mark on one agent: the latest turn end seen, and whether its current block has been
/// viewed. Before U2 the mark was the bare turn number; both forms read.
#[derive(Clone, Copy, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(from = "SeenWire")]
pub struct Seen {
    pub turn_end: u64,
    pub blocked: bool,
}

#[derive(Deserialize)]
#[serde(untagged)]
enum SeenWire {
    Turn(u64),
    Mark { turn_end: u64, blocked: bool },
}

impl From<SeenWire> for Seen {
    fn from(w: SeenWire) -> Self {
        let (turn_end, blocked) = match w {
            SeenWire::Turn(turn_end) => (turn_end, false),
            SeenWire::Mark { turn_end, blocked } => (turn_end, blocked),
        };
        Seen { turn_end, blocked }
    }
}

impl Store {
    /// How many agents in this space need you; at least one while the owner has marked it unread.
    pub fn needs_you(&self, space: &Space) -> usize {
        let agents = space.agents().filter(|a| self.agent_needs_you(a)).count();
        agents.max(usize::from(self.prefs.unread.contains(&space.id)))
    }

    /// The lens's needs-you total: each space's count, summed.
    pub fn needs_you_total(&self) -> usize {
        self.spaces.iter().map(|s| self.needs_you(s)).sum()
    }

    /// Whether this agent needs you: `fleet::Agent::needs_you` against its seen mark.
    pub fn agent_needs_you(&self, name: &str) -> bool {
        let agent = self.fleet.agents.get(name);
        agent.is_some_and(|a| needs(&self.prefs.seen, a))
    }

    /// After a new board: an agent seen for the first time gets its current turn as the baseline (an
    /// unknown baseline is not a new turn); a block mark lapses once its agent is no longer Blocked, so
    /// blocking again needs you again (a seen mark's while its agent is on the board, a pending
    /// file-back's also once it is gone); marks for agents neither on the board nor in a space drop.
    /// True when the seen marks changed.
    pub(super) fn reseen(&mut self) -> bool {
        let (fleet, seen) = (&self.fleet, &mut self.prefs.seen);
        let before = seen.clone();
        for a in fleet.agents.values() {
            if let Some(turn_end) = a.turn_end {
                let fresh = SeenWire::Turn(turn_end).into();
                seen.entry(a.name.clone()).or_insert(fresh);
            }
        }
        let blocked = |name: &str| {
            fleet
                .agents
                .get(name)
                .map(|a| a.status() == Status::Blocked)
        };
        for (name, mark) in seen.iter_mut() {
            mark.blocked &= blocked(name).unwrap_or(true);
        }
        for (agent, sending) in &mut self.sends {
            if let Sending::InFlight {
                file_back: Some(then),
                ..
            } = sending
            {
                then.blocked &= blocked(agent).unwrap_or(false);
            }
        }
        let spaces = &self.spaces;
        let member = |name: &str| spaces.iter().any(|s| s.agents().any(|a| a == name));
        seen.retain(|name, _| fleet.agents.contains_key(name) || member(name));
        *seen != before
    }
}

/// `fleet::Agent::needs_you` against `a`'s seen mark.
fn needs(seen: &BTreeMap<String, Seen>, a: &Agent) -> bool {
    let seen = seen.get(&a.name);
    a.needs_you(seen.map(|s| s.turn_end), seen.is_some_and(|s| s.blocked))
}

/// The owner has looked at `name`: its latest turn and its current block are seen. An agent off the
/// board, or with no turn and no block, gets no mark. True when the mark changed.
pub(super) fn mark_seen(seen: &mut BTreeMap<String, Seen>, fleet: &Fleet, name: &str) -> bool {
    looking(fleet, name).is_some_and(|now| acknowledge(seen, fleet, name, now))
}

/// What looking at `name` now would mark seen; `None` off the board.
pub(super) fn looking(fleet: &Fleet, name: &str) -> Option<Seen> {
    let a = fleet.agents.get(name)?;
    let blocked = a.status() == Status::Blocked;
    Some(Seen {
        turn_end: a.turn_end.unwrap_or(0),
        blocked,
    })
}

/// The owner saw `name` as `then` (a send that files it back lands later): turns up to then are seen,
/// and its block only while it still stands as it was. True when the mark changed.
pub(super) fn acknowledge(
    seen: &mut BTreeMap<String, Seen>,
    fleet: &Fleet,
    name: &str,
    then: Seen,
) -> bool {
    let Some(now) = looking(fleet, name) else {
        return false;
    };
    let before = seen.get(name).copied().unwrap_or_default();
    let mut mark = before;
    mark.turn_end = mark.turn_end.max(then.turn_end);
    if now == then {
        mark.blocked = then.blocked;
    }
    let changed = mark != before;
    if changed {
        seen.insert(name.to_string(), mark);
    }
    changed
}

/// What one notification says (U6). The tag routes its click: `agent:<name>` zooms into that agent
/// (alone when it sits in no space), `space:<id>` (a burst's summary) selects that space on the lens,
/// and `lens` (a summary whose first agent is in no space) just brings the lens.
#[derive(Clone, Debug, PartialEq)]
pub struct Notice {
    pub tag: String,
    pub title: String,
    pub body: String,
}

/// A burst of agents turning to need you within this long becomes one notification.
pub const BURST_MS: u64 = 1000;

/// The needs-you alerts (U6): notifications on a transition into needing you, and the dock count.
#[derive(Clone, Debug, Default)]
pub struct Alerts {
    /// Per agent: whether it could be alerted, and the causes already spent.
    marks: BTreeMap<String, Mark>,
    /// A live board has been applied; once that event's baseline is taken, changes are transitions
    /// (boot, the snapshot and the first live board are not).
    pub(super) live: bool,
    armed: bool,
    /// Agents that turned to need you during the burst waiting out `BURST_MS`, in order.
    burst: Vec<String>,
    /// The dock count last shown; the dock starts clear.
    badge: usize,
    /// The app is frontmost (`Event::Front`).
    pub front: bool,
}

/// One agent's alert state. Eligibility and causes are separate: an alert needs a move into
/// eligibility *and* a cause not yet spent (a turn past `turn`, or a block not yet alerted), so the
/// same turn never alerts twice, and a block or a turn while it already needs you alerts nothing.
#[derive(Clone, Copy, Debug, Default)]
struct Mark {
    eligible: bool,
    /// The latest turn alerted (or baselined, or seen while looking).
    turn: Option<u64>,
    /// This block episode was alerted; cleared when the block ends, so blocking again alerts again.
    block: bool,
}

impl Store {
    /// After every event: an agent that moves into eligibility with an unspent cause joins the burst
    /// (the first to join starts its timer), unless the owner is looking at it, which spends the cause
    /// all the same. Until the first live board everything is baseline, and nothing alerts.
    pub(super) fn transitions(&mut self, out: &mut Vec<Effect>) {
        let looking = self.looking_at().map(String::from);
        let (alerts, seen, fleet) = (&mut self.alerts, &self.prefs.seen, &self.fleet.agents);
        let (was_quiet, armed) = (alerts.burst.is_empty(), alerts.armed);
        for (name, a) in fleet {
            let (turn, blocked) = (a.turn_end, a.status() == Status::Blocked);
            let eligible = needs(seen, a);
            let fresh = Mark {
                eligible: false,
                turn,
                block: false,
            };
            let mark = alerts.marks.entry(name.clone()).or_insert(fresh);
            mark.block &= blocked;
            let cause = turn > mark.turn || (blocked && !mark.block);
            let alert = armed && eligible && !mark.eligible && cause;
            // Once per burst: an agent alerting again before it ends (block, unblock, block) is one notice.
            if alert && looking.as_ref() != Some(name) && !alerts.burst.contains(name) {
                alerts.burst.push(name.clone());
            }
            if alert || !armed {
                (mark.turn, mark.block) = (turn, blocked);
            }
            mark.eligible = eligible;
        }
        alerts.marks.retain(|name, _| fleet.contains_key(name));
        alerts.armed = alerts.live;
        if was_quiet && !alerts.burst.is_empty() {
            out.push(Effect::Burst { after_ms: BURST_MS });
        }
    }

    /// The agent the owner is looking at: zoomed in on it (its transcript is open) with the app
    /// frontmost. It is never alerted.
    pub(super) fn looking_at(&self) -> Option<&str> {
        let open = self
            .transcript
            .open
            .as_ref()
            .filter(|_| self.alerts.front)?;
        Some(&open.agent)
    }

    /// The burst's second is up: one notification for those that still need you, a summary for several.
    pub(super) fn burst_ended(&mut self, out: &mut Vec<Effect>) {
        let burst = std::mem::take(&mut self.alerts.burst);
        let due: Vec<(&str, Option<&Space>)> = (burst.iter())
            .filter(|a| self.agent_needs_you(a) && self.looking_at() != Some(a.as_str()))
            .map(|a| (a.as_str(), self.home(a)))
            .collect();
        fn name(s: Option<&Space>) -> &str {
            s.map_or("no space", |s| s.name.as_str())
        }
        let reason = |a: &str| match self.fleet.agents.get(a).map(|a| a.status()) {
            Some(Status::Blocked) => "blocked",
            _ => "your turn",
        };
        let notice = match due.as_slice() {
            [] => return,
            [(agent, space)] => Notice {
                tag: format!("agent:{agent}"),
                title: format!("{agent} · {}", name(*space)),
                body: reason(agent).into(),
            },
            [(_, first), ..] => Notice {
                tag: first.map_or("lens".into(), |s| format!("space:{}", s.id)),
                title: format!("{} agents need you", due.len()),
                body: (due.iter())
                    .map(|(a, s)| format!("{a} · {} ({})", name(*s), reason(a)))
                    .collect::<Vec<_>>()
                    .join("\n"),
            },
        };
        out.push(Effect::Notify(notice));
    }

    /// The dock badge: the lens's needs-you total plus the agents in no space that need you (they alert
    /// too, owner ruling), sent when it changes, and at boot (`all`) whatever it is, as the effects of
    /// the loads before boot are not run.
    pub(super) fn badge(&mut self, all: bool, out: &mut Vec<Effect>) {
        let fleet = self.fleet.agents.keys();
        let alone = fleet
            .filter(|a| self.home(a).is_none() && self.agent_needs_you(a))
            .count();
        let n = self.needs_you_total() + alone;
        if std::mem::replace(&mut self.alerts.badge, n) != n || all {
            out.push(Effect::Badge(n));
        }
    }
}
