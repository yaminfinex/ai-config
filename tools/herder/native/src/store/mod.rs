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

use crate::api::{Board, StateRow, Wire};
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use sync::{Ns, Step, Syncs};

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
    /// `GET /api/viewer`: the attributed name, or the failure's HTTP status (`None`: transport).
    Viewer(Result<String, Option<u16>>),
    /// The viewer's backoff elapsed.
    ViewerRetry,
    /// A state namespace's network answer, backoff or local edit.
    Sync {
        ns: Ns,
        step: Step,
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
    /// The shell saves the current outbox first and posts only once that save succeeded; a failed
    /// save comes back as `Step::PostFailed(None)`.
    Send(Write),
    /// Dispatch `Event::Sync { ns, step: Step::Retry }` after this long.
    Retry {
        ns: Ns,
        after_ms: u64,
    },
    /// Dispatch `Event::ViewerRetry` after this long.
    RetryViewer {
        after_ms: u64,
    },
    Persist(Persist),
}

/// Who this Mac's writes are attributed to (`GET /api/viewer`).
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub enum Attribution {
    /// Not asked yet, or the request failed in transport or with a server fault (5xx); asked again
    /// after a backoff (500 ms doubling to 10 s) and on every `hello`.
    #[default]
    Unknown,
    Attributed(String),
    /// The server refused (409 on loopback or an unattributed peer); writes will be refused too.
    Refused,
}

#[derive(Clone, Debug, Default)]
pub struct Store {
    pub conn: Conn,
    /// The server's build changed since this app first connected: show "server updated".
    pub server_updated: bool,
    pub viewer: Attribution,
    pub prefs: Prefs,
    pub fleet: fleet::Fleet,
    pub spaces: Vec<spaces::Space>,
    pub notes: Vec<notes::Note>,
    pub sync: Syncs,
    first_build: Option<String>,
    /// Live data has arrived; a snapshot is refused from here on.
    live: bool,
    stream: u64,
    viewer_asked: bool,
    viewer_retry: bool,
    /// The next viewer backoff; 0 until the first failure.
    viewer_backoff_ms: u64,
}

impl Store {
    /// Reduce one event. Returns the effects it implies, in order.
    pub fn apply(&mut self, event: Event) -> Vec<Effect> {
        let mut out = Vec::new();
        match event {
            Event::PrefsLoaded(p) => self.prefs = p,
            Event::Snapshot(snap) => {
                if !self.live {
                    self.sync.restore(snap.rows, false);
                    self.derive();
                    self.board(snap.board, &mut out);
                }
            }
            Event::OutboxLoaded(outbox) => {
                self.sync.restore(outbox, true);
                self.derive();
            }
            Event::Boot => {
                self.stream += 1;
                out.push(Effect::Stream {
                    generation: self.stream,
                    agents: Vec::new(),
                });
                self.catch_up(&mut out);
            }
            Event::Stream { generation, event } if generation == self.stream => match event {
                StreamEvent::Frame(wire) => self.wire(wire, &mut out),
                StreamEvent::Dropped => self.conn = Conn::Offline,
            },
            Event::Stream { .. } => {}
            Event::Viewer(v) => {
                self.viewer_asked = false;
                self.viewer = match v {
                    Ok(name) => Attribution::Attributed(name),
                    Err(Some(409)) => Attribution::Refused,
                    // Transport or a server fault: ask again after a backoff (one timer at a time); a
                    // healthy stream may never send another `hello`.
                    Err(_) => {
                        if !std::mem::replace(&mut self.viewer_retry, true) {
                            let after_ms = self.viewer_backoff_ms.max(500);
                            self.viewer_backoff_ms = (after_ms * 2).min(10_000);
                            out.push(Effect::RetryViewer { after_ms });
                        }
                        Attribution::Unknown
                    }
                };
            }
            Event::ViewerRetry => {
                self.viewer_retry = false;
                self.ask_viewer(&mut out);
            }
            Event::Sync { ns, step } => {
                self.live |= matches!(step, Step::Pulled(_));
                let changes = self.sync.get_mut(ns).apply(step, &mut out);
                if changes.outbox {
                    out.push(Effect::Persist(Persist::Outbox));
                }
                if changes.rows {
                    self.derive();
                    out.push(Effect::Persist(Persist::Snapshot));
                }
            }
            Event::Seen(name) => {
                if spaces::mark_seen(&mut self.prefs.seen, &self.fleet, name) {
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
        spaces::needs_you(space, &self.fleet, &self.prefs.seen)
    }

    pub fn snapshot(&self) -> Snapshot {
        Snapshot {
            board: self.fleet.board.clone(),
            rows: self.sync.export(false),
        }
    }

    pub fn outbox(&self) -> Outbox {
        self.sync.export(true)
    }

    /// Pull every namespace from its cursor, and ask who we are until the server has said. Runs at boot
    /// and on every `hello`, the first included: a change made between a boot pull and the stream's
    /// subscription sends no nudge this client can see. Pulls already in flight coalesce.
    fn catch_up(&mut self, out: &mut Vec<Effect>) {
        self.ask_viewer(out);
        for ns in Ns::ALL {
            self.sync.get_mut(ns).pull(out);
        }
    }

    /// Ask `GET /api/viewer` while the answer is unknown, one request at a time.
    fn ask_viewer(&mut self, out: &mut Vec<Effect>) {
        if self.viewer == Attribution::Unknown && !std::mem::replace(&mut self.viewer_asked, true) {
            out.push(Effect::Fetch(Fetch::Viewer));
        }
    }

    fn wire(&mut self, wire: Wire, out: &mut Vec<Effect>) {
        match wire {
            Wire::Hello(hello) => {
                let first = self.first_build.get_or_insert(hello.build_identity.clone());
                self.server_updated |= *first != hello.build_identity;
                self.conn = Conn::Live {
                    build: hello.build_identity,
                };
                self.catch_up(out);
            }
            Wire::Fleet(board) => {
                self.live = true;
                self.board(board, out);
                out.push(Effect::Persist(Persist::Snapshot));
            }
            Wire::StateChanged(c) => {
                if let Some(ns) = Ns::from_name(&c.namespace) {
                    self.sync.get_mut(ns).changed(c.rev, out);
                }
            }
            // Transcript wakes are U3's.
            Wire::Entry { .. } | Wire::Rewindow(_) | Wire::Ping | Wire::Other(_) => {}
        }
    }

    fn board(&mut self, board: Board, out: &mut Vec<Effect>) {
        self.fleet.ingest(board);
        if spaces::baseline_seen(&mut self.prefs.seen, &self.fleet, &self.spaces) {
            out.push(Effect::Persist(Persist::Prefs));
        }
    }

    fn derive(&mut self) {
        let rows = |ns| &self.sync[&ns].rows;
        self.spaces = spaces::derive(rows(Ns::Spaces), rows(Ns::Members));
        self.notes = notes::derive(rows(Ns::Notes));
    }
}
