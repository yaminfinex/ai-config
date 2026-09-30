//! Scripted runs for screenshots and metrics, driven by `HERDER_NATIVE_SCRIPT` (space-separated steps).
//! Such a run opens its window without focus and behind everything else, quits when the script ends,
//! and exits non-zero when any step fails (settled decision 8 and the A0 review).
//!
//! Steps: `wait:<ms>` · `key:<keystroke>` (GPUI syntax such as `cmd-=`, through
//! `Window::dispatch_keystroke`, the real input path) · `shot:<name>` (draws a fresh frame, then
//! `render_to_image`; needs `--features shots`; written to `HERDER_NATIVE_SHOT_DIR`) · `rss` · `quit`.
//! `cpu:<ms>` (CPU over `ms`, with the pulse's paints and the shell's renders meanwhile) ·
//! `link:<url>` (what clicking a transcript link dispatches) · `expect:<agent>` (the zoom shows it; a
//! preview tab is `expect:<agent>+preview`) · `cpuscroll:<keystroke>x<n>` (`n` keystrokes, each followed
//! by a timed `Window::draw`: the frame's CPU cost, occluded or not). Units add `type:` as they need it.
//!
//! `HERDER_NATIVE_WINDOW=<w>x<h>` sizes the window. `HERDER_NATIVE_VISIBLE=1` orders it in front
//! instead of behind, still without focus: a window behind others is never drawn, so measuring
//! what a visible window costs needs one that is.

use crate::views::{POINTER_MOVES, PULSE_PAINTS};
use gpui_kit::{
    AsyncWindowContext, Keystroke, Modifiers, MouseMoveEvent, Pixels, PlatformInput, Size, point,
    px, size,
};
use std::sync::OnceLock;
use std::sync::atomic::AtomicU32;
use std::sync::atomic::Ordering::Relaxed;
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
            // `cpu:` idles; `move:` sweeps a synthetic pointer across the window every 16 ms (the
            // owner's real pointer stays put). Both count pointer events, real ones included.
            "cpu" | "move" => {
                let ms = arg.parse().unwrap_or(10_000u64);
                let count = || [&PULSE_PAINTS, &RENDERS, &POINTER_MOVES].map(|c| c.load(Relaxed));
                let (before, start) = (cpu_s(), count());
                let (moves, tick) = (if op == "move" { ms / 16 } else { 0 }, 16);
                for i in 0..moves {
                    let position = point(px(40. + (i % 90) as f32 * 10.), px(120.));
                    let (pressed_button, modifiers) = (None, Modifiers::default());
                    let event = PlatformInput::MouseMove(MouseMoveEvent {
                        position,
                        pressed_button,
                        modifiers,
                    });
                    let _ = cx.update(|w, cx| w.dispatch_event(event, cx));
                    cx.background_executor()
                        .timer(Duration::from_millis(tick))
                        .await;
                }
                let rest = Duration::from_millis(ms - moves * tick);
                cx.background_executor().timer(rest).await;
                let pct = (cpu_s() - before) / (ms as f64 / 1000.0) * 100.0;
                let now = count();
                let [p, r, m] = [0, 1, 2].map(|i| now[i] - start[i]);
                let seen = cx.update(|_, _| crate::platform_mac::on_screen());
                let on = if seen.unwrap_or(false) {
                    "on screen"
                } else {
                    "occluded"
                };
                metric(format!(
                    "cpu {pct:.2}% of one core over {ms} ms ({on}): {m} pointer moves, {p} pulse paints, {r} shell renders"
                ));
            }
            "link" => {
                let link = crate::views::transcript::OpenLink(arg.to_string().into());
                let _ = cx.update(|window, cx| window.dispatch_action(Box::new(link), cx));
                metric(format!("link {arg}"));
            }
            "expect" => {
                let shown = crate::views::transcript::SHOWN.lock().unwrap().clone();
                match shown == arg.replace('+', " ") {
                    true => metric(format!("expect {arg}: ok")),
                    false => fail(format!("expect {arg}: the zoom shows `{shown}`")),
                }
            }
            "cpuscroll" => {
                let (key, n) = arg.split_once('x').unwrap_or((arg, "60"));
                let (key, n) = (Keystroke::parse(key).ok(), n.parse().unwrap_or(60usize));
                let mut ms = Vec::with_capacity(n);
                for _ in 0..n {
                    let Some(key) = key.clone() else { break };
                    let drawn = cx.update(|window, cx| {
                        window.dispatch_keystroke(key, cx);
                        let t = Instant::now();
                        window.draw(cx).clear(cx);
                        t.elapsed().as_secs_f64() * 1e3
                    });
                    ms.extend(drawn.ok());
                }
                ms.sort_by(f64::total_cmp);
                let at = |q: f64| {
                    ms.get(((ms.len() as f64 - 1.0) * q) as usize)
                        .copied()
                        .unwrap_or(0.0)
                };
                let (p50, p95, max) = (at(0.5), at(0.95), at(1.0));
                metric(format!(
                    "cpuscroll {arg}: draw p50 {p50:.2} ms, p95 {p95:.2} ms, max {max:.2} ms"
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
