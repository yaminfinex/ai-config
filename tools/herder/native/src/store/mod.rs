//! Pure state. Events in; state and effects out. No GPUI, no I/O, no clocks: the time of day arrives
//! inside events, so every reduction is deterministic and replays from fixtures.
//!
//! The one mutation rule: `Store::apply` is the only place state changes, and the shell calls it on
//! the foreground thread only. Views read `&Store`; they never hold `&mut`.
//!
//! - `fleet`: agents and their status, derived from the board.
//! - `spaces`: spaces and their members, in lens order; the lens row type and the owner's moves.
//! - `attention`: seen marks, needs-you, the alerts and the dock badge (U2, U6).
//! - `notes`: note records and the owner's note edits, hand-off and queueing (U5).
//! - `composer`: drafts, who can be written to, and each message send (U4).
//! - `sync`: the `/api/state` pull cursor and version-aware outbox, one per namespace.
//! - `transcript`: entries → compact items, paging cursors, tool/result pairing (U3).
//! - `cards`: the lens cards' text, each visible agent's last answer, read once per turn (F4).

pub mod attention;
pub mod cards;
pub mod composer;
pub mod condense;
pub mod fleet;
pub mod notes;
pub mod spaces;
pub mod sync;
pub mod transcript;

#[cfg(test)]
pub(crate) mod tests;

use crate::api::{Board, Refusal, StateRow, Wire};
use serde::{Deserialize, Serialize};
use std::collections::{BTreeMap, BTreeSet};
use sync::{Ns, Step, Syncs};

/// Connection state, for the status line and read-only decisions.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub enum Conn {
    #[default]
    Offline,
    Live,
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
    /// Per agent, the latest turn end (`turn_end_id`) the owner has seen, and whether this block was.
    pub seen: BTreeMap<String, attention::Seen>,
    /// Spaces the owner marked unread (`u`): they need you until the next zoom-in.
    pub unread: BTreeSet<String>,
    /// The unsent composer text per agent (U4).
    pub drafts: BTreeMap<String, String>,
    /// The SSH host alias VS Code's Remote-SSH opens files on (web asks; this Mac defaults to it).
    pub vscode_host: String,
    /// The global summon chord (U6), in GPUI's syntax; no UI, edit `prefs.json`.
    pub hotkey: String,
}

