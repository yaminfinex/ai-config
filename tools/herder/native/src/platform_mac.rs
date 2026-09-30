//! The AppKit calls GPUI doesn't wrap, through objc2 (settled decision 2). Everything here runs on the
//! main thread. Coming with their units: the dock badge (`dockTile().setBadgeLabel`, U6) and the
//! `global-hotkey` bridge (U6). Notifications use GPUI's own `show_system_notification`.

use objc2::MainThreadMarker;
use objc2_app_kit::{NSApplication, NSWindow, NSWindowOcclusionState};

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
