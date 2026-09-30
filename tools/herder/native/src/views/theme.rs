//! The type scale: one app-wide factor (`Prefs::text_scale`, ⌘+ ⌘- ⌘0) that every view reads sizes
//! from. Nothing renders text with a hard-coded pixel size; code and terminal fonts scale with it.
//! The base is smaller than the spike's 12 px because the owner found that too big.

use gpui_kit::component::Theme;
use gpui_kit::{App, Pixels, px};

/// Body size at scale 1.0, in pixels.
pub const BASE_PX: f32 = 11.0;

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
    let s = |k: f32| px((BASE_PX * k * scale).round());
    TypeScale {
        small: s(0.9),
        body: s(1.0),
        title: s(1.3),
        code: s(1.0),
        line: s(1.5),
    }
}

/// Push the scale into the kit's theme so its own widgets (inputs, lists, markdown) follow it.
pub fn apply(scale: f32, cx: &mut App) {
    let t = type_scale(scale);
    Theme::global_mut(cx).font_size = t.body;
}
