//! Spaces and what is in them (U1), plus the owner's lens choices (U2).
//!
//! Spaces come from `spaces` and members from `spaces.members` (server rows shared with web,
//! tombstones dropped, ordered by `order` then id as web does; members in dock order). Local only
//! (`Prefs`): each space's row, visible agent and unread mark. Seen marks are `attention`'s.

use crate::api::{Member, MembersValue, SpaceValue, StateRow};
use crate::store::attention::mark_seen;
use crate::store::fleet::Fleet;
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

/// An owner move on the lens. Spaces are named by id.
#[derive(Clone, Debug)]
pub enum Move {
    /// Zoomed into a space, looking at `agent` (the focused panel) with the panels `beside` it on screen:
    /// the space's unread mark clears, the agent is seen (not those beside it), and the zoom opens their
    /// transcripts and streams the space (`transcript::show`).
    View {
        space: String,
        agent: Option<String>,
        beside: Vec<String>,
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
            Move::View {
                space,
                agent,
                beside,
            } => {
                let seen = agent
                    .as_ref()
                    .is_some_and(|a| mark_seen(&mut prefs.seen, fleet, a));
                let changed = prefs.unread.remove(&space) | seen;
                self.show(&space, agent.as_deref(), &beside, out);
                changed
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

    /// The spaces in lens order: focus, watch, background, each row in the store's order.
    pub fn lens(&self) -> Vec<&Space> {
        self.rows().concat()
    }

    /// The first space in lens order holding `agent`: where its notification says it is and where the
    /// click opens it.
    pub fn home(&self, agent: &str) -> Option<&Space> {
        let holds = |s: &&Space| s.agents().any(|m| m == agent);
        self.spaces.iter().filter(holds).min_by_key(|s| self.row(s))
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

    /// The first stop after `from` that needs you, wrapping round to `from` itself last: the spaces in
    /// lens order, then (with `alone`) the agents in no space, by name. With no `from`, or one that is
    /// no longer a stop, the search starts at the top.
    pub fn next_needing(&self, from: Option<Stop>, alone: bool) -> Option<Stop<'_>> {
        let agents = self.fleet.agents.keys();
        let loose = agents.filter(|a| alone && self.home(a).is_none());
        let spaces = self.lens().into_iter().map(Stop::Space);
        let order: Vec<Stop> = spaces.chain(loose.map(|a| Stop::Alone(a))).collect();
        let at = order.iter().position(|s| Some(*s) == from);
        let start = at.map_or(0, |i| i + 1);
        let needs = |s: &Stop| match *s {
            Stop::Space(s) => self.needs_you(s) > 0,
            Stop::Alone(a) => self.agent_needs_you(a),
        };
        (0..order.len())
            .map(|k| order[(start + k) % order.len()])
            .find(needs)
    }
}

/// Where `n` / `N` go: a space, or (owner ruling, after the spaces) an agent that sits in no space,
/// opened alone.
#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Stop<'a> {
    Space(&'a Space),
    Alone(&'a str),
}

/// The owner's pick while it is still a member on the board, else the first member on the board.
fn visible<'a>(
    space: &'a Space,
    fleet: &Fleet,
    picks: &BTreeMap<String, String>,
) -> Option<&'a str> {
    let pick = picks.get(&space.id).map(String::as_str);
    let picked = live(space, fleet).find(|a| Some(*a) == pick);
    picked.or_else(|| live(space, fleet).next())
}

/// The space's members on the board, in dock order.
fn live<'a>(space: &'a Space, fleet: &Fleet) -> impl Iterator<Item = &'a str> {
    space.agents().filter(|a| fleet.agents.contains_key(*a))
}

/// Show the next member on the board after the visible one, wrapping. True when the pick changed.
fn cycle_visible(space: &Space, fleet: &Fleet, prefs: &mut Prefs) -> bool {
    let Some(current) = visible(space, fleet, &prefs.visible) else {
        return false;
    };
    let after = live(space, fleet).skip_while(|a| *a != current).nth(1);
    let next = after
        .or_else(|| live(space, fleet).next())
        .unwrap_or(current);
    let before = prefs.visible.insert(space.id.clone(), next.to_string());
    before.as_deref() != Some(next)
}
