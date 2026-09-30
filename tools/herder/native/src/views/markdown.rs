//! Markdown helpers for the transcript: @agent mentions and file paths become links, and a path opens in
//! VS Code over Remote-SSH. Pure text in, text out; `transcript` renders the result with the kit's
//! markdown (tree-sitter highlighting in fenced blocks) and handles the link schemes below.
//!
//! Mentions follow web's `agentMentions.ts`: every board name, plus the 4-letter base name (`kona` for
//! `native-kona`) when it is unique and not itself a name, matched as a whole run of letters, digits,
//! `_`, `@` and `-` equal to `@?alias`, and never beside a path separator or a file extension.
//! Paths must look like paths (`path_like`); an inline code span that is one path-like token is linked
//! whole. Nothing inside a fenced
//! block, an existing link or a URL is linked, and a mention inside code is not either.

use std::collections::HashMap;

pub const AGENT: &str = "herder-agent:";
pub const PATH: &str = "herder-path:";

/// Mention aliases → the board name they open.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct Mentions(HashMap<String, String>);

impl Mentions {
    pub fn new<'a>(names: impl IntoIterator<Item = &'a str>) -> Self {
        let names: Vec<&str> = names.into_iter().collect();
        let mut aliases: HashMap<String, String> = names
            .iter()
            .map(|n| (n.to_string(), n.to_string()))
            .collect();
        let mut bases: HashMap<&str, Vec<&str>> = HashMap::new();
        for name in &names {
            let base = name.rsplit('-').next().unwrap_or(name);
            if base.len() == 4 && base.bytes().all(|b| b.is_ascii_lowercase()) {
                bases.entry(base).or_default().push(name);
            }
        }
        for (base, owners) in bases {
            if let [only] = owners[..] {
                aliases.entry(base.to_string()).or_insert(only.to_string());
            }
        }
        Mentions(aliases)
    }

    fn target(&self, token: &str) -> Option<&str> {
        let alias = token.strip_prefix('@').unwrap_or(token);
        self.0.get(alias).map(String::as_str)
    }
}

/// `markdown` with mentions and paths turned into `herder-agent:` / `herder-path:` links.
pub fn link(markdown: &str, mentions: &Mentions) -> String {
    let mut out = String::with_capacity(markdown.len() + 64);
    let mut fence: Option<&str> = None;
    for line in markdown.split_inclusive('\n') {
        let trimmed = line.trim_start();
        let marker = ["```", "~~~"].into_iter().find(|m| trimmed.starts_with(m));
        let indent = line.len() - trimmed.len();
        match (fence, marker) {
            (Some(open), Some(m)) if m == open && indent < 4 => fence = None,
            (None, Some(m)) if indent < 4 => fence = Some(m),
            (None, _) if !indented_code(line) => {
                inline(line, mentions, &mut out);
                continue;
            }
            _ => {}
        }
        out.push_str(line);
    }
    out
}

/// Four spaces or a tab of indent, not continuing a list item.
fn indented_code(line: &str) -> bool {
    let trimmed = line.trim_start();
    let indent = &line[..line.len() - trimmed.len()];
    let list = trimmed.starts_with(['-', '*', '+'])
        || trimmed
            .split_once(". ")
            .is_some_and(|(n, _)| n.parse::<u32>().is_ok());
    (indent.contains('\t') || indent.len() >= 4) && !list
}

/// One prose line: code spans, links and URLs kept as they are; the words between linked.
fn inline(line: &str, mentions: &Mentions, out: &mut String) {
    let mut rest = line;
    while !rest.is_empty() {
        let at = rest.find(['`', '[', '<']).unwrap_or(rest.len());
        words(&rest[..at], mentions, out);
        rest = &rest[at..];
        let Some(first) = rest.chars().next() else {
            break;
        };
        let end = match first {
            '`' => code_span(rest, out),
            '[' => rest
                .find("](")
                .and_then(|m| rest[m..].find(')').map(|c| m + c + 1)),
            _ => rest.find('>').map(|e| e + 1),
        };
        let end = end.unwrap_or(1);
        if first != '`' || end == 1 {
            out.push_str(&rest[..end]);
        }
        rest = &rest[end..];
    }
}

/// A code span at the start of `text`: pushed (linked when it is one path-like token) and its length
/// returned; `None` when the backticks never close.
fn code_span(text: &str, out: &mut String) -> Option<usize> {
    let ticks = text.len() - text.trim_start_matches('`').len();
    let fence = &text[..ticks];
    let close = text[ticks..].find(fence)? + ticks;
    let end = close + ticks;
    let body = &text[ticks..close];
    let token = body.trim();
    let one = !token.is_empty() && !token.contains(char::is_whitespace) && !token.contains('`');
    if one && path_like(token, true) {
        out.push_str(&format!("[{}](<{PATH}{token}>)", &text[..end]));
    } else {
        out.push_str(&text[..end]);
    }
    Some(end)
}

const DELIMITERS: &[char] = &[
    '(', ')', '[', ']', '{', '}', '<', '>', '"', '\'', '|', '*', ',',
];

