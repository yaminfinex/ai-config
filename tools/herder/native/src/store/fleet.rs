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
    /// The workspace's working directory, for the zoom placeholder.
    pub cwd: Option<String>,
    /// The id of the latest completed turn (monotonic); what seen marks compare against.
    pub turn_end: Option<u64>,
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
            cwd: ws.cwd.clone(),
            turn_end: p.turn_end_id,
        }
    }

    /// The one needs-you predicate: not working, not retired or stopped, and a turn ended after `seen`
    /// (no mark: no baseline yet, nothing unread), or Blocked if `BLOCKED_ALWAYS_NEEDS_YOU`.
    pub fn needs_you(&self, seen: Option<u64>) -> bool {
        let gone = matches!(self.bus_status.as_str(), "retired" | "stopped");
        let new_turn = matches!((self.turn_end, seen), (Some(turn), Some(seen)) if turn > seen);
        let blocked = BLOCKED_ALWAYS_NEEDS_YOU && self.status() == Status::Blocked;
        !gone && self.status() != Status::Working && (new_turn || blocked)
    }
}

/// Owner policy, not yet ruled: does a Blocked agent need you even without a new turn?
pub const BLOCKED_ALWAYS_NEEDS_YOU: bool = false;

#[derive(Clone, Debug, Default)]
pub struct Fleet {
    /// The last board as received; the disk snapshot keeps it for the next cold start.
    pub board: Board,
    pub agents: BTreeMap<String, Agent>,
}

impl Fleet {
    /// Replace the fleet with a board. Agents missing from the board are gone (retired or culled).
    pub fn ingest(&mut self, board: Board) {
        self.agents = agent_rows(&board)
            .into_iter()
            .map(|(ws, p)| (p.agent.clone(), Agent::from_row(ws, p)))
            .collect();
        self.board = board;
    }
}

/// Every board row that names a bus agent, depth-first: placed panes, their subagents, then unplaced.
pub fn agent_rows(board: &Board) -> Vec<(&Workspace, &Pane)> {
    fn walk<'a>(ws: &'a Workspace, p: &'a Pane, out: &mut Vec<(&'a Workspace, &'a Pane)>) {
        if !p.agent.is_empty() && p.agent != "-" {
            out.push((ws, p));
        }
        for s in &p.subagents {
            walk(ws, s, out);
        }
    }
    static NOWHERE: Workspace = Workspace {
        workspace_id: String::new(),
        label: String::new(),
        cwd: None,
        tabs: Vec::new(),
    };
    let mut out = Vec::new();
    for ws in &board.workspaces {
        for p in ws.tabs.iter().flat_map(|t| &t.panes) {
            walk(ws, p, &mut out);
        }
    }
    for p in &board.unplaced {
        walk(&NOWHERE, p, &mut out);
    }
    out
}
