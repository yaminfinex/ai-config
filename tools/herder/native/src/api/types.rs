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

/// One `/api/events` frame decoded into what it means.
#[derive(Clone, Debug)]
pub enum Wire {
    /// The first frame of every connection; a changed identity means the server was updated.
    Hello(Hello),
    /// A full board snapshot.
    Fleet(Board),
    /// A pull nudge: `namespace` changed and now stands at `rev`.
    StateChanged(StateChanged),
    /// `entry:<agent>`: a new entry for a subscribed agent; it only means "read forward from
    /// `next_offset`", so its data is never decoded.
    Entry(String),
    /// A subscribed agent's session or transcript position reset.
    Rewindow(Rewindow),
    /// An hcom message; its recipients may now have it queued.
    Message(Message),
    Ping,
    /// A type this client does not use (`substrate`, `file-change`, newer ones), or a frame
    /// whose data did not decode.
    Other(String),
}

#[derive(Clone, Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Hello {
    pub build_identity: String,
}

#[derive(Clone, Debug, Deserialize)]
pub struct StateChanged {
    pub namespace: String,
    pub rev: u64,
}

#[derive(Clone, Debug, Default, Deserialize)]
#[serde(default)]
pub struct Message {
    pub to: Vec<String>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct Rewindow {
    pub agent: String,
}

impl Wire {
    /// Decode one frame's `event:` and `data:` (on the stream's thread, so the board's JSON never costs
    /// the foreground anything).
    pub fn decode(event: &str, data: &str) -> Wire {
        let decoded = match event {
            "hello" => serde_json::from_str(data).map(Wire::Hello),
            "fleet" => serde_json::from_str(data).map(Wire::Fleet),
            "state-changed" => serde_json::from_str(data).map(Wire::StateChanged),
            "rewindow" => serde_json::from_str(data).map(Wire::Rewindow),
            "message" => serde_json::from_str(data).map(Wire::Message),
            "ping" => Ok(Wire::Ping),
            _ => match event.strip_prefix("entry:") {
                Some(agent) => Ok(Wire::Entry(agent.to_string())),
                None => return Wire::Other(event.to_string()),
            },
        };
        decoded.unwrap_or_else(|_| Wire::Other(event.to_string()))
    }
}

/// `GET /api/agents/{name}`: the detail the transcript header needs.
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct AgentDetail {
    pub name: String,
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
    pub used_percent: Option<f64>,
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct Queued {
    pub sender: String,
    pub preview: String,
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
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct Reset {}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default, rename_all = "camelCase")]
pub struct Entry {
    pub byte_offset: u64,
    pub kind: Kind,
    pub payload: Payload,
}

/// An entry payload's read fields, each still any JSON (`Null` when absent), so an odd leaf never fails
/// a page. The rest (`toolUseResult`, `attachment`, compaction histories: about half the bytes) is
/// skipped while decoding, never built as a `Value`.
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct Payload {
    #[serde(rename = "compactMetadata")]
    pub compact_metadata: Value,
    #[serde(rename = "fallbackModel")]
    pub fallback_model: Value,
    #[serde(rename = "isApiErrorMessage")]
    pub is_api_error_message: Value,
    pub message: Value,
    pub deliveries: Value,
    pub name: Value,
    pub input: Value,
    pub tool_use_id: Value,
    pub content: Value,
    pub is_error: Value,
    pub subtype: Value,
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
    /// Absent, not null, when unset: web's record omits them.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub quote: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub source: Option<Value>,
    pub created: i64,
}

/// The JSON refusal body: `{error, detail}` with a 4xx/5xx status.
#[derive(Clone, Debug, Default, Deserialize, Serialize, PartialEq, Eq)]
#[serde(default)]
pub struct Refusal {
    pub error: String,
    pub detail: String,
}

/// `GET /api/resolve?q=&agent=`: ranked candidates for a path-like mention, and each root's outcome.
#[derive(Clone, Debug, Default, Deserialize, Serialize, PartialEq)]
#[serde(default)]
pub struct Resolved {
    pub candidates: Vec<Candidate>,
    pub roots: Vec<ResolveRoot>,
}

/// `root` is absolute; `kind` is `file` or `dir`; `tier` is `exact`, `prefix`, `suffix` or `fuzzy`.
#[derive(Clone, Debug, Default, Deserialize, Serialize, PartialEq)]
#[serde(default)]
pub struct Candidate {
    pub root: String,
    pub path: String,
    pub kind: String,
    pub tier: String,
}

/// `status` is `complete`, `degraded` or `failed`.
#[derive(Clone, Debug, Default, Deserialize, Serialize, PartialEq)]
#[serde(default)]
pub struct ResolveRoot {
    pub status: String,
}
