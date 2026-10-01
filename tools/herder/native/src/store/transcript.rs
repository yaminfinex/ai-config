//! One agent's transcript as the compact view renders it (U3). Pages arrive in both directions, so this
//! is not an append-only fold (ARCHITECTURE §3).
//!
//! Rows are keyed `(byte_offset, sub)`, `sub` being the item's index within its entry (one
//! `hcom_delivery` entry carries several deliveries), so a `before=` page is an insertion and no key
//! moves. A `tool_result` fills its call's `Tool`, or waits in `orphans` until an older page brings the
//! call. `next_offset` is set only by tail and `from=` reads; `prev_offset` is seeded from the tail's
//! `window.from`, then from each `before=` page's `prevOffset` (`0`: the start of the file). Every read
//! is tagged `(agent, generation)` and a stale answer is dropped; a `reset` or `rewindow` bumps the
//! generation, clears the rows and reads the tail again.
//!
//! One transcript is live at a time, the zoomed agent's, and none on the lens. Entry wakes coalesce without a timer: one
//! forward read in flight, and a wake meanwhile asks for one more when it lands.

use super::{Effect, Fetch, Store, condense};
use crate::api::client::Page;
use crate::api::{AgentDetail, Candidate, Entries, Entry, Kind, Resolved};
use std::collections::{BTreeMap, HashMap};
use std::mem::{replace, take};

/// Entries per read. The server caps a window at 500; smaller pages keep each request fast.
pub const PAGE: u32 = 100;

pub type Key = (u64, u16);

/// What compact mode renders.
#[derive(Clone, Debug, PartialEq)]
pub enum Item {
    /// The owner's own message, boxed.
    Prompt(String),
    /// `operator`: sent through the web operator envelope; `quiet`: bus traffic (an ack, the
    /// launcher) that web folds into activity.
    Delivery {
        sender: String,
        text: String,
        operator: bool,
        quiet: bool,
    },
    TaskNotification(String),
    SystemChip(String),
    CompactDivider(String),
    Assistant {
        markdown: String,
    },
    /// A folded pill; often empty (redacted reasoning).
    Thinking(String),
    Tool {
        name: String,
        summary: String,
        result: Option<ToolResult>,
    },
    Error(String),
}

/// A tool's result: whether it failed, and its first line.
#[derive(Clone, Debug, PartialEq)]
pub struct ToolResult {
    pub error: bool,
    pub text: String,
}

/// A read, tagged with the transcript it was made for.
#[derive(Clone, Debug, PartialEq)]
pub struct Read {
    pub agent: String,
    pub generation: u64,
    pub what: What,
}

#[derive(Clone, Debug, PartialEq)]
pub enum What {
    Page(Page),
    Detail,
    /// A mentioned path, its `:line` split off, and whether to scope it to the agent (`agent=`): only
    /// for a live agent, as the serve rejects names off the roster.
    Resolve(String, Option<u32>, bool),
}

/// The request a read makes, for its retries and its notice.
#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Op {
    /// The tail, or a `from=` read.
    Forward,
    Back,
    Detail,
    Resolve,
}

impl What {
    fn op(&self) -> Op {
        match self {
            What::Page(Page::Before { .. }) => Op::Back,
            What::Page(_) => Op::Forward,
            What::Detail => Op::Detail,
            What::Resolve(..) => Op::Resolve,
        }
    }
}

/// A failed forward or detail read's retry, due after its backoff. Only the op's pending `token`
/// fires: a success or a `hello` since supersedes it.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Timer {
    pub generation: u64,
    pub op: Op,
    pub token: u64,
}

#[derive(Clone, Debug)]
pub enum Got {
    Page(Box<Entries>),
    Detail(Box<AgentDetail>),
    Resolved(Resolved),
}

