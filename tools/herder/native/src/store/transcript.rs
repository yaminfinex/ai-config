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
//! One transcript is live at a time, the zoomed agent's. Entry wakes coalesce without a timer: one
//! forward read in flight, and a wake meanwhile asks for one more when it lands.

use super::{Effect, Fetch, Store};
use crate::api::client::Page;
use crate::api::{AgentDetail, Entries, Entry, Kind, Resolved};
use serde_json::Value;
use std::collections::{BTreeMap, HashMap};
use std::mem::{replace, take};

/// Entries per read. The server caps a window at 500; smaller pages keep each request fast.
pub const PAGE: u32 = 100;
/// Characters kept per tool line and result, and per thinking pill.
const LINE: usize = 200;
const THINKING: usize = 2000;

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
    /// A mentioned path, its `:line` split off.
    Resolve(String, Option<u32>),
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
    /// The view laid out its first rows: read the page before.
    Older,
    Read(Read, Result<Got, String>),
    /// A clicked path (`src/x.rs:12`): resolve it, then open it.
    OpenPath(String),
    /// Open the agent's working directory.
    OpenCwd,
    Dismiss,
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
    /// The last failed read, or a path that resolved to nothing to open.
    pub notice: Option<String>,
}

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
        if agents != self.transcript.subscribed {
            self.stream += 1;
            let generation = self.stream;
            self.transcript.subscribed = agents.clone();
            out.push(Effect::Stream { generation, agents });
        }
        let live = &mut self.transcript;
        if live.open.as_ref().is_none_or(|t| t.agent != agent) {
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

    pub(super) fn transcript_step(&mut self, step: Step, out: &mut Vec<Effect>) {
        if let Step::Show { space, agent } = &step {
            return self.show(space, agent, out);
        }
        let live = &mut self.transcript;
        let Some(t) = live.open.as_mut() else { return };
        match step {
            Step::Show { .. } => {}
            Step::Older => t.older(out),
            Step::Read(read, _) if read.agent != t.agent || read.generation != t.generation => {}
            Step::Read(_, Ok(Got::Page(e))) if e.reset.is_some() => {
                live.generations += 1;
                t.reset(live.generations, out);
            }
            Step::Read(read, result) => t.answer(read.what, result, out),
            Step::OpenPath(mention) => {
                let (path, line) = split_line(&mention);
                t.read(What::Resolve(path, line), out);
            }
            Step::OpenCwd => {
                let cwd = t.detail.as_ref().and_then(|d| d.cwd.clone());
                out.extend(cwd.map(|path| Effect::OpenFile { path, line: None }));
            }
            Step::Dismiss => t.notice = None,
        }
    }

    /// An `entry:` wake for `agent`, or (`None`) a new `hello`: read forward, and the detail again.
    pub(super) fn transcript_wake(&mut self, agent: Option<&str>, out: &mut Vec<Effect>) {
        let open = self.transcript.open.as_mut();
        if let Some(t) = open.filter(|t| agent.is_none_or(|a| a == t.agent)) {
            t.forward(out);
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
        if offset > 0 && !replace(&mut self.back, true) {
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
                if take(&mut self.detail_again) {
                    self.refresh(out);
                }
            }
            (What::Resolve(query, line), Ok(Got::Resolved(r))) => match pick(&r, &query) {
                Some(path) => out.push(Effect::OpenFile { path, line }),
                None => self.notice = Some(format!("no single file matches {query}")),
            },
            (what, result) => {
                match what {
                    What::Page(Page::Before { .. }) => self.back = false,
                    What::Page(_) => self.reading = false,
                    What::Detail => self.detail_reading = false,
                    What::Resolve(..) => {}
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
        e.entries.into_iter().for_each(|entry| self.ingest(entry));
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
        let id = str_at(p, "tool_use_id").to_string();
        match entry.kind {
            Kind::ToolUse => {
                let name = str_at(p, "name").to_string();
                let summary = clip(&tool_summary(&name, &p["input"]), LINE);
                // A page read twice keeps the result it already paired.
                let kept = match self.items.get(&(offset, 0)) {
                    Some(Item::Tool { result, .. }) => result.clone(),
                    _ => None,
                };
                let result = self.orphans.remove(&id).or(kept);
                let tool = Item::Tool {
                    name,
                    summary,
                    result,
                };
                self.items.insert((offset, 0), tool);
                self.calls.insert(id, (offset, 0));
            }
            Kind::ToolResult => {
                let text = clip(first_line(&text_of(&p["content"])), LINE);
                let error = p["is_error"].as_bool().unwrap_or(false);
                let result = ToolResult { error, text };
                match self.calls.get(&id).and_then(|k| self.items.get_mut(k)) {
                    Some(Item::Tool { result: slot, .. }) => *slot = Some(result),
                    _ => drop(self.orphans.insert(id, result)),
                }
            }
            _ => {
                for (sub, item) in condense(&entry).into_iter().enumerate() {
                    self.items.insert((offset, sub as u16), item);
                }
            }
        }
    }
}

/// The items one entry yields in compact mode, as web's clean view (tool pairs are `ingest`'s).
pub fn condense(entry: &Entry) -> Vec<Item> {
    let p = &entry.payload;
    let text = text_of(&p["message"]["content"]);
    let item = match entry.kind {
        Kind::HumanPrompt => Item::Prompt(text),
        Kind::HcomDelivery => {
            let deliveries = p["deliveries"].as_array().into_iter().flatten();
            return deliveries.map(delivery).collect();
        }
        Kind::TaskNotification => {
            let summary = between(&text, "<summary>", "</summary>").unwrap_or(first_line(&text));
            Item::TaskNotification(summary.trim().to_string())
        }
        // A slash command; its output (the next entry) joins it in web, so shows nothing here.
        Kind::CommandStdout => match between(&text, "<command-name>", "</command-name>") {
            Some(name) => {
                let args = between(&text, "<command-args>", "</command-args>").unwrap_or("");
                Item::SystemChip(format!("{name} {}", clip(args.trim(), 80)))
            }
            None => return Vec::new(),
        },
        Kind::CompactDivider => {
            let m = &p["compactMetadata"];
            let k = |key: &str| m[key].as_u64().map(|n| format!("{}k", n / 1000));
            Item::CompactDivider(match (k("preTokens"), k("postTokens")) {
                (Some(pre), Some(post)) => {
                    let trigger = m["trigger"].as_str().unwrap_or("auto");
                    format!("context compacted ({trigger}, {pre} → {post} tokens)")
                }
                _ => "compaction summary".into(),
            })
        }
        Kind::AssistantText if p["isApiErrorMessage"].as_bool() == Some(true) => Item::Error(text),
        Kind::AssistantText => Item::Assistant {
            markdown: clean(&text),
        },
        Kind::Thinking => return vec![Item::Thinking(clip(&text, THINKING))],
        Kind::SystemChip => Item::SystemChip(system_chip(p)),
        Kind::Unknown => {
            let label = Some(clip(first_line(&text), 80)).filter(|l| !l.is_empty());
            Item::SystemChip(label.unwrap_or_else(|| "unrecognized entry".into()))
        }
        // Carriers, telemetry, injected context and tool pairs.
        Kind::HcomDeliveryStub | Kind::InjectedSystem | Kind::TurnDuration => return Vec::new(),
        Kind::ToolUse | Kind::ToolResult => return Vec::new(),
    };
    let empty = match &item {
        Item::Prompt(s) | Item::SystemChip(s) | Item::Assistant { markdown: s } => {
            s.trim().is_empty()
        }
        _ => false,
    };
    if empty { Vec::new() } else { vec![item] }
}

fn delivery(d: &Value) -> Item {
    let (sender, raw) = (str_at(d, "sender").to_string(), str_at(d, "text"));
    let quiet = sender == "[hcom-launcher]" || str_at(d, "intent") == "ack";
    let (body, operator) = (strip_operator(raw), strip_operator(raw).is_some());
    let text = body.unwrap_or(raw).trim_end().trim_end_matches(" |");
    let text = text.trim().to_string();
    Item::Delivery {
        sender,
        text,
        operator,
        quiet,
    }
}

/// The message inside the web operator envelope, current and prerelease forms.
fn strip_operator(text: &str) -> Option<&str> {
    const FORMS: [(&str, &str); 2] = [
        (
            "[HERDER_WEB_OPERATOR_NOTE_BEGIN]",
            "[HERDER_WEB_OPERATOR_NOTE_END]",
        ),
        (
            "<<<HERDER_WEB_OPERATOR_NOTE>>>",
            "<<<END_HERDER_WEB_OPERATOR_NOTE>>>",
        ),
    ];
    FORMS.iter().find_map(|(begin, end)| {
        let rest = text.strip_prefix(begin)?;
        Some(rest[rest.find(end)? + end.len()..].trim_start_matches('\n'))
    })
}

/// Web's compact view shows only these system entries (an empty label hides the rest).
fn system_chip(p: &Value) -> String {
    let to = p["fallbackModel"].as_str().map(|m| format!(" to {m}"));
    let to = to.unwrap_or_default();
    match str_at(p, "subtype") {
        "scheduled_task_fire" => str_at(p, "content").to_string(),
        "model_refusal_fallback" => format!("model switched{to} — safeguards flagged a message"),
        "model_consent_fallback" => format!("model switched{to} — consent required"),
        _ => String::new(),
    }
}

/// `<internal>…</internal>` removed (an unclosed one hides the rest) and `<status>` tags unwrapped.
pub fn clean(text: &str) -> String {
    const CLOSE: &str = "</internal>";
    let (mut out, mut rest) = (String::with_capacity(text.len()), text);
    while let Some(at) = rest.find("<internal>") {
        out.push_str(&rest[..at]);
        let end = rest[at..].find(CLOSE).map(|e| at + e + CLOSE.len());
        rest = &rest[end.unwrap_or(rest.len())..];
    }
    out.push_str(rest);
    let out = out.replace("<status>", "").replace("</status>", "");
    out.trim().to_string()
}

/// Web's one-line tool summary: the command or file when there is one, else the first input value.
fn tool_summary(name: &str, input: &Value) -> String {
    let keys: &[&str] = match name {
        "Bash" => &["command"],
        "Edit" | "Write" | "Read" => &["file_path", "path", "file"],
        _ => &[],
    };
    let preferred = keys.iter().map(|k| &input[k]);
    let values = preferred.chain(input.as_object().into_iter().flat_map(|o| o.values()));
    let mut texts = values.map(|v| text_of(v).split_whitespace().collect::<Vec<_>>().join(" "));
    let found = texts.find(|t| !t.is_empty());
    found.unwrap_or_else(|| "no input summary".into())
}

/// The file to open: the one exact or suffix candidate when every root answered completely, else a
/// top candidate web calls confident (not fuzzy, or fuzzy scoring 20 per query character).
fn pick(r: &Resolved, query: &str) -> Option<String> {
    let complete = r.roots.iter().all(|root| root.status == "complete");
    let (all, bar) = (&r.candidates, 20.0 * query.chars().count() as f64);
    let mut strong = all
        .iter()
        .filter(|c| c.tier == "exact" || c.tier == "suffix");
    let only = strong
        .next()
        .filter(|_| strong.next().is_none() && complete);
    let top = all.first().filter(|c| c.tier != "fuzzy" || c.score >= bar);
    let c = only.or(top)?;
    Some(format!("{}/{}", c.root.trim_end_matches('/'), c.path))
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

/// A message's text: a string, or each block's `text` (or `thinking`), joined.
fn text_of<'a>(v: &'a Value) -> String {
    match v {
        Value::String(s) => s.clone(),
        Value::Array(blocks) => {
            let text = |b: &'a Value| b["text"].as_str().or(b["thinking"].as_str());
            let texts: Vec<&str> = blocks.iter().filter_map(text).collect();
            texts.join("\n")
        }
        Value::Null | Value::Object(_) => String::new(),
        other => other.to_string(),
    }
}

fn str_at<'a>(v: &'a Value, key: &str) -> &'a str {
    v[key].as_str().unwrap_or("")
}

fn between<'a>(text: &'a str, open: &str, close: &str) -> Option<&'a str> {
    let rest = &text[text.find(open)? + open.len()..];
    Some(&rest[..rest.find(close)?])
}

fn first_line(text: &str) -> &str {
    let mut lines = text.lines().map(str::trim);
    lines.find(|l| !l.is_empty()).unwrap_or("")
}

fn clip(text: &str, max: usize) -> String {
    match text.char_indices().nth(max) {
        Some((at, _)) => format!("{}…", &text[..at]),
        None => text.to_string(),
    }
}
