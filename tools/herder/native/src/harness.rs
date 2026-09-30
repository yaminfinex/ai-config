//! Scripted runs for screenshots and metrics, driven by `HERDER_NATIVE_SCRIPT` (space-separated steps).
//! Such a run opens its window without focus and behind everything else, quits when the script ends,
//! and exits non-zero when any step fails (settled decision 8 and the A0 review).
//!
//! Steps: `wait:<ms>` · `key:<keystroke>` (GPUI syntax such as `cmd-=`, through
//! `Window::dispatch_keystroke`, the real input path) · `shot:<name>` (draws a fresh frame, then
//! `render_to_image`; needs `--features shots`; written to `HERDER_NATIVE_SHOT_DIR`) · `rss` · `quit`.
//! `cpu:<ms>` (CPU over `ms`, drawing a frame every 200 ms as a visible window does while a working dot
//! pulses; a window behind others is not drawn at all). Units add `type:`, `cpuscroll:`, `keycpu:`.

use gpui_kit::{AsyncWindowContext, Keystroke};
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

/// One `ps` field for this process, or `None`.
fn ps(field: &str) -> Option<String> {
    let pid = std::process::id().to_string();
    let out = std::process::Command::new("ps")
        .args(["-o", field, "-p", &pid])
        .output();
    Some(
        String::from_utf8_lossy(&out.ok()?.stdout)
            .trim()
            .to_string(),
    )
}

/// Resident set size of this process in MB, via `ps`.
pub fn rss_mb() -> f64 {
    ps("rss=")
        .and_then(|kb| kb.parse::<f64>().ok())
        .unwrap_or(0.0)
        / 1024.0
}

/// CPU time this process has used, in seconds (`ps` prints `m:ss.cc`).
fn cpu_s() -> f64 {
    let time = ps("time=").unwrap_or_default();
    let (m, s) = time.split_once(':').unwrap_or(("0", &time));
    m.parse::<f64>().unwrap_or(0.0) * 60.0 + s.parse::<f64>().unwrap_or(0.0)
}

pub async fn run(script: String, cx: &mut AsyncWindowContext) {
    let shot_dir = std::env::var("HERDER_NATIVE_SHOT_DIR").unwrap_or_else(|_| ".".into());
    let mut failed = false;
    let mut fail = |what: String| {
        eprintln!("harness: FAILED {what}");
        failed = true;
    };
    for step in script.split_whitespace() {
        let (op, arg) = step.split_once(':').unwrap_or((step, ""));
        match op {
            "wait" => {
                let ms = arg.parse().unwrap_or(500);
                cx.background_executor()
                    .timer(Duration::from_millis(ms))
                    .await;
            }
            "key" => match Keystroke::parse(arg) {
                Ok(keystroke) => {
                    let handled = cx.update(|window, cx| window.dispatch_keystroke(keystroke, cx));
                    match handled {
                        Ok(true) => metric(format!("key {arg}")),
                        _ => fail(format!("key {arg}: not handled by any binding")),
                    }
                }
                Err(e) => fail(format!("key {arg}: {e}")),
            },
            "rss" => metric(format!("rss {:.1} MB", rss_mb())),
            "cpu" => {
                let (ms, before) = (arg.parse().unwrap_or(10_000u64), cpu_s());
                for _ in 0..ms / 200 {
                    cx.background_executor()
                        .timer(Duration::from_millis(200))
                        .await;
                    let _ = cx.update(|window, cx| window.draw(cx).clear(cx));
                }
                let pct = (cpu_s() - before) / (ms as f64 / 1000.0) * 100.0;
                metric(format!(
                    "cpu {pct:.1}% of one core over {ms} ms, drawing at the pulse rate"
                ));
            }
            "shot" => {
                if let Err(e) = shot(&shot_dir, arg, cx) {
                    fail(format!("shot {arg}: {e}"));
                }
            }
            "quit" => break,
            _ => fail(format!("unknown step {step}")),
        }
    }
    if failed {
        metric("quit (failed)");
        std::process::exit(1);
    }
    metric("quit");
    let _ = cx.update(|_, cx| cx.quit());
}

#[cfg(feature = "shots")]
fn shot(dir: &str, name: &str, cx: &mut AsyncWindowContext) -> Result<(), String> {
    let path = format!("{dir}/{name}.png");
    // render_to_image reads the last drawn scene; a window ordered behind others may not have drawn
    // since the state changed, so draw now.
    let image = cx
        .update(|window, cx| {
            window.draw(cx).clear(cx);
            window.render_to_image()
        })
        .map_err(|e| e.to_string())?
        .map_err(|e| e.to_string())?;
    image.save(&path).map_err(|e| e.to_string())?;
    metric(format!("shot {path}"));
    Ok(())
}

#[cfg(not(feature = "shots"))]
fn shot(_dir: &str, name: &str, _cx: &mut AsyncWindowContext) -> Result<(), String> {
    Err(format!("shot:{name} needs a build with --features shots"))
}
