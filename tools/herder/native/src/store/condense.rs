//! The compact projection: one transcript entry as web's clean view shows it. Pure and stateless; the
//! paging, keys and tool pairing that put these items in order are `transcript`'s. This changes with web's
//! cleanView, `transcript` with the entries API.

use super::transcript::{Item, ToolResult};
use crate::api::{Entry, Kind};
use serde_json::Value;

/// Characters kept per tool line and result, and per thinking pill.
const LINE: usize = 200;
const THINKING: usize = 2000;

/// A `tool_use`'s name and web's one-line summary of its input.
pub(super) fn tool_call(p: &Value) -> (String, String) {
    let name = str_at(p, "name").to_string();
    let summary = clip(&tool_summary(&name, &p["input"]), LINE);
    (name, summary)
}

/// A `tool_result`: whether it failed, and its first line.
pub(super) fn tool_result(p: &Value) -> ToolResult {
    let text = clip(first_line(&text_of(&p["content"])), LINE);
    let error = p["is_error"].as_bool().unwrap_or(false);
    ToolResult { error, text }
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
    let body = strip_operator(raw);
    let operator = body.is_some();
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

pub(super) fn str_at<'a>(v: &'a Value, key: &str) -> &'a str {
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
