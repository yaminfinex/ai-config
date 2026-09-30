//! Scripted runs for screenshots and metrics, driven by `HERDER_NATIVE_SCRIPT` (space-separated steps).
//! Such a run opens its window without focus and behind everything else, quits when the script ends,
//! and exits non-zero when any step fails (settled decision 8 and the A0 review).
//!
//! Steps: `wait:<ms>` · `key:<keystroke>` (GPUI syntax such as `cmd-=`, through
//! `Window::dispatch_keystroke`, the real input path) · `shot:<name>` (draws a fresh frame, then
//! `render_to_image`; needs `--features shots`; written to `HERDER_NATIVE_SHOT_DIR`) · `rss` · `quit`.
//! `cpu:<ms>` (CPU over `ms`, with the pulse's paints and the shell's renders meanwhile). Units add
//! `type:`, `cpuscroll:` and `keycpu:` as they need them.
//!
//! `HERDER_NATIVE_WINDOW=<w>x<h>` sizes the window. `HERDER_NATIVE_VISIBLE=1` orders it in front
//! instead of behind, still without focus: a window behind others is never drawn, so measuring
//! what a visible window costs needs one that is.

use crate::views::PULSE_PAINTS;
use gpui_kit::{AsyncWindowContext, Keystroke, Pixels, Size, px, size};
use std::sync::OnceLock;
use std::sync::atomic::{AtomicU32, Ordering};
use std::time::{Duration, Instant};

static T0: OnceLock<Instant> = OnceLock::new();

/// Call once at the top of `main` so metrics are relative to process start.
pub fn start_clock() {
    T0.get_or_init(Instant::now);
}

/// Shell renders, for the `cpu:` step.
pub static RENDERS: AtomicU32 = AtomicU32::new(0);

/// The window's size: `HERDER_NATIVE_WINDOW`, else 1400 × 900.
pub fn window_size() -> Size<Pixels> {
    let var = std::env::var("HERDER_NATIVE_WINDOW").unwrap_or_default();
    let wh = var
        .split_once('x')
        .and_then(|(w, h)| Some((w.parse().ok()?, h.parse().ok()?)));
    let (w, h) = wh.unwrap_or((1400.0, 900.0));
    size(px(w), px(h))
}

/// A harness run whose window should be seen (in front, unfocused).
pub fn visible() -> bool {
    std::env::var("HERDER_NATIVE_VISIBLE").is_ok_and(|v| v == "1")
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
                let ms = arg.parse().unwrap_or(10_000u64);
                let count = || {
                    (
                        PULSE_PAINTS.load(Ordering::Relaxed),
                        RENDERS.load(Ordering::Relaxed),
                    )
                };
                let (before, (paints, renders)) = (cpu_s(), count());
                cx.background_executor()
                    .timer(Duration::from_millis(ms))
                    .await;
                let pct = (cpu_s() - before) / (ms as f64 / 1000.0) * 100.0;
                let (p, r) = (count().0 - paints, count().1 - renders);
                let seen = cx
                    .update(|_, _| crate::platform_mac::on_screen())
                    .unwrap_or(false);
                let on = if seen { "on screen" } else { "occluded" };
                metric(format!(
                    "cpu {pct:.2}% of one core over {ms} ms ({on}): {p} pulse paints, {r} shell renders"
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
    // since the state changed, so draw now, twice: a draw can queue a follow-up (revealing the selection).
    cx.update(|window, cx| window.draw(cx).clear(cx))
        .map_err(|e| e.to_string())?;
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