#[derive(Clone, Debug)]
pub enum Step {
    /// The zoom closed: drop the transcript and stop streaming its agents.
    Hide,
    /// The viewport neared the first rows: read the page before.
    Older,
    Read(Read, Result<Got, String>),
    /// A clicked path (`src/x.rs:12`): resolve it, then open it.
    OpenPath(String),
    /// Open the agent's working directory.
    OpenCwd,
    Dismiss,
    /// A failed forward or detail read's backoff ran out: read it again.
    Retry(Timer),
    /// The view started (or stopped) following the bottom: the owner is watching the tail.
    Tail(bool),
}

#[derive(Clone, Debug, Default)]
pub struct Transcript {
    pub agent: String,
    pub generation: u64,
    pub session: Option<String>,
    pub items: BTreeMap<Key, Item>,
    calls: HashMap<String, Key>,
    orphans: HashMap<String, ToolResult>,
    pub next_offset: Option<u64>,
    pub prev_offset: Option<u64>,
    /// Reads in flight: tail or forward (`again`: a wake came meanwhile), backward, detail.
    reading: bool,
    again: bool,
    back: bool,
    detail_reading: bool,
    detail_again: bool,
    pub detail: Option<AgentDetail>,
    /// The view follows the bottom (`Step::Tail`): what lands is seen as it arrives (`attention`).
    pub tail: bool,
    /// The last failed read, or a path that resolved to nothing to open, and its op. A failed read
    /// holds paging back until that op succeeds, `Dismiss` or a `hello`.
    notice: Option<(Op, String)>,
    /// Each op's own retries, so a sibling's success leaves them alone.
    forward_retry: Backoff,
    detail_retry: Backoff,
    timers: u64,
}

/// Retries of a failed forward or detail read, after 1, 2 and 4 s; then a `hello` or a wake.
const RETRIES: u32 = 3;

/// One op's consecutive failures, and its one pending timer.
#[derive(Clone, Copy, Debug, Default)]
struct Backoff {
    failures: u32,
    timer: Option<u64>,
}

/// The open transcript, the stream's `agents=` set, and the counter behind every generation.
#[derive(Clone, Debug, Default)]
pub struct Live {
    pub open: Option<Transcript>,
    subscribed: Vec<String>,
    generations: u64,
}

impl Store {
    /// The zoom shows `agent` in `space` (`Move::View`): subscribe the stream to the space's agents (and
    /// a previewed outsider), and open the agent's transcript unless it is the open one. A zoom with no
    /// agent (an empty space) shows none: as zoomed out.
    pub(super) fn show(&mut self, space: &str, agent: Option<&str>, out: &mut Vec<Effect>) {
        let Some(agent) = agent else {
            return self.transcript_step(Step::Hide, out);
        };
        let space = self.spaces.iter().filter(|s| s.id == space);
        let mut agents: Vec<String> = space.flat_map(|s| s.agents().map(String::from)).collect();
        if !agents.iter().any(|a| a == agent) {
            agents.push(agent.to_string());
        }
        agents.sort();
        self.subscribe(agents, out);
        let live = &mut self.transcript;
        // Another agent, or one whose tail never arrived (and is not being read): open afresh.
        let stale = |t: &Transcript| t.agent != agent || !t.loaded() && !t.reading;
        if live.open.as_ref().is_none_or(stale) {
            live.generations += 1;
            let agent = agent.to_string();
            let mut t = Transcript {
                agent,
                ..Transcript::default()
            };
            t.reset(live.generations, out);
            live.open = Some(t);
        }
    }

    /// Stream exactly `agents`' entries: a new connection whenever the set changes.
    fn subscribe(&mut self, agents: Vec<String>, out: &mut Vec<Effect>) {
        if agents != self.transcript.subscribed {
            self.stream += 1;
            self.transcript.subscribed = agents.clone();
            out.push(Effect::Stream {
                generation: self.stream,
                agents,
            });
        }
    }

