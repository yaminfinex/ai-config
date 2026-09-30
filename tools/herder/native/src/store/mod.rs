//! Pure state. Events in; state and effects out. No GPUI, no I/O, no clocks: the time of day arrives
//! inside events, so every reduction is deterministic and replays from fixtures.
//!
//! The one mutation rule: `Store::apply` is the only place state changes, and the shell calls it on
//! the foreground thread only. Views read `&Store`; they never hold `&mut`.
//!
//! - `fleet`: agents and their status, derived from the board.
//! - `spaces`: spaces and their members, in lens order; the lens row type.
//! - `notes`: note records; drafts live in `Prefs`.
//! - `sync`: the `/api/state` pull cursor and version-aware outbox, one per namespace.
//! - `transcript`: entries → compact items, paging cursors, tool/result pairing (U3).

pub mod fleet;
pub mod notes;
pub mod spaces;
pub mod sync;
pub mod transcript;

#[cfg(test)]
mod tests;

use crate::api::{Board, StateRow, StateRows, Wire};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use sync::{Ns, Sync};

/// Connection state, for the status line and read-only decisions.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub enum Conn {
    #[default]
    Offline,
    /// Live; `build` is the server's `hello.buildIdentity`.
    Live { build: String },
}

/// Owner preferences that live on this Mac (`local::prefs.json`).
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Prefs {
    /// App-wide text scale; every size is a token times this (see `views::theme`).
    pub text_scale: f32,
    /// The lens row per space id (U2).
    pub rows: BTreeMap<String, spaces::Row>,
    /// The visible agent per space id (U2).
    pub visible: BTreeMap<String, String>,
    /// Per agent, the latest turn end (`turn_end_id`) the owner has seen.
    pub seen: BTreeMap<String, u64>,
    /// The unsent composer text per agent (U4).
    pub drafts: BTreeMap<String, String>,
}

impl Default for Prefs {
    fn default() -> Self {
        Prefs {
            text_scale: 1.0,
            rows: BTreeMap::new(),
            visible: BTreeMap::new(),
            seen: BTreeMap::new(),
            drafts: BTreeMap::new(),
        }
    }
}

const SCALE_STEP: f32 = 1.1;
const SCALE_RANGE: std::ops::RangeInclusive<f32> = 0.7..=1.8;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TextScale {
    Bigger,
    Smaller,
    Reset,
}

/// The last board and state rows, for the first paint of the next launch (`snapshot.json`).
#[derive(Clone, Debug, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct Snapshot {
    pub board: Board,
    pub rows: BTreeMap<Ns, Vec<StateRow>>,
}

/// Unsent local edits per namespace (`outbox.json`).
pub type Outbox = BTreeMap<Ns, Vec<StateRow>>;

/// What arrived on the event stream opened under some generation.
#[derive(Clone, Debug)]
pub enum StreamEvent {
    Frame(Wire),
    /// The connection ended; the shell reconnects with backoff.
    Dropped,
}

/// Something that happened: a network result, a user action, a tick or a boot-time load.
#[derive(Clone, Debug)]
pub enum Event {
    PrefsLoaded(Prefs),
    /// The disk snapshot. Refused once anything live has arrived.
    Snapshot(Snapshot),
    OutboxLoaded(Outbox),
    /// Local state is loaded: open the stream and pull everything.
    Boot,
    /// From the stream opened as `generation`; anything from an older stream is dropped.
    Stream {
        generation: u64,
        event: StreamEvent,
    },
    /// `GET /api/viewer`: the attributed sender, or `None` when refused.
    Viewer(Option<String>),
    Pulled {
        ns: Ns,
        rows: StateRows,
    },
    /// A failed request: its HTTP status, or `None` for a transport failure.
    PullFailed {
        ns: Ns,
        status: Option<u16>,
    },
    Posted {
        ns: Ns,
    },
    PostFailed {
        ns: Ns,
        status: Option<u16>,
    },
    /// A backoff elapsed.
    Retry(Ns),
    /// A local edit to shared state: full rows with `(updated, writeID)` already set.
    Edit {
        ns: Ns,
        rows: Vec<StateRow>,
    },
    /// The owner has looked at this agent's latest turn.
    Seen(String),
    TextScale(TextScale),
}

#[derive(Clone, Debug, PartialEq)]
pub enum Fetch {
    Viewer,
    State { ns: Ns, since: u64 },
}

#[derive(Clone, Debug, PartialEq)]
pub enum Write {
    State { ns: Ns, rows: Vec<StateRow> },
}

