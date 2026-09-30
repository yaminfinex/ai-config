//! Spaces and what is in them (U1), plus the owner's lens choices (U2).
//!
//! Spaces come from the `spaces` namespace and members from `spaces.members` (both server rows shared
//! with web, tombstones dropped, ordered by `order` then id as web does; members in dock order). Local,
//! never on the server (`Prefs`): the row each space sits in (focus / watch / background), the visible
//! agent per space, and the seen mark per agent, which is what "needs you" compares the agent's latest
//! turn against.

use crate::api::{Member, MembersValue, SpaceValue, StateRow};
use crate::store::fleet::Fleet;
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

/// Which lens row a space sits in; the owner's choice (U2).
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Row {
    Focus,
    Watch,
    Background,
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

/// The owner has looked at `name`'s latest turn. True when its mark moved.
pub fn mark_seen(seen: &mut BTreeMap<String, u64>, fleet: &Fleet, name: String) -> bool {
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
