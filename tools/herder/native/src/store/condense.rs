//! The compact projection: one transcript entry as web's clean view shows it, and the rows those items
//! group into. Pure and stateless; the paging, keys and tool pairing that put these items in order are
//! `transcript`'s. This changes with web's cleanView, cleanRows and fencingModel, `transcript` with the
//! entries API.

use super::transcript::{Head, Item, Key, Tone, ToolResult};
use crate::api::{Entry, Kind, Payload};
use serde_json::Value;
use std::collections::BTreeMap;

/// Characters kept per tool summary line.
const LINE: usize = 200;

/// A `tool_use`: its name, web's one-line summary of its input, and the input as web's detail shows it
/// (`JSON.stringify(input, null, 2)`).
pub(super) fn tool_call(p: &Payload) -> (String, String, String) {
    let name = p.name.as_str().unwrap_or("").to_string();
    let summary = clip(&tool_summary(&name, &p.input), LINE);
    let input = match &p.input {
        Value::Object(_) => serde_json::to_string_pretty(&p.input).unwrap_or_default(),
        _ => "{}".into(),
    };
    (name, summary, input)
}

/// A `tool_result`: whether it failed, its text (the serve caps it at 16 KiB) and when it came.
pub(super) fn tool_result(p: &Payload, timestamp: &str) -> ToolResult {
    let text = text_of(&p.content);
    let error = p.is_error.as_bool().unwrap_or(false);
    let at = epoch_ms(timestamp);
    ToolResult { error, text, at }
}

/// The items one entry yields in compact mode, as web's clean view (tool pairs are `ingest`'s).
pub fn condense(entry: &Entry) -> Vec<Item> {
    let p = &entry.payload;
    let text = text_of(&p.message["content"]);
    let item = match entry.kind {
        Kind::HumanPrompt => Item::Prompt(text),
        // Acks and the launcher are bus traffic web's compact view hides.
        Kind::HcomDelivery => {
            let deliveries = p.deliveries.as_array().into_iter().flatten();
            let quiet = |d: &&Value| {
                str_at(d, "sender") == "[hcom-launcher]" || str_at(d, "intent") == "ack"
            };
            return deliveries.filter(|d| !quiet(d)).map(delivery).collect();
        }
        Kind::TaskNotification => {
            let summary = between(&text, "<summary>", "</summary>").unwrap_or(first_line(&text));
            chip(Tone::Other, "task", summary.trim())
        }
        // A slash command; its output (the next entry) joins it in web, so shows nothing here.
        Kind::CommandStdout => match between(&text, "<command-name>", "</command-name>") {
            Some(name) => {
                let args = between(&text, "<command-args>", "</command-args>").unwrap_or("");
                chip(Tone::Tool, name, &clip(args.trim(), 80))
            }
            None => return Vec::new(),
        },
        // With its metadata, web's divider; without, the summary folded.
        Kind::CompactDivider => match p.compact_metadata.as_object().filter(|m| !m.is_empty()) {
            Some(m) => {
                let n = |key: &str| m.get(key).and_then(Value::as_u64).map_or("?".into(), group);
                let trigger = m.get("trigger").and_then(Value::as_str);
                let trigger = trigger.filter(|t| !t.is_empty()).unwrap_or("unknown");
                let (pre, post) = (n("preTokens"), n("postTokens"));
                Item::CompactDivider(format!(
                    "context compacted ({trigger}, {pre} → {post} tokens)"
                ))
            }
            None if text.is_empty() => Item::CompactSummary(text_of(&p.content)),
            None => Item::CompactSummary(text),
        },
        Kind::AssistantText if p.is_api_error_message.as_bool() == Some(true) => Item::Error(text),
        // Unfenced or malformed, the answer is literal, tags and all (web's fail-open). One of only
        // statuses and internal notes is a run's member (`marker`).
        Kind::AssistantText => match fence(&text) {
            Some(segs) => Item::Assistant(segs),
            None => Item::Assistant(vec![Seg::Text(text.trim().into())]),
        },
        Kind::Thinking => return vec![Item::Thinking(text)],
        Kind::SystemChip => Item::SystemChip(system_chip(p)),
        Kind::Unknown => chip(Tone::Other, "unknown", &clip(first_line(&text), 80)),
        // Carriers, telemetry, injected context and tool pairs.
        Kind::HcomDeliveryStub | Kind::InjectedSystem | Kind::TurnDuration => return Vec::new(),
        Kind::ToolUse | Kind::ToolResult => return Vec::new(),
    };
    let empty = match &item {
        Item::Prompt(s) | Item::SystemChip(s) => s.trim().is_empty(),
        Item::Assistant(segs) => segs == &[Seg::Text(String::new())],
        _ => false,
    };
    if empty { Vec::new() } else { vec![item] }
}

