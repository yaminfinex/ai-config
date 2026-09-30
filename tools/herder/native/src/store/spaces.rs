//! Spaces and what is in them (U1), plus the owner's lens choices (U2).
//!
//! Spaces come from the `spaces` namespace and members from `spaces.members` (both server rows shared
//! with web, tombstones dropped, ordered by `order` then id as web does; members in dock order). Local,
//! never on the server (`Prefs`): the row each space sits in (focus / watch / background), the visible
//! agent per space, and the seen mark per agent, which is what "needs you" compares the agent's latest
//! turn against.

use crate::api::{Member, MembersValue, SpaceValue, StateRow};
use crate::store::fleet::Fleet;
use crate::store::{Effect, Persist, Store};
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

/// Agents in this space whose latest turn the owner has not seen.
pub fn needs_you(space: &Space, fleet: &Fleet, seen: &BTreeMap<String, u64>) -> usize {
    space
        .agents()
        .filter(|name| {
            let mark = seen.get(*name).copied();
            fleet.agents.get(*name).is_some_and(|a| a.needs_you(mark))
        })
        .count()
}

/// After a new board: an agent seen for the first time gets its current turn as the baseline (an
/// unknown baseline is not a new turn); marks for agents neither on the board nor in a space are
/// dropped. True when the marks changed.
pub fn baseline_seen(seen: &mut BTreeMap<String, u64>, fleet: &Fleet, spaces: &[Space]) -> bool {
    let before = seen.clone();
    for a in fleet.agents.values() {
        if let Some(turn) = a.turn_end {
            seen.entry(a.name.clone()).or_insert(turn);
        }
    }
    let members: Vec<&str> = spaces.iter().flat_map(|s| s.agents()).collect();
    seen.retain(|name, _| fleet.agents.contains_key(name) || members.contains(&name.as_str()));
    *seen != before
}

/// An owner move on the lens.
#[derive(Clone, Debug)]
pub enum Move {
    /// The owner has looked at this agent's latest turn.
    Seen(String),
    /// Make this agent's latest turn unread again.
    Unseen(String),
    /// Put a space (by id) in a lens row.
    SetRow { space: String, row: Row },
    /// Show the space's (by id) next agent on its card.
    CycleVisible(String),
}

/// The lens: spaces in rows, the agent each card shows, and where `n` goes next.
impl Store {
    pub(super) fn lens_move(&mut self, m: Move, out: &mut Vec<Effect>) {
        let (fleet, prefs) = (&self.fleet, &mut self.prefs);
        let changed = match m {
            Move::Seen(name) => mark_seen(&mut prefs.seen, fleet, name),
            Move::Unseen(name) => mark_unseen(&mut prefs.seen, fleet, name),
            Move::SetRow { space, row } => prefs.rows.insert(space, row) != Some(row),
            Move::CycleVisible(id) => {
                let space = self.spaces.iter().find(|s| s.id == id);
                space.is_some_and(|s| cycle_visible(s, fleet, &mut prefs.visible))
            }
        };
        if changed {
            out.push(Effect::Persist(Persist::Prefs));
            self.ask_status_lines(out);
        }
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
        let seen = self.prefs.seen.get(name).copied();
        self.fleet
            .agents
            .get(name)
            .is_some_and(|a| a.needs_you(seen))
    }

    /// The first space after `from` in lens order that needs you, wrapping round to `from` itself
    /// last. With no `from`, the search starts at the top.
    pub fn next_needing(&self, from: Option<&str>) -> Option<&Space> {
        let order = self.lens();
        let start = from
            .and_then(|id| order.iter().position(|s| s.id == id))
            .map_or(0, |i| i + 1);
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
fn cycle_visible(space: &Space, fleet: &Fleet, picks: &mut BTreeMap<String, String>) -> bool {
    let Some(current) = visible(space, fleet, picks) else {
        return false;
    };
    let live: Vec<&str> = space
        .agents()
        .filter(|a| fleet.agents.contains_key(*a))
        .collect();
    let at = live.iter().position(|a| *a == current).unwrap_or(0);
    let next = live[(at + 1) % live.len()].to_string();
    picks.insert(space.id.clone(), next.clone()).as_ref() != Some(&next)
}

/// The owner has looked at `name`'s latest turn. True when its mark moved.
fn mark_seen(seen: &mut BTreeMap<String, u64>, fleet: &Fleet, name: String) -> bool {
    let turn = fleet.agents.get(&name).and_then(|a| a.turn_end);
    let mark = seen.entry(name).or_default();
    match turn.filter(|t| t > mark) {
        Some(turn) => {
            *mark = turn;
            true
        }
        None => false,
    }
}

/// Make `name`'s latest turn unread again. True when its mark moved.
fn mark_unseen(seen: &mut BTreeMap<String, u64>, fleet: &Fleet, name: String) -> bool {
    let Some(before) = fleet
        .agents
        .get(&name)
        .and_then(|a| a.turn_end?.checked_sub(1))
    else {
        return false;
    };
    seen.insert(name, before) != Some(before)
}