    pub(super) fn transcript_step(&mut self, step: Step, out: &mut Vec<Effect>) {
        if let Step::Hide = step {
            self.transcript.open = None;
            return self.subscribe(Vec::new(), out);
        }
        let (live, agents) = (&mut self.transcript, &self.fleet.agents);
        let Some(t) = live.open.as_mut() else { return };
        match step {
            Step::Hide => {}
            Step::Older => t.older(out),
            Step::Read(read, _) if read.agent != t.agent || read.generation != t.generation => {}
            Step::Read(_, Ok(Got::Page(e))) if e.reset.is_some() => {
                live.generations += 1;
                t.reset(live.generations, out);
            }
            Step::Read(read, result) => t.answer(read.what, result, out),
            Step::OpenPath(mention) => {
                let (path, line) = split_line(&mention);
                let scoped = agents.contains_key(&t.agent) && !t.retired();
                t.read(What::Resolve(path, line, scoped), out);
            }
            Step::OpenCwd => {
                let cwd = t.detail.as_ref().and_then(|d| d.cwd.clone());
                out.extend(cwd.map(|path| Effect::OpenFile { path, line: None }));
            }
            Step::Dismiss => t.notice = None,
            Step::Retry(timer) if timer.generation == t.generation => t.retry(timer, out),
            Step::Retry(_) => {}
            Step::Tail(tail) => t.tail = tail,
        }
    }

    /// An `entry:` wake for `agent`, or (`None`) a new `hello`: read forward, and the detail again.
    pub(super) fn transcript_wake(&mut self, agent: Option<&str>, out: &mut Vec<Effect>) {
        let open = self.transcript.open.as_mut();
        if let Some(t) = open.filter(|t| agent.is_none_or(|a| a == t.agent)) {
            if agent.is_none() {
                t.notice = None;
                (t.forward_retry, t.detail_retry) = Default::default();
            }
            t.forward(out);
            t.refresh(out);
        }
    }

    /// A `message` frame: a message addressed to the open agent may now be queued for it.
    pub(super) fn transcript_message(&mut self, to: &[String], out: &mut Vec<Effect>) {
        let open = self.transcript.open.as_mut();
        if let Some(t) = open.filter(|t| to.contains(&t.agent)) {
            t.refresh(out);
        }
    }

    /// The agent's session or position reset: throw the rows away and read the tail again.
    pub(super) fn transcript_rewindow(&mut self, agent: &str, out: &mut Vec<Effect>) {
        let live = &mut self.transcript;
        if let Some(t) = live.open.as_mut().filter(|t| t.agent == agent) {
            live.generations += 1;
            t.reset(live.generations, out);
        }
    }
}

impl Transcript {
    /// Every entry back to byte 0 is here.
    pub fn at_start(&self) -> bool {
        self.prev_offset == Some(0)
    }

    /// The first page has arrived.
    pub fn loaded(&self) -> bool {
        self.session.is_some()
    }

    pub fn paging(&self) -> bool {
        self.back
    }

    pub fn notice(&self) -> Option<&str> {
        self.notice.as_ref().map(|(_, text)| text.as_str())
    }

    /// A failed read holds paging back, so a dead serve is not asked again on every frame.
    pub fn blocked(&self) -> bool {
        matches!(self.notice, Some((op, _)) if op != Op::Resolve)
    }

    pub fn retired(&self) -> bool {
        let detail = self.detail.as_ref();
        detail.is_some_and(|d| d.bus_status == "retired")
    }

    fn read(&self, what: What, out: &mut Vec<Effect>) {
        let (agent, generation) = (self.agent.clone(), self.generation);
        let read = Read {
            agent,
            generation,
            what,
        };
        out.push(Effect::Fetch(Fetch::Transcript(read)));
    }

    /// A fresh window under `generation` (the detail stays), reading the tail and the detail.
    fn reset(&mut self, generation: u64, out: &mut Vec<Effect>) {
        let (agent, detail) = (take(&mut self.agent), self.detail.take());
        *self = Transcript {
            agent,
            generation,
            detail,
            ..Transcript::default()
        };
        self.forward(out);
        self.refresh(out);
    }