/// Plain words: a path-like word becomes a path link, otherwise its mention runs become agent links.
fn words(text: &str, mentions: &Mentions, out: &mut String) {
    let mut last = 0;
    let bounds = text.match_indices(|c: char| c.is_whitespace() || DELIMITERS.contains(&c));
    let mut spans = Vec::new();
    for (at, sep) in bounds {
        spans.push((last, at));
        last = at + sep.len();
    }
    spans.push((last, text.len()));
    let mut copied = 0;
    for (start, end) in spans {
        let word = &text[start..end];
        let path = word.trim_end_matches(['.', ',', ';', ':', '!', '?']);
        if path_like(path, false) {
            out.push_str(&text[copied..start]);
            out.push_str(&format!("[{path}](<{PATH}{path}>)"));
            copied = start + path.len();
        } else if !word.contains("://") && !word.starts_with("www.") {
            copied = mention_runs(text, start, end, copied, mentions, out);
        }
    }
    out.push_str(&text[copied..]);
}

/// Link the mention runs in `text[start..end]`; returns how far `text` has been copied to `out`.
fn mention_runs(
    text: &str,
    start: usize,
    end: usize,
    mut copied: usize,
    m: &Mentions,
    out: &mut String,
) -> usize {
    let run = |c: char| c.is_alphanumeric() || matches!(c, '_' | '@' | '-');
    let mut i = start;
    while i < end {
        let Some(c) = text[i..end].chars().next() else {
            break;
        };
        if !run(c) {
            i += c.len_utf8();
            continue;
        }
        let len = text[i..end].find(|c: char| !run(c)).unwrap_or(end - i);
        let token = &text[i..i + len];
        let before = text[..i].chars().next_back();
        let after = &text[i + len..];
        let beside_path = matches!(before, Some('/' | '\\'))
            || after.starts_with(['/', '\\'])
            || after
                .strip_prefix('.')
                .is_some_and(|a| a.starts_with(char::is_alphanumeric));
        if let Some(target) = m.target(token).filter(|_| !beside_path) {
            out.push_str(&text[copied..i]);
            out.push_str(&format!("[{token}]({AGENT}{target})"));
            copied = i + len;
        }
        i += len;
    }
    copied
}

/// File extensions a bare `name.ext` must end in to count as a path (`self.items` does not).
const EXTENSIONS: &[&str] = &[
    "rs", "md", "mdx", "ts", "tsx", "js", "jsx", "mjs", "cjs", "json", "jsonl", "toml", "yaml",
    "yml", "py", "go", "sh", "bash", "zsh", "txt", "html", "css", "scss", "lock", "sql", "swift",
    "kt", "java", "c", "h", "cc", "cpp", "hpp", "rb", "proto", "log", "csv", "env", "cfg", "ini",
    "conf", "xml", "svg", "png", "jpg", "pdf", "vue", "zig", "nix", "plist", "patch", "diff",
    "ipynb",
];

/// A word that looks like a path. In prose, where a false link costs more: absolute with two
/// segments, home or relative, three segments, or a name with a known extension. A code span
/// (`loose`) also counts any separator. Only path characters, never a URL.
pub fn path_like(word: &str, loose: bool) -> bool {
    let (path, _) = crate::store::transcript::split_line(word);
    let chars = |c: char| c.is_alphanumeric() || matches!(c, '.' | '_' | '-' | '/' | '~' | '+');
    if path.len() < 3 || !path.chars().all(chars) || path.contains("//") {
        return false;
    }
    let segments = path.split('/').filter(|s| !s.is_empty()).count();
    let anchored = ["~/", "./", "../"].iter().any(|p| path.starts_with(p))
        || (path.starts_with('/') && segments >= 2);
    let last = path.rsplit('/').next().unwrap_or(&path);
    let ext = last
        .rsplit_once('.')
        .is_some_and(|(stem, ext)| !stem.is_empty() && EXTENSIONS.contains(&ext));
    anchored || segments >= 3 || ext || (loose && path.contains('/'))
}

/// VS Code's Remote-SSH URL for `path` on `host`, at `line` when given (web's `vscodeModel`).
pub fn vscode_url(host: &str, path: &str, line: Option<u32>) -> Option<String> {
    let bad = |c: char| c.is_whitespace() || c.is_control() || "/+?#%".contains(c);
    if host.is_empty() || host.contains(bad) || !path.starts_with('/') {
        return None;
    }
    let encoded: Vec<String> = path.split('/').map(encode).collect();
    let line = line.map(|l| format!(":{l}")).unwrap_or_default();
    Some(format!(
        "vscode://vscode-remote/ssh-remote+{host}{}{line}",
        encoded.join("/")
    ))
}

/// Percent-encoding of one path segment: unreserved characters kept, every other UTF-8 byte `%XX`.
fn encode(segment: &str) -> String {
    let mut out = String::with_capacity(segment.len());
    for b in segment.bytes() {
        match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'.' | b'_' | b'~' => {
                out.push(b as char)
            }
            _ => out.push_str(&format!("%{b:02X}")),
        }
    }
    out
}
