//! Scripted runs for screenshots and metrics, driven by `HERDER_NATIVE_SCRIPT` (space-separated steps).
//! Such a run opens its window without focus and behind everything else, quits when the script ends,
//! and exits non-zero when any step fails (settled decision 8 and the A0 review).
//!
//! Steps: `wait:<ms>` · `key:<keystroke>` (GPUI syntax such as `cmd-=`, through
//! `Window::dispatch_keystroke`, the real input path) · `shot:<name>` (draws a fresh frame, then
//! `render_to_image`; needs `--features shots`; written to `HERDER_NATIVE_SHOT_DIR`) · `rss` · `quit`.
//! `cpu:<ms>` (CPU over `ms`, with the pulse's paints and the shell's renders meanwhile) · `draw` (one
//! frame, as an occluded window gets none) · `link:<url>` (what clicking a transcript link dispatches) ·
//! `summon:<tag>` (what clicking a notification tagged so dispatches, without activating the app; U6) ·
//! `expect:<agent>` (the zoom shows it; a preview tab is `expect:<agent>+preview`) ·
//! `cpuscroll:<keystroke>x<n>` (`n` keystrokes, each followed by a timed `Window::draw`: the frame's CPU
//! cost, occluded or not) · `start:<ms>` (draws every 16 ms until the open transcript has paged back to
//! its start; fails after `ms`) · `box:<focused|idle>:<text>` (the composer's focus and text, `+` for a
//! space; U4) · `says:<text>` (the line under the composer contains it) · `has:<text>` (the composer's
//! `box:` contains it) · `notes:<n>:<closed|focused:text|idle:text>` (the zoomed agent's notes and the
//! notes editor; U5) · `header:<text>` (the lens header contains it; U6) · `select:<text>` (as if the pointer had selected it in the transcript) ·
//! `tap:<keystroke>` (as `key:`, but bound to nothing is fine) · `click:<capture|handoff|edit:i|delete:i>`
//! (what a click on the notes strip dispatches, `i` the zoomed agent's note, oldest first; it fails when
//! there is no such thing to click). Units add `type:` as they need it.
//!
//! `HERDER_NATIVE_WINDOW=<w>x<h>` sizes the window.

use crate::views::{POINTER_MOVES, PULSE_PAINTS};
use gpui_kit::{Action, App, AsyncWindowContext, Keystroke, Pixels, Size, Window, px, size};
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

/// The script, if this is a harness run. `HERDER_NATIVE_SCRIPT` set at all makes the run automation
/// (`platform_mac::quiet`), so a script that is empty or blank is refused before the app opens.
pub fn script() -> Result<Option<String>, String> {
    script_of(std::env::var_os("HERDER_NATIVE_SCRIPT").map(|v| v.to_string_lossy().into_owned()))
}

fn script_of(var: Option<String>) -> Result<Option<String>, String> {
    match var {
        Some(s) if s.trim().is_empty() => Err("HERDER_NATIVE_SCRIPT is set but empty".into()),
        var => Ok(var),
    }
}

#[cfg(test)]
#[test]
fn a_set_script_must_have_steps() {
    assert_eq!(script_of(None), Ok(None));
    assert!(script_of(Some(String::new())).is_err());
    assert!(script_of(Some(" \t ".into())).is_err());
    let steps = Some("wait:1 quit".to_string());
    assert_eq!(script_of(steps.clone()), Ok(steps));
}

/// One metric line on stderr, stamped with milliseconds since `start_clock`.
pub fn metric(msg: impl AsRef<str>) {
    let ms = T0.get().map_or(0.0, |t| t.elapsed().as_secs_f64() * 1e3);
    eprintln!("[metric +{ms:>8.1}ms] {}", msg.as_ref());
}