impl Default for Prefs {
    fn default() -> Self {
        Prefs {
            text_scale: 1.0,
            rows: BTreeMap::new(),
            visible: BTreeMap::new(),
            seen: BTreeMap::new(),
            unread: BTreeSet::new(),
            drafts: BTreeMap::new(),
            vscode_host: "superset".into(),
            hotkey: "ctrl-alt-cmd-h".into(),
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
    /// A state namespace's network answer, backoff or local edit.
    Sync {
        ns: Ns,
        step: Step,
    },
    /// An owner move on the lens (`spaces::Move`).
    Lens(spaces::Move),
    TextScale(TextScale),
    Transcript(transcript::Step),
    Compose(composer::Step),
    Note(notes::Step),
    /// A lens card's tail read landed (`cards`).
    Card(cards::Got),
    /// The app became frontmost, or stopped being (U6): the agent zoomed in meanwhile is never notified.
    Front(bool),
    /// A timer set by `Effect::After` is up.
    Wake(Wake),
    /// The summon hotkey; the shell handles it before the store, which ignores it.
    Summon,
}

#[derive(Clone, Debug, PartialEq)]
pub enum Fetch {
    Viewer,
    State {
        ns: Ns,
        since: u64,
    },
    Transcript(transcript::Read),
    /// A text card's agent's tail, for its last answer, read for `turn` (`cards`).
    Card {
        agent: String,
        turn: Option<u64>,
    },
}

/// A file the shell writes from the store's current state; it coalesces bursts.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
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
    /// `POST /api/state/{ns}`. The shell saves the current outbox first and posts only once that save
    /// succeeded; a failed save comes back as `Step::PostFailed(None)`.
    Post {
        ns: Ns,
        rows: Vec<StateRow>,
    },
    /// `POST /api/agents/{agent}/message`, once, after the prefs (with the draft) are saved; a failed
    /// save comes back as `Failure::NotSaved`.
    Message {
        agent: String,
        text: String,
    },
    /// Dispatch `Event::Wake(wake)` after this long.
    After {
        after_ms: u64,
        wake: Wake,
    },
    Persist(Persist),
    /// Save the destination `to` now, from the store as it is, then dispatch `notes::Step::Landed` for
    /// `agent`: the destination of a note transfer is on disk before its source changes.
    Transfer {
        to: notes::Dest,
        agent: String,
    },
    /// A filed-back send (`cmd-shift-enter`) landed: leave the zoom if it is still on `agent`.
    FiledBack {
        agent: String,
    },
    /// Post a notification (U6); on the foreground, through `platform_mac`'s test-mode switch.
    Notify(attention::Notice),
    /// Show this needs-you count on the dock (0 clears it).
    Badge(usize),
    /// Open a file or folder on the agents' host in VS Code (the file panel's seam, Rung 2).
    OpenFile {
        path: String,
        line: Option<u32>,
    },
}

/// What a timer wakes (`Effect::After`): a namespace's, the transcript's or the viewer's retry, or the
/// end of a notification burst (`attention::BURST_MS`).
#[derive(Clone, Debug, PartialEq)]
pub enum Wake {
    Sync(Ns),
    Transcript(transcript::Timer),
    Viewer,
    Burst,
}

/// Who this Mac's writes are attributed to (`GET /api/viewer`).
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub enum Attribution {
    /// Not asked yet, or the request failed in transport or with a server fault (5xx); asked again
    /// after a backoff (500 ms doubling to 10 s) and on every `hello`.
    #[default]
    Unknown,
    Attributed(String),
    /// The server refused (409 on loopback or an unattributed peer); writes will be refused too. With
    /// the reason when a send was refused for it (`attribution required`, `sender refused`).
    Refused(Option<Refusal>),
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
    pub transcript: transcript::Live,
    /// Message sends in flight, or their last failure, per agent.
    pub sends: BTreeMap<String, composer::Sending>,
    /// Note hand-offs and queued drafts waiting on their destination's save, per agent.
    pub transfers: BTreeMap<String, notes::Transfer>,
    /// Per agent, why its last queue or transfer did not happen; until its next transfer.
    pub note_problems: BTreeMap<String, String>,
    pub alerts: attention::Alerts,
    pub cards: cards::Cards,
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
        let (mut out, boot) = (Vec::new(), matches!(event, Event::Boot));
        match event {
            Event::PrefsLoaded(p) => self.prefs = p,
            Event::Snapshot(snap) => {
                if !self.live {
                    self.sync.restore(snap.rows, false);
                    self.derive();
                    self.board(snap.board, false, &mut out);
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
            // A send refused for attribution outranks the answer to a GET asked before it.
            Event::Viewer(_) if matches!(self.viewer, Attribution::Refused(Some(_))) => {
                self.viewer_asked = false
            }
            Event::Viewer(v) => {
                self.viewer_asked = false;
                self.viewer = match v {
                    Ok(name) => Attribution::Attributed(name),
                    Err(Some(409)) => Attribution::Refused(None),
                    // Transport or a server fault: ask again after a backoff (one timer at a time); a
                    // healthy stream may never send another `hello`.
                    Err(_) => {
                        if !std::mem::replace(&mut self.viewer_retry, true) {
                            let after_ms = self.viewer_backoff_ms.max(500);
                            self.viewer_backoff_ms = (after_ms * 2).min(10_000);
                            let wake = Wake::Viewer;
                            out.push(Effect::After { after_ms, wake });
                        }
                        Attribution::Unknown
                    }
                };
            }
            Event::Wake(Wake::Sync(ns)) => self.sync_step(ns, Step::Retry, &mut out),
            Event::Wake(Wake::Transcript(t)) => {
                self.transcript_step(transcript::Step::Retry(t), &mut out)
            }
            Event::Wake(Wake::Viewer) => {
                self.viewer_retry = false;
                self.ask_viewer(&mut out);
            }
            Event::Sync { ns, step } => self.sync_step(ns, step, &mut out),
            Event::Lens(m) => self.lens_move(m, &mut out),
            Event::Transcript(step) => self.transcript_step(step, &mut out),
            Event::Compose(step) => self.compose(step, &mut out),
            Event::Note(step) => self.note(step, &mut out),
            Event::Card(got) => self.card_read(got),
            Event::Front(front) => self.alerts.front = front,
            Event::Wake(Wake::Burst) => self.burst_ended(&mut out),
            Event::Summon => {}
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
        self.watch(&mut out);
        self.card_reads(&mut out);
        self.transitions(&mut out);
        self.badge(boot, &mut out);
        out
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
                self.conn = Conn::Live;
                self.catch_up(out);
                self.cards_hello();
                self.transcript_wake(None, out);
            }
            Wire::Fleet(board) => {
                self.live = true;
                self.board(board, true, out);
                out.push(Effect::Persist(Persist::Snapshot));
            }
            Wire::StateChanged(c) => {
                if let Some(ns) = Ns::from_name(&c.namespace) {
                    self.sync.get_mut(ns).changed(c.rev, out);
                }
            }
            Wire::Entry(agent) => self.transcript_wake(Some(&agent), out),
            Wire::Rewindow(r) => self.transcript_rewindow(&r.agent, out),
            Wire::Message(m) => self.transcript_message(&m.to, out),
            Wire::Ping | Wire::Other(_) => {}
        }
    }

    fn board(&mut self, board: Board, live: bool, out: &mut Vec<Effect>) {
        self.fleet.ingest(board);
        self.alerts.live |= live;
        if self.reseen() {
            out.push(Effect::Persist(Persist::Prefs));
        }
    }

    /// One step of a namespace's sync; what it changed is re-derived and persisted.
    fn sync_step(&mut self, ns: Ns, step: Step, out: &mut Vec<Effect>) {
        self.live |= matches!(step, Step::Pulled(_));
        let changes = self.sync.get_mut(ns).apply(step, out);
        if changes.outbox {
            out.push(Effect::Persist(Persist::Outbox));
        }
        if changes.rows {
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