    /// The tail when nothing is loaded, else everything after `next_offset`; one read at a time.
    fn forward(&mut self, out: &mut Vec<Effect>) {
        if replace(&mut self.reading, true) {
            self.again = true;
            return;
        }
        let page = match (self.session.clone(), self.next_offset) {
            (Some(session), Some(offset)) => Page::From {
                offset,
                session,
                limit: PAGE,
            },
            _ => Page::Tail { limit: PAGE },
        };
        self.read(What::Page(page), out);
    }

    fn older(&mut self, out: &mut Vec<Effect>) {
        let (Some(session), Some(offset)) = (self.session.clone(), self.prev_offset) else {
            return;
        };
        if offset > 0 && !self.blocked() && !replace(&mut self.back, true) {
            let page = Page::Before {
                offset,
                session,
                limit: PAGE,
            };
            self.read(What::Page(page), out);
        }
    }

    /// The detail (model, context, cwd, queued messages), one read at a time.
    fn refresh(&mut self, out: &mut Vec<Effect>) {
        if replace(&mut self.detail_reading, true) {
            self.detail_again = true;
        } else {
            self.read(What::Detail, out);
        }
    }

    fn answer(&mut self, what: What, result: Result<Got, String>, out: &mut Vec<Effect>) {
        match (what, result) {
            (What::Page(page), Ok(Got::Page(e))) => self.page(page, *e, out),
            (What::Detail, Ok(Got::Detail(d))) => {
                (self.detail, self.detail_reading) = (Some(*d), false);
                self.succeeded(Op::Detail);
                if take(&mut self.detail_again) {
                    self.refresh(out);
                }
            }
            (What::Resolve(query, line, _), Ok(Got::Resolved(r))) => match pick(&r) {
                // VS Code opens a remote path as a folder unless it ends in `:<line>`.
                Some((path, file)) => {
                    let line = if file { line.or(Some(1)) } else { None };
                    out.push(Effect::OpenFile { path, line });
                }
                None => {
                    let text = format!("no single file matches {query}");
                    self.notice = Some((Op::Resolve, text));
                }
            },
            (what, result) => {
                // A failed forward or detail read is owed again (with any wake queued behind it)
                // after its op's backoff; a failed page back waits until the notice clears.
                let op = what.op();
                match op {
                    Op::Forward => (self.reading, self.again) = (false, true),
                    Op::Back => self.back = false,
                    Op::Detail => (self.detail_reading, self.detail_again) = (false, true),
                    Op::Resolve => {}
                }
                let (generation, token) = (self.generation, self.timers + 1);
                let backoff = self.backoff(op);
                if let Some(b) = backoff.filter(|b| b.timer.is_none() && b.failures < RETRIES) {
                    let after_ms = 1000 << b.failures;
                    (b.failures, b.timer, self.timers) = (b.failures + 1, Some(token), token);
                    let timer = Timer {
                        generation,
                        op,
                        token,
                    };
                    out.push(Effect::RetryTranscript { timer, after_ms });
                }
                self.notice = result.err().map(|e| (op, format!("could not read: {e}")));
            }
        }
    }

    fn backoff(&mut self, op: Op) -> Option<&mut Backoff> {
        match op {
            Op::Forward => Some(&mut self.forward_retry),
            Op::Detail => Some(&mut self.detail_retry),
            Op::Back | Op::Resolve => None,
        }
    }

    /// `op` read: its retries start over, any pending timer is void, and its own notice goes.
    fn succeeded(&mut self, op: Op) {
        if let Some(b) = self.backoff(op) {
            *b = Backoff::default();
        }
        if self.notice.as_ref().is_some_and(|(o, _)| *o == op) {
            self.notice = None;
        }
    }

