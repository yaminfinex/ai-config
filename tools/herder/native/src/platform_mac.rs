//! The AppKit calls GPUI doesn't wrap, through objc2 (settled decision 2), and the summon chord through
//! `global-hotkey` (U6). Everything here runs on the main thread. Notifications use GPUI's own
//! `show_system_notification`, which the shell calls only when `quiet()` is false.
//!
//! Test mode is decided here, once: a scripted run (`HERDER_NATIVE_SCRIPT`: the harness and every
//! `just check-*`) is `quiet`. It posts no notification, sets no badge and never takes the owner's
//! chord; each is a logged no-op (`platform: would notify …`, `platform: badge 3`) the scenarios read.

use global_hotkey::hotkey::HotKey;
use global_hotkey::{GlobalHotKeyEvent, GlobalHotKeyManager, HotKeyState};
use objc2::MainThreadMarker;
use objc2_app_kit::{NSApplication, NSWindow, NSWindowOcclusionState};
use objc2_foundation::NSString;

/// Test mode: notifications, the badge and the chord are logged, never done.
pub fn quiet() -> bool {
    std::env::var("HERDER_NATIVE_SCRIPT").is_ok_and(|s| !s.trim().is_empty())
}

/// A quiet run with `HERDER_NATIVE_FRONT=1` counts as frontmost, though its window stays behind.
pub fn assume_front() -> bool {
    quiet() && std::env::var("HERDER_NATIVE_FRONT").is_ok_and(|v| v == "1")
}

/// What a quiet run did instead of the real call.
pub fn log(what: impl AsRef<str>) {
    eprintln!("platform: {}", what.as_ref());
}

/// The needs-you count on the dock icon; 0 clears it.
pub fn badge(n: usize) {
    if quiet() {
        return log(format!("badge {n}"));
    }
    let Some(mtm) = MainThreadMarker::new() else {
        return;
    };
    let label = (n > 0).then(|| NSString::from_str(&n.to_string()));
    let tile = NSApplication::sharedApplication(mtm).dockTile();
    tile.setBadgeLabel(label.as_deref());
}

/// Register the summon chord (GPUI's syntax, `ctrl-alt-cmd-h`); each press calls `pressed`, on the
/// main thread. Call once the run loop has started. A chord that does not parse or is taken (another
/// app holds it) is logged, and the app carries on without one.
pub fn summon_chord(chord: &str, pressed: impl Fn() + Send + Sync + 'static) {
    if quiet() {
        return log(format!("would register {chord}"));
    }
    let hotkey = match chord.replace('-', "+").parse::<HotKey>() {
        Ok(hotkey) => hotkey,
        Err(e) => return eprintln!("hotkey: {chord} does not parse: {e}"),
    };
    let manager = match GlobalHotKeyManager::new() {
        Ok(manager) => manager,
        Err(e) => return eprintln!("hotkey: no manager: {e}"),
    };
    if let Err(e) = manager.register(hotkey) {
        return eprintln!("hotkey: {chord} not registered: {e}");
    }
    let id = hotkey.id();
    GlobalHotKeyEvent::set_event_handler(Some(move |e: GlobalHotKeyEvent| {
        if e.id == id && e.state == HotKeyState::Pressed {
            pressed()
        }
    }));
    // Dropping the manager unregisters the chord; it lives as long as the app.
    std::mem::forget(manager);
}

/// Push every window of this app behind the other apps' windows (or in front of them, `front`)
/// without activating anything. Automated runs call this right after opening, so the owner keeps
/// focus (settled decision 8).
pub fn order_windows(front: bool) {
    let Some(mtm) = MainThreadMarker::new() else {
        return;
    };
    let app = NSApplication::sharedApplication(mtm);
    for window in app.windows().iter() {
        match front {
            true => window.orderFrontRegardless(),
            false => window.orderBack(None),
        }
    }
}

/// Whether any window of this app is at least partly on screen (AppKit's occlusion state). An
/// occluded window draws no frames, so the harness reports this beside a CPU figure.
pub fn on_screen() -> bool {
    let Some(mtm) = MainThreadMarker::new() else {
        return false;
    };
    let app = NSApplication::sharedApplication(mtm);
    let visible = |w: &NSWindow| w.occlusionState().contains(NSWindowOcclusionState::Visible);
    app.windows().iter().any(|w| visible(&w))
}
