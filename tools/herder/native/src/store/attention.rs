//! Attention (U2, U6): what needs the owner, and what has told them so.
//!
//! Two marks per agent, kept apart on purpose though they look alike:
//! - The owner's view: the read marker shared with web (`markers`; unread) and, local beside it, whether
//!   they viewed the current block (`Prefs::blocks`). Needs-you, the cards' counts, the header and the
//!   dock badge read them.
//! - `Mark` (in `Alerts`, this session only) is the causes already alerted: a turn or a block notifies
//!   once. Alerting never marks read, so an agent notified and not yet looked at still needs you.

use super::composer::Sending;
use super::fleet::{Agent, Fleet, Status};
use super::markers::{self, Marker};
use super::spaces::Space;
use super::{Effect, Persist, Store, Wake};
use serde::{Deserialize, Serialize};
use std::collections::{BTreeMap, BTreeSet};

/// An agent as the owner saw it: its latest turn end, and whether it was blocked. A file-back's
/// snapshot, and the pre-RM `Prefs::seen` mark (before U2 the bare turn number; both forms read).
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
    /// How many agents in this space need you.
    pub fn needs_you(&self, space: &Space) -> usize {
        space.agents().filter(|a| self.agent_needs_you(a)).count()
    }

    /// The header's "N need you" and the dock badge (owner ruling, 2026-10-01): each agent that needs
    /// you once, whether it sits in one space, several or none.
    pub fn needs_you_total(&self) -> usize {
        let agents = self.fleet.agents.keys();
        agents.filter(|a| self.agent_needs_you(a)).count()
    }

    /// Whether this agent needs you: unread by its read marker (`markers::unread`), or Blocked with this
    /// block not yet viewed (owner ruling, U2).
    pub fn agent_needs_you(&self, name: &str) -> bool {
        let agent = self.fleet.agents.get(name);
        agent.is_some_and(|a| needs(a, &self.markers, &self.prefs.blocks))
    }

    /// After a new board: a block mark lapses once its agent is no longer Blocked, so blocking again
    /// needs you again (a mark's while its agent is on the board, a pending file-back's also once it is
    /// gone); marks for agents neither on the board nor in a space drop. True when the marks changed.
    pub(super) fn reseen(&mut self) -> bool {
        let (fleet, blocks) = (&self.fleet, &mut self.prefs.blocks);
        let before = blocks.len();
        let blocked = |name: &str| Some(fleet.agents.get(name)?.status() == Status::Blocked);
        let spaces = &self.spaces;
        let member = |name: &str| spaces.iter().any(|s| s.agents().any(|a| a == name));
        blocks.retain(|name| {
            let on = fleet.agents.contains_key(name) || member(name);
            blocked(name).unwrap_or(true) && on
        });
        for (agent, sending) in &mut self.sends {
            if let Sending::InFlight {
                file_back: Some(then),
                ..
            } = sending
            {
                then.blocked &= blocked(agent).unwrap_or(false);
            }
        }
        blocks.len() != before
    }
}

/// Needs-you against the read markers and the viewed blocks.
fn needs(a: &Agent, read: &BTreeMap<String, Marker>, blocks: &BTreeSet<String>) -> bool {
    let block = a.status() == Status::Blocked && !blocks.contains(&a.name);
    block || markers::unread(Some(a), read.get(&a.name))
}

/// The owner has looked at `name`: its current block is viewed. True when the mark changed.
pub(super) fn view_block(blocks: &mut BTreeSet<String>, fleet: &Fleet, name: &str) -> bool {
    let blocked = looking(fleet, name).is_some_and(|now| now.blocked);
    blocked && blocks.insert(name.to_string())
}

/// What looking at `name` now would see; `None` off the board.
pub(super) fn looking(fleet: &Fleet, name: &str) -> Option<Seen> {
    let a = fleet.agents.get(name)?;
    let (turn_end, blocked) = (a.turn_end.unwrap_or(0), a.status() == Status::Blocked);
    Some(Seen { turn_end, blocked })
}

/// The owner saw `name` as `then` (a send that files it back lands later): its block is viewed only
/// while it still stands as it was. True when the mark changed.
pub(super) fn acknowledge(
    blocks: &mut BTreeSet<String>,
    fleet: &Fleet,
    name: &str,
    then: Seen,
) -> bool {
    match looking(fleet, name) {
        Some(now) if now == then && then.blocked => blocks.insert(name.to_string()),
        Some(now) if now == then => blocks.remove(name),
        _ => false,
    }
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
        let (alerts, fleet) = (&mut self.alerts, &self.fleet.agents);
        let (read, blocks) = (&self.markers, &self.prefs.blocks);
        let (was_quiet, armed) = (alerts.burst.is_empty(), alerts.armed);
        for (name, a) in fleet {
            let (turn, blocked) = (a.turn_end, a.status() == Status::Blocked);
            let eligible = needs(a, read, blocks);
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
            let (after_ms, wake) = (BURST_MS, Wake::Burst);
            out.push(Effect::After { after_ms, wake });
        }
    }

    /// Owner ruling (2026-10-01): while the owner watches an agent's tail (frontmost, its panel focused,
    /// its transcript following the bottom), a block that lands is viewed as it arrives; turns are read
    /// by the dwell (`markers`). A panel beside it is not watched (owner, 2026-10-03).
    pub(super) fn watch(&mut self, out: &mut Vec<Effect>) {
        let watched = self.watched().map(String::from);
        let blocks = &mut self.prefs.blocks;
        if watched.is_some_and(|a| view_block(blocks, &self.fleet, &a)) {
            out.push(Effect::Persist(Persist::Prefs));
        }
    }

    /// The agent the owner is looking at: its panel focused (its transcript is open) with the app
    /// frontmost. It is never alerted.
    pub(super) fn looking_at(&self) -> Option<&str> {
        let open = self.transcript.focused()?;
        self.alerts.front.then_some(open.agent.as_str())
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

    /// The dock badge: `needs_you_total`, sent when it changes, and at boot (`all`) whatever it is, as the
    /// effects of the loads before boot are not run.
    pub(super) fn badge(&mut self, all: bool, out: &mut Vec<Effect>) {
        let n = self.needs_you_total();
        if std::mem::replace(&mut self.alerts.badge, n) != n || all {
            out.push(Effect::Badge(n));
        }
    }
}