    /// The op's pending timer fired: read again what failed, and any wake queued behind it.
    fn retry(&mut self, timer: Timer, out: &mut Vec<Effect>) {
        let pending = self.backoff(timer.op);
        match pending.filter(|b| b.timer == Some(timer.token)) {
            Some(b) => b.timer = None,
            None => return,
        }
        match timer.op {
            Op::Forward if take(&mut self.again) => self.forward(out),
            Op::Detail if take(&mut self.detail_again) => self.refresh(out),
            _ => {}
        }
    }

    fn page(&mut self, page: Page, e: Entries, out: &mut Vec<Effect>) {
        let full = e.entries.len() >= PAGE as usize;
        match page {
            Page::Tail { .. } => {
                self.session = Some(e.session_id);
                (self.next_offset, self.prev_offset) = (e.next_offset, Some(e.window.from));
            }
            Page::From { .. } => self.next_offset = e.next_offset.or(self.next_offset),
            Page::Before { .. } => {
                (self.back, self.prev_offset) = (false, Some(e.prev_offset.unwrap_or(0)));
            }
        }
        let back = matches!(page, Page::Before { .. });
        self.succeeded(if back { Op::Back } else { Op::Forward });
        e.entries.into_iter().for_each(|entry| self.ingest(entry));
        // A window of only hidden entries shows nothing: keep reading back until rows or the start.
        if self.items.is_empty() && !matches!(page, Page::From { .. }) {
            self.older(out);
        }
        if !matches!(page, Page::Before { .. }) {
            self.reading = false;
            // More may be waiting: a wake came meanwhile, or a catch-up filled its window.
            if take(&mut self.again) || (full && matches!(page, Page::From { .. })) {
                self.forward(out);
            }
        }
    }

    fn ingest(&mut self, entry: Entry) {
        let (offset, p) = (entry.byte_offset, &entry.payload);
        let id = p.tool_use_id.as_str().unwrap_or("").to_string();
        match entry.kind {
            Kind::ToolUse => {
                let (name, summary) = condense::tool_call(p);
                let result = self.orphans.remove(&id);
                let tool = Item::Tool {
                    name,
                    summary,
                    result,
                };
                self.items.insert((offset, 0), tool);
                self.calls.insert(id, (offset, 0));
            }
            Kind::ToolResult => {
                let result = condense::tool_result(p);
                match self.calls.get(&id).and_then(|k| self.items.get_mut(k)) {
                    Some(Item::Tool { result: slot, .. }) => *slot = Some(result),
                    _ => drop(self.orphans.insert(id, result)),
                }
            }
            _ => {
                for (sub, item) in condense::condense(&entry).into_iter().enumerate() {
                    self.items.insert((offset, sub as u16), item);
                }
            }
        }
    }
}

/// What to open, and whether it is a file, as web's auto-open: every root answered completely and there
/// is exactly one exact or suffix candidate. Anything else is a notice (Rung 1 has no chooser).
fn pick(r: &Resolved) -> Option<(String, bool)> {
    let complete = r.roots.iter().all(|root| root.status == "complete");
    let strong = |c: &&Candidate| c.tier == "exact" || c.tier == "suffix";
    let mut strong = r.candidates.iter().filter(strong);
    let c = strong
        .next()
        .filter(|_| strong.next().is_none() && complete)?;
    let path = format!("{}/{}", c.root.trim_end_matches('/'), c.path);
    Some((path, c.kind == "file"))
}

/// `src/x.rs:12` or `src/x.rs:12:4` → the path and its line.
pub fn split_line(mention: &str) -> (String, Option<u32>) {
    let mut parts: Vec<&str> = mention.rsplitn(3, ':').collect();
    parts.reverse();
    let digits = |p: &&str| !p.is_empty() && p.bytes().all(|b| b.is_ascii_digit());
    match parts.as_slice() {
        [path, line, rest @ ..] if !path.is_empty() && digits(line) && rest.iter().all(digits) => {
            (path.to_string(), line.parse().ok())
        }
        _ => (mention.to_string(), None),
    }
}
