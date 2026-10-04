//! Spaces and what is in them (U1), plus the owner's lens choices (U2).
//!
//! Spaces come from `spaces` and members from `spaces.members` (server rows shared with web,
//! tombstones dropped, ordered by `order` then id as web does; members in dock order). Local only
//! (`Prefs`): each space's row and visible agent. Read marks are `markers`'.

use crate::api::{Member, MembersValue, SpaceValue, StateRow};
use crate::store::attention::view_block;
use crate::store::fleet::Fleet;
use crate::store::markers::Mark;
use crate::store::notes::Stamp;
use crate::store::sync::{Ns, Step as SyncStep};
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

/// The version `layouts.json` is written as; a file of another is ignored and each dock opens on its
/// members. The kit's own `load` never checks its version, so this is ours.
pub const LAYOUTS: u32 = 1;

/// Each space's dock as the owner left it, local to this Mac (`layouts.json`): the kit's dump of the
/// dock's tree (splits, groups, tabs by agent), reconciled with the members when rebuilt. Maximize is
/// not kept.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Layouts {
    pub version: u32,
    pub spaces: BTreeMap<String, serde_json::Value>,
}

impl Default for Layouts {
    fn default() -> Self {
        Layouts {
            version: LAYOUTS,
            spaces: BTreeMap::new(),
        }
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
    /// the agent's block is viewed (not those beside it; reading it takes the dwell, `markers`), and the
    /// zoom opens their transcripts and streams the space (`transcript::show`).
    View {
        space: String,
        agent: Option<String>,
        beside: Vec<String>,
    },
    /// `m`: every unread agent in the space is marked read, and its block viewed.
    Read(String),
    /// `u`: every agent in the space on the board is marked unread, until left and come back to.
    Unread(String),
    /// `alt-u` on the focused agent: marked read if unread, else unread (web's toggle).
    Toggle(String),
    /// Put a space (by id) in a lens row.
    SetRow { space: String, row: Row },
    /// Show the space's (by id) next agent on its card.
    CycleVisible(String),
    /// A preview tab pinned (DK2): the agent joins the space, last (a `spaces.members` write).
    Pin {
        space: String,
        agent: String,
        stamp: Stamp,
    },
    /// A pinned tab closed: the agent leaves the space.
    Unpin {
        space: String,
        agent: String,
        stamp: Stamp,
    },
}

/// The lens: spaces in rows, the agent each card shows, and where `n` goes next.
impl Store {
    pub(super) fn lens_move(&mut self, m: Move, out: &mut Vec<Effect>) {
        let (fleet, prefs) = (&self.fleet, &mut self.prefs);
        let space = |id: &str| self.spaces.iter().find(|s| s.id == id);
        let changed = match m {
            Move::Pin {
                space,
                agent,
                stamp,
            } => {
                let joins = |m: &mut Vec<Member>| {
                    let member = agent_member(&agent);
                    if !m.contains(&member) {
                        m.push(member);
                    }
                };
                return self.members_edit(&space, stamp, joins, out);
            }
            Move::Unpin {
                space,
                agent,
                stamp,
            } => {
                return self.members_edit(
                    &space,
                    stamp,
                    |m| m.retain(|m| *m != agent_member(&agent)),
                    out,
                );
            }
            Move::View {
                space,
                agent,
                beside,
            } => {
                let blocks = &mut prefs.blocks;
                let viewed = agent.as_ref().is_some_and(|a| view_block(blocks, fleet, a));
                self.show(&space, agent.as_deref(), &beside, out);
                viewed
            }
            Move::Read(id) => {
                let agents: Vec<String> =
                    space(&id).map_or(Vec::new(), |s| live(s, fleet).map(String::from).collect());
                let viewed = agents
                    .iter()
                    .fold(false, |c, a| view_block(&mut prefs.blocks, fleet, a) | c);
                self.mark(Mark::Read(agents, None), out);
                viewed
            }
            Move::Unread(id) => {
                let agents: Vec<String> =
                    space(&id).map_or(Vec::new(), |s| live(s, fleet).map(String::from).collect());
                return self.mark(Mark::Unread(agents), out);
            }
            Move::Toggle(agent) => return self.mark(Mark::Toggle(agent), out),
            Move::SetRow { space, row } => prefs.rows.insert(space, row) != Some(row),
            Move::CycleVisible(id) => space(&id).is_some_and(|s| cycle_visible(s, fleet, prefs)),
        };
        if changed {
            out.push(Effect::Persist(Persist::Prefs));
        }
    }

    /// Write `space`'s members as `edit` leaves them, files and all (web's `{members, updated}` row),
    /// if that changes them.
    fn members_edit(
        &mut self,
        space: &str,
        stamp: Stamp,
        edit: impl FnOnce(&mut Vec<Member>),
        out: &mut Vec<Effect>,
    ) {
        let Some(s) = self.spaces.iter().find(|s| s.id == space) else {
            return;
        };
        let mut members = s.members.clone();
        edit(&mut members);
        if members == s.members {
            return;
        }
        let previous = self.sync[&Ns::Members]
            .rows
            .get(space)
            .map_or(0, |r| r.updated);
        let updated = stamp.now.max(previous + 1);
        let value = serde_json::json!({ "members": members, "updated": updated });
        let row = StateRow {
            key: space.to_string(),
            value,
            updated,
            write_id: stamp.write,
            deleted: false,
        };
        self.sync_step(Ns::Members, SyncStep::Edit(vec![row]), out);
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

fn agent_member(name: &str) -> Member {
    let name = name.to_string();
    Member::Agent { name }
}