/// One `ps` field for this process, or `None`.
fn ps(field: &str) -> Option<String> {
    let pid = std::process::id().to_string();
    let out = std::process::Command::new("ps")
        .args(["-o", field, "-p", &pid])
        .output()
        .ok()?;
    Some(String::from_utf8_lossy(&out.stdout).trim().to_string())
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

/// What the harness asks the app; the shell answers from `views::probe`, so the harness knows no views.
pub trait Probe {
    /// What the app shows, for `expect`, `box`, `has`, `says`, `notes`, `header` and `start` (`None`: not yet).
    fn ask(&self, op: &str, window: &Window, cx: &App) -> Option<String>;
    /// The action a click dispatches, for `link`, `summon` and `click` (`None`: nothing to click).
    fn action(&self, op: &str, arg: &str, cx: &App) -> Option<Box<dyn Action>>;
    /// Stand in for a pointer selection in the transcript, for `select:`.
    fn select(&self, text: &str, cx: &mut App);
}

pub async fn run(script: String, probe: impl Probe, cx: &mut AsyncWindowContext) {
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
                let ms = Duration::from_millis(arg.parse().unwrap_or(500));
                cx.background_executor().timer(ms).await;
            }
            // `tap:` presses a key that may be bound to nothing (a guard checks what did not happen).
            "key" | "tap" => match Keystroke::parse(arg) {
                Ok(keystroke) => {
                    let handled = cx.update(|window, cx| window.dispatch_keystroke(keystroke, cx));
                    match (op, handled) {
                        ("tap", h) => metric(format!("tap {arg}: handled {}", h.unwrap_or(false))),
                        (_, Ok(true)) => metric(format!("key {arg}")),
                        _ => fail(format!("key {arg}: not handled by any binding")),
                    }
                }
                Err(e) => fail(format!("{op} {arg}: {e}")),
            },
            "select" => {
                let _ = cx.update(|_, cx| probe.select(&arg.replace('+', " "), cx));
                metric(format!("select {arg}"));
            }
            "rss" => metric(format!("rss {:.1} MB", rss_mb())),
            // `cpu:` idles, counting pointer events (real ones) meanwhile.
            "cpu" => {
                let ms = arg.parse().unwrap_or(10_000u64);
                let count = || [&PULSE_PAINTS, &RENDERS, &POINTER_MOVES].map(|c| c.load(Relaxed));
                let (before, start) = (cpu_s(), count());
                cx.background_executor()
                    .timer(Duration::from_millis(ms))
                    .await;
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
            // A window behind others is not drawn on its own; layout-driven work (paging) needs a frame.
            "draw" => drop(cx.update(|window, cx| window.draw(cx).clear(cx))),
            "link" | "summon" | "click" => {
                let ok = cx.update(|window, cx| {
                    let action = probe.action(op, arg, cx);
                    action.map(|a| window.dispatch_action(a, cx)).is_some()
                });
                match ok {
                    Ok(true) => metric(format!("{op} {arg}")),
                    _ => fail(format!("{op} {arg}: nothing to click")),
                }
            }
            "expect" | "box" | "has" | "says" | "notes" | "header" => {
                let got = cx.update(|window, cx| probe.ask(op, window, cx));
                let got = got.ok().flatten().unwrap_or_default();
                let want = arg.replace('+', " ");
                let part = matches!(op, "says" | "has" | "header") && got.contains(&want);
                match got == want || part {
                    true => metric(format!("{op} {arg}: ok")),
                    false => fail(format!("{op} {arg}: got `{got}`")),
                }
            }
            "start" => {
                let limit = Instant::now() + Duration::from_millis(arg.parse().unwrap_or(600_000));
                let tick = Duration::from_millis(16);
                let reached = loop {
                    let _ = cx.update(|window, cx| window.draw(cx).clear(cx));
                    let reached = cx.update(|window, cx| probe.ask(op, window, cx));
                    let reached = reached.ok().flatten();
                    if reached.is_some() || Instant::now() > limit {
                        break reached;
                    }
                    cx.background_executor().timer(tick).await;
                };
                match reached {
                    Some(reached) => metric(format!("transcript {reached}")),
                    None => fail(format!("start {arg}: not reached")),
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
                let i = |q: f64| ((ms.len() as f64 - 1.0) * q) as usize;
                let at = |q: f64| ms.get(i(q)).copied().unwrap_or(0.0);
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
