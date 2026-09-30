//! Agents and their status, derived from each full board the server sends.

use crate::api::{Board, Pane, Workspace};
use std::collections::BTreeMap;

/// One bus agent as the lens sees it. Keyed by the full bus name.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Agent {
    pub name: String,
    pub tool: String,
    pub herdr_status: String,
    pub bus_status: String,
    pub title: Option<String>,
    pub group: Option<String>,
    pub parent: Option<String>,
    /// The herdr workspace label; empty when unplaced.
    pub workspace: String,
    pub pane_id: Option<String>,
    pub context_used: Option<u64>,
}

/// What the cards and notifications key on. Derived, never stored.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Status {
    Working,
    /// A turn just ended.
    Done,
    Idle,
    Blocked,
}

impl Agent {
    pub fn status(&self) -> Status {
        match (self.herdr_status.as_str(), self.bus_status.as_str()) {
            (_, "blocked") | ("blocked", _) => Status::Blocked,
            ("working", _) => Status::Working,
            ("done", _) => Status::Done,
            _ => Status::Idle,
        }
    }

    fn from_row(ws: &Workspace, p: &Pane) -> Self {
        Agent {
            name: p.agent.clone(),
            tool: p.tool.clone(),
            herdr_status: p.herdr_status.clone(),
            bus_status: p.bus_status.clone(),
            title: p.title.clone(),
            group: p.group.clone(),
            parent: p.parent_agent.clone(),
            workspace: ws.label.clone(),
            pane_id: (p.pane_id != "-" && !p.pane_id.is_empty()).then(|| p.pane_id.clone()),
            context_used: p.context_used,
        }
    }
}

#[derive(Clone, Debug, Default)]
pub struct Fleet {
    pub agents: BTreeMap<String, Agent>,
}

impl Fleet {
    /// Replace the fleet with a board. Agents missing from the board are gone (retired or culled).
    pub fn ingest(&mut self, board: &Board) {
        self.agents = board
            .agent_rows()
            .map(|(ws, p)| (p.agent.clone(), Agent::from_row(ws, p)))
            .collect();
    }
}
