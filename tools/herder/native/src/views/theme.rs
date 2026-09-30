//! The type scale: one app-wide factor (`Prefs::text_scale`, ⌘+ ⌘- ⌘0) that every view reads sizes
//! from. Nothing renders text with a hard-coded pixel size; code and terminal fonts scale with it.
//! Owner ruling (A0): the spike's sizes at 0.9× feel right, so scale 1.0 is exactly that: body
//! 12 × 0.9, code 13 × 0.9, meta 11 × 0.9. Fractional pixels are fine; GPUI does not round text.

use gpui_kit::component::Theme;
use gpui_kit::{App, Pixels, px};

/// The owner's factor on the spike's sizes; the spike's body was 12 px.
const OWNER: f32 = 0.9;
/// Body size at scale 1.0, in pixels.
pub const BASE_PX: f32 = 12.0 * OWNER;

/// Sizes for one scale factor. Line height is 1.5× body, which keeps transcript rows even.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct TypeScale {
    pub small: Pixels,
    pub body: Pixels,
    pub title: Pixels,
    pub code: Pixels,
    pub line: Pixels,
}

pub fn type_scale(scale: f32) -> TypeScale {
    // Spike pixel sizes, then the owner's factor and the live scale.
    let s = |spike_px: f32| px(spike_px * OWNER * scale);
    TypeScale {
        small: s(11.0),
        body: s(12.0),
        title: s(15.0),
        code: s(13.0),
        line: s(18.0),
    }
}

/// Push the scale into the kit's theme so its own widgets (inputs, lists, markdown) follow it.
pub fn apply(scale: f32, cx: &mut App) {
    let t = type_scale(scale);
    Theme::global_mut(cx).font_size = t.body;
}
