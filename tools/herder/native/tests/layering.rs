//! The one-way dependency rule from ARCHITECTURE.md, checked by reading the sources.
//! `store` and `api` never see GPUI; `store` never does I/O or reads a clock; `api` never reaches up.

use std::path::Path;

fn sources(dir: &str) -> Vec<(String, String)> {
    let root = Path::new(env!("CARGO_MANIFEST_DIR")).join("src").join(dir);
    let mut out = Vec::new();
    let mut stack = vec![root];
    while let Some(d) = stack.pop() {
        for entry in std::fs::read_dir(&d).into_iter().flatten().flatten() {
            let p = entry.path();
            if p.is_dir() {
                stack.push(p);
            } else if p.extension().is_some_and(|e| e == "rs") {
                out.push((
                    p.display().to_string(),
                    std::fs::read_to_string(&p).unwrap(),
                ));
            }
        }
    }
    assert!(!out.is_empty(), "no sources under src/{dir}");
    out
}

fn forbid(dir: &str, needles: &[&str]) {
    for (path, text) in sources(dir) {
        for (n, line) in text.lines().enumerate() {
            let code = line.trim_start();
            if code.starts_with("//") {
                continue;
            }
            for needle in needles {
                assert!(
                    !code.contains(needle),
                    "{path}:{}: `{needle}` is not allowed in {dir}",
                    n + 1
                );
            }
        }
    }
}

#[test]
fn store_is_pure() {
    forbid(
        "store",
        &[
            "gpui",
            "ureq",
            "std::fs",
            "std::thread",
            "Instant::now",
            "SystemTime",
            "crate::views",
            "crate::shell",
        ],
    );
}

#[test]
fn api_knows_nothing_above_it() {
    forbid(
        "api",
        &["gpui", "crate::store", "crate::views", "crate::shell"],
    );
}

#[test]
fn views_do_no_io() {
    forbid(
        "views",
        &[
            "ureq",
            "std::fs",
            "std::thread",
            "crate::api::client",
            "crate::api::sse",
        ],
    );
}
