//! Spaces and what is in them (U1), plus the owner's lens choices (U2).
//!
//! Spaces come from `spaces` and members from `spaces.members` (server rows shared with web,
//! tombstones dropped, ordered by `order` then id as web does; members in dock order). Local only
//! (`Prefs`): each space's row, visible agent and unread mark, and each agent's seen mark.

use crate::api::{Member, MembersValue, SpaceValue, StateRow};
use crate::store::fleet::{Fleet, Status};
use crate::store::{Effect, Persist, Prefs, Store};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;

#[derive(Clone, Debug, PartialEq)]
pub struct Space {
    pub id: String,
    pub name: String,
    pub order: f64,
    /// Dock order, each member once.
    pub members: Vec<Member>,
}

impl Space {
    pub fn agents(&self) -> impl Iterator<Item = &str> {
        self.members.iter().filter_map(|m| match m {
            Member::Agent { name } => Some(name.as_str()),
            Member::File { .. } => None,
        })
    }
}

/// Which lens row a space sits in; the owner's choice. A space never placed sits in `Watch`.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Row {
    Focus,
    #[default]
    Watch,
    Background,
}

impl Row {
    pub const ALL: [Row; 3] = [Row::Focus, Row::Watch, Row::Background];
}

/// The live spaces in lens order, each with its members. A row whose value does not decode is skipped
/// rather than failing the whole namespace.
pub fn derive(
    spaces: &BTreeMap<String, StateRow>,
    members: &BTreeMap<String, StateRow>,
) -> Vec<Space> {
    let mut out: Vec<Space> = spaces
        .values()
        .filter(|r| !r.deleted)
        .filter_map(|r| {
            let v: SpaceValue = serde_json::from_value(r.value.clone()).ok()?;
            let dock = members
                .get(&r.key)
                .filter(|m| !m.deleted)
                .and_then(|m| serde_json::from_value::<MembersValue>(m.value.clone()).ok())
                .map(|m| m.members)
                .unwrap_or_default();
            let mut unique: Vec<Member> = Vec::with_capacity(dock.len());
            for m in dock {
                if !unique.contains(&m) {
                    unique.push(m);
                }
            }
            Some(Space {
                id: r.key.clone(),
                name: v.name,
                order: v.order,
                members: unique,
            })
        })
        .collect();
    out.sort_by(|a, b| a.order.total_cmp(&b.order).then_with(|| a.id.cmp(&b.id)));
    out
}

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

/// After a new board: an agent seen for the first time gets its current turn as the baseline (an
/// unknown baseline is not a new turn), one no longer Blocked loses its block mark (blocking again
/// alerts again), and marks for agents neither on the board nor in a space drop. True on a change.
pub fn baseline_seen(seen: &mut BTreeMap<String, Seen>, fleet: &Fleet, spaces: &[Space]) -> bool {
    let before = seen.clone();
    for a in fleet.agents.values() {
        if let Some(turn_end) = a.turn_end {
            let fresh = SeenWire::Turn(turn_end).into();
            seen.entry(a.name.clone()).or_insert(fresh);
        }
        if let Some(mark) = seen.get_mut(&a.name) {
            mark.blocked &= a.status() == Status::Blocked;
        }
    }
    let members: Vec<&str> = spaces.iter().flat_map(|s| s.agents()).collect();
    seen.retain(|name, _| fleet.agents.contains_key(name) || members.contains(&name.as_str()));
    *seen != before
}

/// An owner move on the lens. Spaces are named by id.
#[derive(Clone, Debug)]
pub enum Move {
    /// Zoomed into a space, looking at `agent`: the space's unread mark clears and the agent is seen.
    View {
        space: String,
        agent: Option<String>,
    },
    /// `m`: every agent in the space is seen, and its unread mark clears.
    Read(String),
    /// `u`: the space needs you (bright, sticky) until the next zoom-in.
    Unread(String),
    /// Put a space (by id) in a lens row.
    SetRow { space: String, row: Row },
    /// Show the space's (by id) next agent on its card.
    CycleVisible(String),
}