fn chip(tone: Tone, label: &str, text: &str) -> Item {
    let (label, text) = (label.to_string(), text.to_string());
    Item::Chip { tone, label, text }
}

fn delivery(d: &Value) -> Item {
    let (sender, raw) = (str_at(d, "sender").to_string(), str_at(d, "text"));
    let body = strip_operator(raw);
    let operator = body.is_some();
    let text = body.unwrap_or(raw).trim_end().trim_end_matches(" |");
    let text = text.trim().to_string();
    let head = Head {
        to: str_at(d, "recipient").into(),
        intent: str_at(d, "intent").into(),
        thread: str_at(d, "thread").into(),
        id: str_at(d, "message_id").into(),
    };
    Item::Delivery {
        sender,
        text,
        operator,
        head: Box::new(head),
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

/// Web's compact view shows only these system entries, the model switches (an empty label hides the rest).
fn system_chip(p: &Payload) -> String {
    let to = p.fallback_model.as_str().map(|m| format!(" to {m}"));
    let to = to.unwrap_or_default();
    match p.subtype.as_str().unwrap_or("") {
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

/// A fenced answer's parts (web's `fencingModel`).
#[derive(Clone, Debug, PartialEq)]
pub enum Seg {
    Text(String),
    Status(String),
    Internal(String),
}

/// An answer split at its `<status>` and `<internal>` fences; `None` when it is literal, as web reads
/// it: no fence, or a closing tag with nothing open, a mismatched or nested tag, an unclosed one, or a
/// status body over more than one line.
pub fn fence(text: &str) -> Option<Vec<Seg>> {
    const TAGS: [&str; 4] = ["<internal>", "</internal>", "<status>", "</status>"];
    let at = |i: usize| TAGS.into_iter().find(|t| text[i..].starts_with(t));
    let tags = text
        .match_indices('<')
        .filter_map(|(i, _)| Some((i, at(i)?)));
    let (mut segs, mut cursor, mut open) = (Vec::new(), 0, None);
    for (i, tag) in tags {
        let internal = tag.contains("internal");
        match open {
            None if tag.starts_with("</") => return None,
            None => {
                if i > cursor {
                    segs.push(Seg::Text(text[cursor..i].to_string()));
                }
                open = Some((internal, i + tag.len()));
            }
            Some((kind, from)) if kind == internal && tag.starts_with("</") => {
                let body = text[from..i].to_string();
                if !internal && body.contains(['\r', '\n']) {
                    return None;
                }
                segs.push(if internal {
                    Seg::Internal(body)
                } else {
                    Seg::Status(body)
                });
                (cursor, open) = (i + tag.len(), None);
            }
            Some(_) => return None,
        }
    }
    if open.is_some() || segs.is_empty() && cursor == 0 {
        return None;
    }
    if cursor < text.len() {
        segs.push(Seg::Text(text[cursor..].to_string()));
    }
    Some(segs)
}

/// A fenced answer with no visible text is a run's member, and its pill, as web's: its statuses (and
/// `internal note`) joined, toned as a status, or as thinking when it holds only internal notes.
pub fn marker(segs: &[Seg]) -> Option<(Tone, String)> {
    let mut statuses = Vec::new();
    let mut internal = false;
    for seg in segs {
        match seg {
            Seg::Text(t) if t.trim().is_empty() => continue,
            Seg::Text(_) => return None,
            Seg::Status(s) => statuses.push(if s.trim().is_empty() { "status" } else { s }),
            Seg::Internal(_) => internal = true,
        }
    }
    let tone = if statuses.is_empty() {
        Tone::Thinking
    } else {
        Tone::Status
    };
    statuses.extend(internal.then_some("internal note"));
    Some((tone, statuses.join(" · ")))
}

/// One row of the compact list: a standalone item, or a run of consecutive activity items (tools,
/// thinking, chips, agents' messages) from its first key to its last.
#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Row {
    One(Key),
    Run(Key, Key),
}

impl Row {
    pub fn first(&self) -> Key {
        match *self {
            Row::One(k) | Row::Run(k, _) => k,
        }
    }

    pub fn last(&self) -> Key {
        match *self {
            Row::One(k) | Row::Run(_, k) => k,
        }
    }
}

/// The rows `items` group into, as web's `cleanRows`: a run is a maximal stretch of activity, so its
/// members are `items.range(first..=last)`. A single tool between two answers is still a run.
pub fn rows(items: &BTreeMap<Key, Item>) -> Vec<Row> {
    let (mut rows, mut run) = (Vec::new(), None);
    for (&key, item) in items {
        if item.activity() {
            run = Some(run.map_or((key, key), |(first, _)| (first, key)));
            continue;
        }
        rows.extend(run.take().map(|(first, last)| Row::Run(first, last)));
        rows.push(Row::One(key));
    }
    rows.extend(run.map(|(first, last)| Row::Run(first, last)));
    rows
}

/// One pill of a run's strip: `count` adjacent members merged (`Bash ×4`) from the `at`th; `error`
/// when a merged tool failed.
#[derive(Clone, Debug, PartialEq)]
pub struct Pill {
    pub tone: Tone,
    pub label: String,
    pub count: usize,
    pub error: bool,
    pub at: usize,
}

/// A run's pills, as web's `aggregateActivityPills`: adjacent tools of one name merge, and adjacent
/// equal statuses or internal notes; thinking and messages never do.
pub fn pills<'a>(members: impl IntoIterator<Item = &'a Item>) -> Vec<Pill> {
    let mut pills: Vec<Pill> = Vec::new();
    let mut prev: Option<&Item> = None;
    for (at, item) in members.into_iter().enumerate() {
        let (tone, label, error) = match item {
            Item::Tool { name, result, .. } => {
                let error = result.as_ref().is_some_and(|r| r.error);
                (Tone::Tool, name.clone(), error)
            }
            Item::Thinking(_) => (Tone::Thinking, "thinking".into(), false),
            Item::Chip { tone, label, .. } => (*tone, label.clone(), false),
            Item::Assistant(segs) => match marker(segs) {
                Some((tone, label)) => (tone, label, false),
                None => continue,
            },
            Item::Delivery { sender, .. } => (Tone::Message, format!("✉ {sender}"), false),
            _ => continue,
        };
        let merges = match (prev, item) {
            (Some(Item::Tool { name: a, .. }), Item::Tool { name: b, .. }) => a == b,
            (Some(a), Item::Assistant(_)) => a == item,
            _ => false,
        };
        match pills.last_mut().filter(|_| merges) {
            Some(pill) => (pill.count, pill.error) = (pill.count + 1, pill.error || error),
            None => pills.push(Pill {
                tone,
                label,
                count: 1,
                error,
                at,
            }),
        }
        prev = Some(item);
    }
    pills
}

/// Epoch seconds of an RFC 3339 timestamp (`epoch_ms`, floored).
pub fn epoch(ts: &str) -> Option<u64> {
    epoch_ms(ts).map(|ms| ms / 1000)
}

/// Epoch milliseconds of an RFC 3339 timestamp: the serve's entries' (`2026-09-30T00:07:16.868Z`) and
/// hcom's queued messages' (`2026-09-30T00:07:16.868123+00:00`), the fraction to the millisecond;
/// `None` for any other form. The store reads no clock: the view says how long ago.
pub fn epoch_ms(ts: &str) -> Option<u64> {
    let n = |from: usize, to: usize| ts.get(from..to)?.parse::<i64>().ok();
    if ts.get(10..11) != Some("T") {
        return None;
    }
    let zone = ts.get(19..)?;
    let fraction = zone.strip_prefix('.').unwrap_or("");
    let digits = fraction.len()
        - fraction
            .trim_start_matches(|c: char| c.is_ascii_digit())
            .len();
    let ms = format!("{:0<3}", &fraction[..digits.min(3)])
        .parse::<i64>()
        .ok()?;
    let zone = zone.strip_prefix('.').map_or(zone, |f| &f[digits..]);
    let offset = match zone.as_bytes() {
        b"Z" => 0,
        [sign @ (b'+' | b'-'), _, _, b':', _, _] => {
            let at = |from: usize| zone.get(from..from + 2)?.parse::<i64>().ok();
            let offset = at(1)? * 3600 + at(4)? * 60;
            if *sign == b'+' { offset } else { -offset }
        }
        _ => return None,
    };
    let (y, m, d) = (n(0, 4)?, n(5, 7)?, n(8, 10)?);
    let secs = n(11, 13)? * 3600 + n(14, 16)? * 60 + n(17, 19)? - offset;
    // Days from 1970-01-01 to the civil date (Hinnant's algorithm), years starting in March.
    let y = if m <= 2 { y - 1 } else { y };
    let (era, yoe) = (y.div_euclid(400), y.rem_euclid(400));
    let doy = (153 * ((m + 9) % 12) + 2) / 5 + d - 1;
    let days = era * 146_097 + yoe * 365 + yoe / 4 - yoe / 100 + doy - 719_468;
    u64::try_from((days * 86_400 + secs) * 1000 + ms).ok()
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

/// A count as web's `toLocaleString` writes it in English: `167,432`.
pub fn group(n: u64) -> String {
    let digits = n.to_string();
    let mut out = String::new();
    for (i, c) in digits.chars().enumerate() {
        if i > 0 && (digits.len() - i).is_multiple_of(3) {
            out.push(',');
        }
        out.push(c);
    }
    out
}

fn clip(text: &str, max: usize) -> String {
    match text.char_indices().nth(max) {
        Some((at, _)) => format!("{}…", &text[..at]),
        None => text.to_string(),
    }
}
