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

#[derive(Clone, Debug)]
pub enum Got {
    Page(Box<Entries>),
    Detail(Box<AgentDetail>),
    Resolved(Resolved),
}

#[derive(Clone, Debug)]
pub enum Step {
    /// The zoom shows this agent of this space (or previews it there).
    Show {
        space: String,
        agent: String,
    },
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
    /// A failed tail, forward or detail read's backoff ran out (for this generation): read again.
    Retry(u64),
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
    /// The last failed read, or a path that resolved to nothing to open. While set, paging back
    /// waits: `Dismiss`, a `hello` or a landed page clear it.
    pub notice: Option<String>,
    /// Consecutive failed tail, forward or detail reads, each retried after a backoff up to `RETRIES`.
    failures: u32,
}

/// Retries of a failed tail, forward or detail read, after 1, 2 and 4 s; then a `hello` or a wake.
const RETRIES: u32 = 3;

/// The open transcript, the stream's `agents=` set, and the counter behind every generation.
#[derive(Clone, Debug, Default)]
pub struct Live {
    pub open: Option<Transcript>,
    subscribed: Vec<String>,
    generations: u64,
}

impl Store {
    /// The zoom shows `agent` in `space`: subscribe the stream to the space's agents (and a previewed
    /// outsider), and open the agent's transcript unless it is the open one.
    fn show(&mut self, space: &str, agent: &str, out: &mut Vec<Effect>) {
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
        if let Step::Show { space, agent } = &step {
            return self.show(space, agent, out);
        }
        if let Step::Hide = step {
            self.transcript.open = None;
            return self.subscribe(Vec::new(), out);
        }
        let (live, agents) = (&mut self.transcript, &self.fleet.agents);
        let Some(t) = live.open.as_mut() else { return };
        match step {
            Step::Show { .. } | Step::Hide => {}
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
            Step::Retry(generation) if generation == t.generation => {
                if take(&mut t.again) {
                    t.forward(out);
                }
                if take(&mut t.detail_again) {
                    t.refresh(out);
                }
            }
            Step::Retry(_) => {}
        }
    }

    /// An `entry:` wake for `agent`, or (`None`) a new `hello`: read forward, and the detail again.
    pub(super) fn transcript_wake(&mut self, agent: Option<&str>, out: &mut Vec<Effect>) {
        let open = self.transcript.open.as_mut();
        if let Some(t) = open.filter(|t| agent.is_none_or(|a| a == t.agent)) {
            if agent.is_none() {
                (t.notice, t.failures) = (None, 0);
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
        if offset > 0 && self.notice.is_none() && !replace(&mut self.back, true) {
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
                (self.detail, self.detail_reading, self.failures) = (Some(*d), false, 0);
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
                None => self.notice = Some(format!("no single file matches {query}")),
            },
            (what, result) => {
                // A failed tail, forward or detail read is owed again (with any wake queued behind
                // it) after a backoff; a failed page back waits until the notice clears.
                let retry = match what {
                    What::Page(Page::Before { .. }) => {
                        self.back = false;
                        false
                    }
                    What::Page(_) => {
                        (self.reading, self.again) = (false, true);
                        true
                    }
                    What::Detail => {
                        (self.detail_reading, self.detail_again) = (false, true);
                        true
                    }
                    What::Resolve(..) => false,
                };
                if retry && self.failures < RETRIES {
                    let (generation, after_ms) = (self.generation, 1000 << self.failures);
                    self.failures += 1;
                    out.push(Effect::RetryTranscript {
                        generation,
                        after_ms,
                    });
                }
                self.notice = result.err().map(|e| format!("could not read: {e}"));
            }
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
        (self.notice, self.failures) = (None, 0);
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
        let id = condense::str_at(p, "tool_use_id").to_string();
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