/// The lens: spaces in rows, the agent each card shows, and where `n` goes next.
impl Store {
    pub(super) fn lens_move(&mut self, m: Move, out: &mut Vec<Effect>) {
        let (fleet, prefs) = (&self.fleet, &mut self.prefs);
        let space = |id: &str| self.spaces.iter().find(|s| s.id == id);
        let changed = match m {
            Move::View { space, agent } => {
                let seen = agent.is_some_and(|a| mark_seen(&mut prefs.seen, fleet, &a));
                prefs.unread.remove(&space) | seen
            }
            Move::Read(id) => {
                let agents = space(&id).into_iter().flat_map(Space::agents);
                let seen = agents.fold(false, |c, a| mark_seen(&mut prefs.seen, fleet, a) | c);
                prefs.unread.remove(&id) | seen
            }
            Move::Unread(id) => space(&id).is_some() && prefs.unread.insert(id),
            Move::SetRow { space, row } => prefs.rows.insert(space, row) != Some(row),
            Move::CycleVisible(id) => space(&id).is_some_and(|s| cycle_visible(s, fleet, prefs)),
        };
        if changed {
            out.push(Effect::Persist(Persist::Prefs));
        }
    }

    /// How many agents in this space need you; at least one while the owner has marked it unread.
    pub fn needs_you(&self, space: &Space) -> usize {
        let agents = space.agents().filter(|a| self.agent_needs_you(a)).count();
        agents.max(usize::from(self.prefs.unread.contains(&space.id)))
    }

    /// The spaces in lens order: focus, watch, background, each row in the store's order.
    pub fn lens(&self) -> Vec<&Space> {
        self.rows().concat()
    }

    /// Each row's spaces (focus, watch, background), in the store's order.
    pub fn rows(&self) -> [Vec<&Space>; 3] {
        Row::ALL.map(|r| self.spaces.iter().filter(|s| self.row(s) == r).collect())
    }

    pub fn row(&self, space: &Space) -> Row {
        self.prefs.rows.get(&space.id).copied().unwrap_or_default()
    }

    /// The agent a space's card shows.
    pub fn visible<'a>(&self, space: &'a Space) -> Option<&'a str> {
        visible(space, &self.fleet, &self.prefs.visible)
    }

    /// Whether this agent needs you: `fleet::Agent::needs_you` against its seen mark.
    pub fn agent_needs_you(&self, name: &str) -> bool {
        let seen = self.prefs.seen.get(name);
        let (turn, block) = (seen.map(|s| s.turn_end), seen.is_some_and(|s| s.blocked));
        let agent = self.fleet.agents.get(name);
        agent.is_some_and(|a| a.needs_you(turn, block))
    }

    /// The first space after `from` in lens order that needs you, wrapping round to `from` itself
    /// last. With no `from`, the search starts at the top.
    pub fn next_needing(&self, from: Option<&str>) -> Option<&Space> {
        let order = self.lens();
        let at = from.and_then(|id| order.iter().position(|s| s.id == id));
        let start = at.map_or(0, |i| i + 1);
        (0..order.len())
            .map(|k| order[(start + k) % order.len()])
            .find(|s| self.needs_you(s) > 0)
    }
}

/// The owner's pick while it is still a member on the board, else the first member on the board.
fn visible<'a>(
    space: &'a Space,
    fleet: &Fleet,
    picks: &BTreeMap<String, String>,
) -> Option<&'a str> {
    let live = || space.agents().filter(|a| fleet.agents.contains_key(*a));
    let pick = picks.get(&space.id).map(String::as_str);
    live().find(|a| Some(*a) == pick).or_else(|| live().next())
}

/// Show the next member on the board after the visible one, wrapping. True when the pick changed.
fn cycle_visible(space: &Space, fleet: &Fleet, prefs: &mut Prefs) -> bool {
    let Some(current) = visible(space, fleet, &prefs.visible) else {
        return false;
    };
    let live = || space.agents().filter(|a| fleet.agents.contains_key(*a));
    let after = live().skip_while(|a| *a != current).nth(1);
    let next = after.or_else(|| live().next()).unwrap_or(current);
    let before = prefs.visible.insert(space.id.clone(), next.to_string());
    before.as_deref() != Some(next)
}

