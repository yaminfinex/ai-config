//! The AppKit calls GPUI doesn't wrap, through objc2 (settled decision 2). Everything here runs on the
//! main thread. Coming with their units: the dock badge (`dockTile().setBadgeLabel`, U6) and the
//! `global-hotkey` bridge (U6). Notifications use GPUI's own `show_system_notification`.

use objc2::MainThreadMarker;
use objc2_app_kit::NSApplication;

/// Push every window of this app behind the other apps' windows without activating anything.
/// Automated runs call this right after opening, so the owner keeps focus (settled decision 8).
pub fn order_windows_back() {
    let Some(mtm) = MainThreadMarker::new() else {
        return;
    };
    let app = NSApplication::sharedApplication(mtm);
    for window in app.windows().iter() {
        window.orderBack(None);
    }
}
