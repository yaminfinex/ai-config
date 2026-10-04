//! Markdown helpers for the transcript: @agent mentions and file paths become links, and a path opens in
//! VS Code over Remote-SSH. Pure text in, text out; `transcript` renders the result with the kit's
//! markdown (tree-sitter highlighting in fenced blocks) and handles the link schemes below.
//!
//! Mentions follow web's `agentMentions.ts`: every board name, plus the 4-letter base name (`kona` for
//! `native-kona`) when it is unique and not itself a name, matched as a whole run of letters, digits,
//! `_`, `@` and `-` equal to `@?alias`, and never beside a path separator or a file extension.
//! Paths must look like paths (`path_like`); an inline code span that is one path-like token is linked
//! whole. Only prose text is linked (see `link`), never code, an existing link or a URL.

use markdown::mdast::Node;
use markdown::{ParseOptions, to_mdast};
use std::collections::HashMap;
use std::ops::Range;

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

/// `markdown` with mentions and paths turned into `herder-agent:` / `herder-path:` links, read off the
/// parse tree (the `markdown` crate the kit renders with), so only prose text and code spans are touched:
/// fenced and indented code, existing and reference links, definitions and HTML keep their source. A
/// mermaid fence becomes a link to the agent in herder web (`web`), which draws it.
pub fn link(markdown: &str, mentions: &Mentions, web: &str) -> String {
    let Ok(root) = to_mdast(markdown, &ParseOptions::gfm()) else {
        return markdown.to_string();
    };
    let mut edits = Vec::new();
    visit(&root, markdown, mentions, web, &mut edits);
    let (mut out, mut copied) = (String::with_capacity(markdown.len() + 64), 0);
    for (range, text) in edits {
        out.push_str(&markdown[copied..range.start]);
        out.push_str(&text);
        copied = range.end;
    }
    out.push_str(&markdown[copied..]);
    out
}

/// Collect `(source range, replacement)` in document order.
fn visit(node: &Node, src: &str, m: &Mentions, web: &str, edits: &mut Vec<(Range<usize>, String)>) {
    let Some(range) = node.position().map(|p| p.start.offset..p.end.offset) else {
        return;
    };
    let raw = &src[range.clone()];
    match node {
        Node::Text(_) => {
            let mut out = String::with_capacity(raw.len());
            words(raw, m, &mut out);
            if out != raw {
                edits.push((range, out));
            }
        }
        // A code span that is one path-like token links whole; a mention in code never does.
        Node::InlineCode(code) => {
            let token = code.value.trim();
            if !token.contains(char::is_whitespace) && path_like(token, true) {
                edits.push((range, format!("[{raw}](<{PATH}{token}>)")));
            }
        }
        Node::Code(code) if code.lang.as_deref() == Some("mermaid") => {
            edits.push((range, format!("[view diagram in web ↗](<{web}>)")));
        }
        Node::Code(_) | Node::Html(_) | Node::Link(_) | Node::LinkReference(_) => {}
        Node::Definition(_) | Node::Image(_) | Node::ImageReference(_) => {}
        _ => {
            for child in node.children().into_iter().flatten() {
                visit(child, src, m, web, edits);
            }
        }
    }
}

/// Where a clicked link goes: a herder link as it is, an authored relative or absolute path (no scheme)
/// as a path to resolve, a web URL to the browser (`None` here); anything else nowhere.
pub fn route(url: &str) -> Option<String> {
    if url.starts_with(AGENT) || url.starts_with(PATH) {
        return Some(url.to_string());
    }
    let scheme = url
        .split_once(':')
        .is_some_and(|(s, _)| !s.contains('/') && s.len() > 1);
    (!scheme && !url.starts_with('#') && !url.is_empty()).then(|| format!("{PATH}{url}"))
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

/// How VS Code opens `file` (relative) in the project `root` on `host`, at `line` (G3b): its command
/// line tool run twice, in order, as the owner found works (one call with both opens the file but not
/// the folder): `--folder-uri vscode-remote://ssh-remote+<host><root>` opens the root as a window's
/// folder, then `-r --remote ssh-remote+<host> -g <root>/<file>[:<line>]` goes to the file in that
/// window; without a file, the first alone. And the Remote-SSH URL for when the tool is missing (the
/// file alone, at line 1 at least so it does not open as a folder).
pub fn vscode(
    host: &str,
    root: &str,
    file: Option<&str>,
    line: Option<u32>,
) -> Option<(Vec<Vec<String>>, String)> {
    let root = root.trim_end_matches('/');
    let target = file.map(|f| format!("{root}/{f}"));
    let url = match &target {
        Some(target) => vscode_url(host, target, line.or(Some(1)))?,
        None => vscode_url(host, root, None)?,
    };
    let folder: Vec<String> = root.split('/').map(encode).collect();
    let folder = format!("vscode-remote://ssh-remote+{host}{}", folder.join("/"));
    let mut calls = vec![vec!["--folder-uri".into(), folder]];
    if let Some(target) = target {
        let at = line.map(|l| format!(":{l}")).unwrap_or_default();
        let (remote, go) = (format!("ssh-remote+{host}"), format!("{target}{at}"));
        calls.push(vec![
            "-r".into(),
            "--remote".into(),
            remote,
            "-g".into(),
            go,
        ]);
    }
    Some((calls, url))
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