/// A file the shell writes from the store's current state; it coalesces bursts.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Persist {
    Prefs,
    /// Written before any send that follows it in the same batch.
    Outbox,
    Snapshot,
}

/// Something the shell must now do off the store: open the stream, fetch, send, persist.
#[derive(Clone, Debug, PartialEq)]
pub enum Effect {
    /// Open the event stream under `generation`, closing any other.
    Stream {
        generation: u64,
        agents: Vec<String>,
    },
    Fetch(Fetch),
    Send(Write),
    /// Dispatch `Event::Retry(ns)` after this long.
    Retry {
        ns: Ns,
        after_ms: u64,
    },
    Persist(Persist),
}

#[derive(Clone, Debug)]
pub struct Store {
    pub conn: Conn,
    /// The server's build changed since this app first connected: show "server updated".
    pub server_updated: bool,
    pub viewer: Option<String>,
    pub prefs: Prefs,
    pub fleet: fleet::Fleet,
    pub spaces: Vec<spaces::Space>,
    pub notes: Vec<notes::Note>,
    pub sync: BTreeMap<Ns, Sync>,
    first_build: Option<String>,
    /// Live data has arrived; a snapshot is refused from here on.
    live: bool,
    stream: u64,
}

impl Default for Store {
    fn default() -> Self {
        Store {
            conn: Conn::Offline,
            server_updated: false,
            viewer: None,
            prefs: Prefs::default(),
            fleet: fleet::Fleet::default(),
            spaces: Vec::new(),
            notes: Vec::new(),
            sync: Ns::ALL.into_iter().map(|ns| (ns, Sync::new(ns))).collect(),
            first_build: None,
            live: false,
            stream: 0,
        }
    }
}

impl Store {
    /// Reduce one event. Returns the effects it implies, in order.
    pub fn apply(&mut self, event: Event) -> Vec<Effect> {
        let mut out = Vec::new();
        match event {
            Event::PrefsLoaded(p) => self.prefs = p,
            Event::Snapshot(snap) => {
                if !self.live {
                    for (ns, rows) in snap.rows {
                        self.sync_mut(ns).merge(rows);
                    }
                    self.derive();
                    self.board(snap.board, &mut out);
                }
            }
            Event::OutboxLoaded(outbox) => {
                for (ns, rows) in outbox {
                    self.sync_mut(ns).queue(rows);
                }
                self.derive();
            }
            Event::Boot => {
                self.stream += 1;
                out.push(Effect::Stream {
                    generation: self.stream,
                    agents: Vec::new(),
                });
                out.push(Effect::Fetch(Fetch::Viewer));
                for ns in Ns::ALL {
                    self.sync_op(ns, &mut out, |s, out| s.pull(out));
                }
            }
            Event::Stream { generation, event } if generation == self.stream => match event {
                StreamEvent::Frame(wire) => self.wire(wire, &mut out),
                StreamEvent::Dropped => self.conn = Conn::Offline,
            },
            Event::Stream { .. } => {}
            Event::Viewer(v) => self.viewer = v,
            Event::Pulled { ns, rows } => {
                self.live = true;
                self.sync_op(ns, &mut out, |s, out| s.pulled(rows, out));
            }
            Event::PullFailed { ns, status } => {
                self.sync_op(ns, &mut out, |s, out| s.pull_failed(status, out))
            }
            Event::Posted { ns } => self.sync_op(ns, &mut out, |s, out| s.posted(out)),
            Event::PostFailed { ns, status } => {
                self.sync_op(ns, &mut out, |s, out| s.post_failed(status, out))
            }
            Event::Retry(ns) => self.sync_op(ns, &mut out, |s, out| s.retry(out)),
            Event::Edit { ns, rows } => self.sync_op(ns, &mut out, |s, out| s.edit(rows, out)),
            Event::Seen(name) => {
                let turn = self.fleet.agents.get(&name).and_then(|a| a.turn_end);
                let mark = self.prefs.seen.entry(name).or_default();
                if let Some(turn) = turn.filter(|t| t > mark) {
                    *mark = turn;
                    out.push(Effect::Persist(Persist::Prefs));
                }
            }
            Event::TextScale(step) => {
                let s = self.prefs.text_scale;
                let next = match step {
                    TextScale::Bigger => s * SCALE_STEP,
                    TextScale::Smaller => s / SCALE_STEP,
                    TextScale::Reset => 1.0,
                };
                self.prefs.text_scale =
                    (next.clamp(*SCALE_RANGE.start(), *SCALE_RANGE.end()) * 100.0).round() / 100.0;
                out.push(Effect::Persist(Persist::Prefs));
            }
        }
        out
    }