/// The owner has looked at `name`: its latest turn and its current block are seen. An agent off the
/// board, or with no turn and no block, gets no mark. True when the mark changed.
fn mark_seen(seen: &mut BTreeMap<String, Seen>, fleet: &Fleet, name: &str) -> bool {
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

/// What one notification says (U6). The tag routes its click: `agent:<name>` zooms into that agent,
/// `space:<id>` (a burst's summary) selects that space on the lens.
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
    /// The agent zoomed in while the app is frontmost (the shell says, `Event::Looking`): never alerted.
    pub looking: Option<String>,
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
        let was_quiet = self.alerts.burst.is_empty();
        let armed = self.alerts.armed;
        let names: Vec<String> = self.fleet.agents.keys().cloned().collect();
        for name in names {
            let a = &self.fleet.agents[&name];
            let (turn, blocked) = (a.turn_end, a.status() == Status::Blocked);
            let eligible = self.alertable(&name);
            let looking = self.alerts.looking.as_deref() == Some(name.as_str());
            let fresh = Mark {
                eligible: false,
                turn,
                block: false,
            };
            let mark = self.alerts.marks.entry(name.clone()).or_insert(fresh);
            mark.block &= blocked;
            let cause = turn > mark.turn || (blocked && !mark.block);
            let alert = armed && eligible && !mark.eligible && cause;
            if alert && !looking {
                self.alerts.burst.push(name);
            }
            if alert || !armed {
                (mark.turn, mark.block) = (turn, blocked);
            }
            mark.eligible = eligible;
        }
        let fleet = &self.fleet.agents;
        self.alerts.marks.retain(|name, _| fleet.contains_key(name));
        self.alerts.armed = self.alerts.live;
        if was_quiet && !self.alerts.burst.is_empty() {
            out.push(Effect::Burst { after_ms: BURST_MS });
        }
    }

    /// The burst's second is up: one notification for those that still need you, a summary for several.
    pub(super) fn burst_ended(&mut self, out: &mut Vec<Effect>) {
        let burst = std::mem::take(&mut self.alerts.burst);
        let lens = self.lens();
        let home = |a: &str| lens.iter().copied().find(|s| s.agents().any(|m| m == a));
        let due: Vec<(&str, &Space)> = (burst.iter())
            .filter(|a| self.alertable(a) && self.alerts.looking.as_ref() != Some(a))
            .filter_map(|a| Some((a.as_str(), home(a)?)))
            .collect();
        let reason = |a: &str| match self.fleet.agents.get(a).map(|a| a.status()) {
            Some(Status::Blocked) => "blocked",
            _ => "your turn",
        };
        let notice = match due.as_slice() {
            [] => return,
            [(agent, space)] => Notice {
                tag: format!("agent:{agent}"),
                title: format!("{agent} · {}", space.name),
                body: reason(agent).into(),
            },
            [(_, first), ..] => Notice {
                tag: format!("space:{}", first.id),
                title: format!("{} agents need you", due.len()),
                body: (due.iter())
                    .map(|(a, s)| format!("{a} · {} ({})", s.name, reason(a)))
                    .collect::<Vec<_>>()
                    .join("\n"),
            },
        };
        out.push(Effect::Notify(notice));
    }

    /// Needs you and sits in a space (the lens shows it).
    fn alertable(&self, name: &str) -> bool {
        self.agent_needs_you(name) && self.spaces.iter().any(|s| s.agents().any(|a| a == name))
    }

    /// The dock badge: the lens's needs-you total, sent when it changes, and at boot (`all`) whatever it
    /// is, as the effects of the loads before boot are not run.
    pub(super) fn badge(&mut self, all: bool, out: &mut Vec<Effect>) {
        let n = self.spaces.iter().map(|s| self.needs_you(s)).sum();
        if std::mem::replace(&mut self.alerts.badge, n) != n || all {
            out.push(Effect::Badge(n));
        }
    }
}
