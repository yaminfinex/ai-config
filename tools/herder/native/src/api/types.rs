//! Wire models for herder serve. Field names follow the JSON; the store owns the derived shapes.

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// `GET /api/fleet` and the SSE `fleet` frame: herdr's placement, panes joined to bus agents.
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct Board {
    pub workspaces: Vec<Workspace>,
    pub unplaced: Vec<Pane>,
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct Workspace {
    pub workspace_id: String,
    pub label: String,
    pub cwd: Option<String>,
    pub tabs: Vec<Tab>,
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct Tab {
    pub tab_id: String,
    pub panes: Vec<Pane>,
}

/// A herdr pane or an unplaced/subagent row. `agent` is `-` or empty when no bus agent claims it.
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct Pane {
    pub pane_id: String,
    pub agent: String,
    pub tool: String,
    pub herdr_status: String,
    pub bus_status: String,
    pub title: Option<String>,
    pub group: Option<String>,
    pub parent_agent: Option<String>,
    pub context_used: Option<u64>,
    /// The hcom event id of the latest completed turn: monotonic, so it doubles as a read marker.
    pub turn_end_id: Option<u64>,
    pub subagents: Vec<Pane>,
}

impl Board {
    /// Every row that names a bus agent, depth-first: placed panes, their subagents, then unplaced.
    pub fn agent_rows(&self) -> impl Iterator<Item = (&Workspace, &Pane)> {
        fn walk<'a>(ws: &'a Workspace, p: &'a Pane, out: &mut Vec<(&'a Workspace, &'a Pane)>) {
            if !p.agent.is_empty() && p.agent != "-" {
                out.push((ws, p));
            }
            for s in &p.subagents {
                walk(ws, s, out);
            }
        }
        let mut out = Vec::new();
        for ws in &self.workspaces {
            for t in &ws.tabs {
                for p in &t.panes {
                    walk(ws, p, &mut out);
                }
            }
        }
        static NOWHERE: Workspace = Workspace {
            workspace_id: String::new(),
            label: String::new(),
            cwd: None,
            tabs: Vec::new(),
        };
        for p in &self.unplaced {
            walk(&NOWHERE, p, &mut out);
        }
        out.into_iter()
    }
}

/// One `/api/events` frame decoded into what it means (`sse::Wire::decode`).
#[derive(Clone, Debug)]
pub enum Wire {
    /// The first frame of every connection; a changed identity means the server was updated.
    Hello {
        build_identity: String,
    },
    /// A full board snapshot.
    Fleet(Board),
    /// A pull nudge: `namespace` changed and now stands at `rev`.
    StateChanged {
        namespace: String,
        rev: u64,
    },
    /// One new entry for a subscribed agent; it only means "read forward from `next_offset`".
    Entry {
        agent: String,
        entry: Entry,
    },
    /// A subscribed agent's session or transcript position reset.
    Rewindow {
        agent: String,
    },
    Ping,
    /// A type this client does not use (`message`, `substrate`, `file-change`, newer ones), or a frame
    /// whose data did not decode.
    Other(String),
}

/// `GET /api/agents/{name}`: the detail the transcript header needs.
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct AgentDetail {
    pub name: String,
    pub tool: String,
    pub herdr_status: String,
    pub bus_status: String,
    pub cwd: Option<String>,
    pub session_id: Option<String>,
    pub model: Option<String>,
    pub context_usage: Option<ContextUsage>,
    pub queued: Option<Vec<Queued>>,
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct ContextUsage {
    pub used_tokens: u64,
    pub window_tokens: Option<u64>,
    pub used_percent: Option<f64>,
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct Queued {
    pub id: i64,
    pub sender: String,
    pub intent: Option<String>,
    pub preview: String,
    pub sent_at: Option<String>,
}

/// `GET /api/agents/{name}/entries`. The envelope is camelCase; `reset` is snake_case.
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default, rename_all = "camelCase")]
pub struct Entries {
    pub session_id: String,
    pub window: EntriesWindow,
    pub entries: Vec<Entry>,
    /// End of the last complete line; the forward cursor.
    pub next_offset: Option<u64>,
    /// First returned entry's offset on a `before=` page; `0` means the start of the file.
    pub prev_offset: Option<u64>,
    pub reset: Option<Reset>,
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct EntriesWindow {
    /// `tail`, `from` or `before`.
    pub mode: String,
    pub from: u64,
    pub limit: u64,
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct Reset {
    /// `truncated` or `session_changed`.
    pub reason: String,
    pub session_id: Option<String>,
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default, rename_all = "camelCase")]
pub struct Entry {
    pub uuid: Option<String>,
    pub byte_offset: u64,
    pub timestamp: Option<String>,
    pub kind: Kind,
    pub payload: Value,
}

/// The server's entry kinds. Anything newer decodes as `Unknown` rather than failing the page.
#[derive(Clone, Copy, Debug, Default, Deserialize, Serialize, PartialEq, Eq, Hash)]
#[serde(rename_all = "snake_case")]
pub enum Kind {
    HumanPrompt,
    HcomDeliveryStub,
    HcomDelivery,
    TaskNotification,
    InjectedSystem,
    CommandStdout,
    CompactDivider,
    AssistantText,
    Thinking,
    ToolUse,
    ToolResult,
    TurnDuration,
    SystemChip,
    #[default]
    #[serde(other)]
    Unknown,
}

/// `GET /api/state/{ns}?since=` and the body of `POST /api/state/{ns}`.
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct StateRows {
    pub rows: Vec<StateRow>,
    pub rev: u64,
}

/// One row of a state namespace. `value` is opaque to the server; last write wins on `(updated, write_id)`.
#[derive(Clone, Debug, Default, Deserialize, Serialize, PartialEq)]
#[serde(default)]
pub struct StateRow {
    pub key: String,
    pub value: Value,
    pub updated: i64,
    #[serde(rename = "writeID")]
    pub write_id: String,
    pub deleted: bool,
}

impl StateRow {
    /// The last-write-wins order: `updated`, then `writeID` (web's `compareStateVersions`).
    pub fn version_cmp(&self, other: &StateRow) -> std::cmp::Ordering {
        (self.updated, &self.write_id).cmp(&(other.updated, &other.write_id))
    }
}

/// The reply to `POST /api/state/{ns}`. `accepted` omits idempotent and losing rows, so it is never
/// used as the acknowledgement (ARCHITECTURE §6).
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct Accepted {
    pub accepted: Vec<String>,
    pub rev: u64,
}

/// `GET /api/viewer`: the web sender this connection is attributed to.
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct Viewer {
    pub viewer: String,
}

/// The `spaces` namespace value, shared with herder web.
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct SpaceValue {
    pub id: String,
    pub name: String,
    pub order: f64,
    pub created: i64,
}

/// The `spaces.members` namespace value (asks/herder-space-members.md), keyed by space id.
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct MembersValue {
    pub members: Vec<Member>,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(tag = "kind", rename_all = "lowercase")]
pub enum Member {
    Agent { name: String },
    File { root: String, path: String },
}

/// The `notes` namespace value, shared with herder web. `group` is an agent name or `general`.
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct NoteValue {
    pub id: String,
    pub group: String,
    pub text: String,
    pub quote: Option<String>,
    pub source: Option<Value>,
    pub created: i64,
}

/// The JSON refusal body: `{error, detail}` with a 4xx/5xx status.
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct Refusal {
    pub error: String,
    pub detail: String,
}