    /// Agents in this space whose latest turn the owner has not seen.
    pub fn needs_you(&self, space: &spaces::Space) -> usize {
        space
            .agents()
            .filter(|name| {
                let seen = self.prefs.seen.get(*name).copied();
                self.fleet
                    .agents
                    .get(*name)
                    .is_some_and(|a| a.needs_you(seen))
            })
            .count()
    }

    pub fn snapshot(&self) -> Snapshot {
        Snapshot {
            board: self.fleet.board.clone(),
            rows: self.rows_by_ns(|s| &s.rows),
        }
    }

    pub fn outbox(&self) -> Outbox {
        self.rows_by_ns(|s| &s.outbox)
    }

    fn rows_by_ns(
        &self,
        pick: impl Fn(&Sync) -> &BTreeMap<String, StateRow>,
    ) -> BTreeMap<Ns, Vec<StateRow>> {
        self.sync
            .iter()
            .map(|(ns, s)| (*ns, pick(s).values().cloned().collect()))
            .filter(|(_, rows): &(Ns, Vec<StateRow>)| !rows.is_empty())
            .collect()
    }

    fn wire(&mut self, wire: Wire, out: &mut Vec<Effect>) {
        match wire {
            Wire::Hello { build_identity } => {
                match &self.first_build {
                    None => self.first_build = Some(build_identity.clone()),
                    // A reopen: catch up on whatever changed while the stream was down.
                    Some(first) => {
                        self.server_updated |= *first != build_identity;
                        for ns in Ns::ALL {
                            self.sync_op(ns, out, |s, out| s.pull(out));
                        }
                    }
                }
                self.conn = Conn::Live {
                    build: build_identity,
                };
            }
            Wire::Fleet(board) => {
                self.live = true;
                self.board(board, out);
                out.push(Effect::Persist(Persist::Snapshot));
            }
            Wire::StateChanged { namespace, rev } => {
                if let Some(ns) = Ns::from_name(&namespace) {
                    self.sync_op(ns, out, |s, out| s.changed(rev, out));
                }
            }
            // Transcript wakes are U3's.
            Wire::Entry { .. } | Wire::Rewindow { .. } | Wire::Ping | Wire::Other(_) => {}
        }
    }

    /// Take a board. An agent seen for the first time gets its current turn as the baseline (an unknown
    /// baseline is not a new turn); marks for agents neither on the board nor in a space are dropped.
    fn board(&mut self, board: Board, out: &mut Vec<Effect>) {
        self.fleet.ingest(board);
        let before = self.prefs.seen.clone();
        for a in self.fleet.agents.values() {
            if let Some(turn) = a.turn_end {
                self.prefs.seen.entry(a.name.clone()).or_insert(turn);
            }
        }
        let members: Vec<&str> = self.spaces.iter().flat_map(|s| s.agents()).collect();
        let agents = &self.fleet.agents;
        self.prefs
            .seen
            .retain(|name, _| agents.contains_key(name) || members.contains(&name.as_str()));
        if self.prefs.seen != before {
            out.push(Effect::Persist(Persist::Prefs));
        }
    }

    fn sync_mut(&mut self, ns: Ns) -> &mut Sync {
        self.sync.get_mut(&ns).expect("every namespace has a sync")
    }

    /// Run one sync step. A changed outbox is persisted before any send the step emits; changed rows
    /// re-derive the domain and refresh the snapshot.
    fn sync_op(
        &mut self,
        ns: Ns,
        out: &mut Vec<Effect>,
        op: impl FnOnce(&mut Sync, &mut Vec<Effect>),
    ) {
        let s = self.sync_mut(ns);
        let (rows, outbox) = (s.rows.clone(), s.outbox.clone());
        let mut effects = Vec::new();
        op(s, &mut effects);
        if s.outbox != outbox {
            out.push(Effect::Persist(Persist::Outbox));
        }
        let rows_changed = s.rows != rows;
        out.extend(effects);
        if rows_changed {
            self.derive();
            out.push(Effect::Persist(Persist::Snapshot));
        }
    }

    fn derive(&mut self) {
        let rows = |ns| &self.sync[&ns].rows;
        self.spaces = spaces::derive(rows(Ns::Spaces), rows(Ns::Members));
        self.notes = notes::derive(rows(Ns::Notes));
    }
}
