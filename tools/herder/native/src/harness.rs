//! Scripted runs for screenshots and metrics, driven by `HERDER_NATIVE_SCRIPT` (space-separated steps).
//! Such a run opens its window without focus and behind everything else, and quits when told
//! (settled decision 8). Steps in A0: `wait:<ms>` · `shot:<name>` (needs `--features shots`; written to
//! `HERDER_NATIVE_SHOT_DIR`) · `rss` · `quit`. Units add `key:`, `type:`, `cpuscroll:` and `keycpu:`
//! as they need them; keys go through `Window::dispatch_keystroke`, the same path as real input.

use gpui_kit::AsyncWindowContext;
use std::sync::OnceLock;
use std::time::{Duration, Instant};

static T0: OnceLock<Instant> = OnceLock::new();

/// Call once at the top of `main` so metrics are relative to process start.
pub fn start_clock() {
    T0.get_or_init(Instant::now);
}

/// The script, if this is a harness run.
pub fn script() -> Option<String> {
    std::env::var("HERDER_NATIVE_SCRIPT")
        .ok()
        .filter(|s| !s.trim().is_empty())
}

/// One metric line on stderr, stamped with milliseconds since `start_clock`.
pub fn metric(msg: impl AsRef<str>) {
    let ms = T0
        .get()
        .map(|t| t.elapsed().as_secs_f64() * 1e3)
        .unwrap_or(0.0);
    eprintln!("[metric +{ms:>8.1}ms] {}", msg.as_ref());
}

/// Resident set size of this process in MB, via `ps`.
pub fn rss_mb() -> f64 {
    let out = std::process::Command::new("ps")
        .args(["-o", "rss=", "-p", &std::process::id().to_string()])
        .output();
    out.ok()
        .and_then(|o| {
            String::from_utf8_lossy(&o.stdout)
                .trim()
                .parse::<f64>()
                .ok()
        })
        .unwrap_or(0.0)
        / 1024.0
}

pub async fn run(script: String, cx: &mut AsyncWindowContext) {
    let shot_dir = std::env::var("HERDER_NATIVE_SHOT_DIR").unwrap_or_else(|_| ".".into());
    for step in script.split_whitespace() {
        let (op, arg) = step.split_once(':').unwrap_or((step, ""));
        match op {
            "wait" => {
                let ms = arg.parse().unwrap_or(500);
                cx.background_executor()
                    .timer(Duration::from_millis(ms))
                    .await;
            }
            "rss" => metric(format!("rss {:.1} MB", rss_mb())),
            "shot" => shot(&shot_dir, arg, cx),
            "quit" => {
                metric("quit");
                let _ = cx.update(|_, cx| cx.quit());
            }
            _ => eprintln!("harness: unknown step {step}"),
        }
    }
}

#[cfg(feature = "shots")]
fn shot(dir: &str, name: &str, cx: &mut AsyncWindowContext) {
    let path = format!("{dir}/{name}.png");
    match cx.update(|window, _| window.render_to_image()) {
        Ok(Ok(img)) => match img.save(&path) {
            Ok(()) => metric(format!("shot {path}")),
            Err(e) => eprintln!("harness: shot {path}: {e}"),
        },
        e => eprintln!("harness: shot failed: {:?}", e.err()),
    }
}

#[cfg(not(feature = "shots"))]
fn shot(_dir: &str, name: &str, _cx: &mut AsyncWindowContext) {
    eprintln!("harness: shot:{name} skipped (build with --features shots)");
}
