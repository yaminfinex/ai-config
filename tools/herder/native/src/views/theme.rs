//! The palette and the type scale: one app-wide factor (`Prefs::text_scale`, ⌘+ ⌘- ⌘0) every size
//! comes from, text and layout alike. Owner ruling (A0): scale 1.0 is the spike's sizes at 0.9× (body
//! 12 × 0.9, code 13 × 0.9, meta 11 × 0.9). Fractional pixels are fine; GPUI does not round text.

use gpui_kit::component::theme::{ThemeConfig, ThemeRegistry};
use gpui_kit::component::{Theme, ThemeMode};
use gpui_kit::{App, Pixels, px};
use std::rc::Rc;

/// The families are explicit so the kit never enumerates installed fonts to resolve `.SystemUIFont`
/// (~150 ms of the cold-start budget). U2 owns the choice.
pub const FONT: &str = "Menlo";
pub const MONO: &str = "Monaco";

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
    scale: f32,
}

impl TypeScale {
    /// A layout length (padding, card width) given in the spike's design pixels, so layout follows ⌘+.
    pub fn px(&self, design: f32) -> Pixels {
        px(design * OWNER * self.scale)
    }
}

/// The lens palette as `rgb()` hex (the prototype's); status colours are navigation lights.
pub mod pal {
    pub const GROUND: u32 = 0x0C121C;
    pub const PANEL: u32 = 0x121A26;
    pub const INK: u32 = 0xE4E8EF;
    pub const SLATE: u32 = 0x8C97A8;
    pub const RULE: u32 = 0x1F2A3A;
    pub const WASH: u32 = 0x18222F;
    pub const ACC: u32 = 0x86A8FF;
    pub const ACCW: u32 = 0x1A2745;
    pub const GREEN: u32 = 0x3CC486;
    pub const AMBER: u32 = 0xF2B51C;
    pub const PORT: u32 = 0xF0685C;
}

pub fn type_scale(scale: f32) -> TypeScale {
    let s = |spike_px: f32| px(spike_px * OWNER * scale);
    TypeScale {
        small: s(11.0),
        body: s(12.0),
        title: s(15.0),
        code: s(13.0),
        line: s(18.0),
        scale,
    }
}

/// Push the scale into the kit's theme so its own widgets follow it: `font_size` for inputs, lists and
/// markdown, `mono_font_size` for the code editor. The kit rebuilds its Base defaults only in
/// `sync_base`, and open windows only pick the change up when refreshed.
pub fn apply(scale: f32, cx: &mut App) {
    let t = type_scale(scale);
    let theme = Theme::global_mut(cx);
    theme.font_size = t.body;
    theme.mono_font_size = t.code;
    Theme::sync_base(cx);
    cx.refresh_windows();
}

fn with_fonts(c: &ThemeConfig) -> Rc<ThemeConfig> {
    let mut c = c.clone();
    c.font_family = Some(FONT.into());
    c.mono_font_family = Some(MONO.into());
    Rc::new(c)
}

/// Before `gpui_kit::init`: a theme global with explicit families, so init never enumerates fonts.
pub fn seed(cx: &mut App) {
    let mut theme = Theme::default();
    theme.light_theme = with_fonts(&ThemeConfig::default());
    theme.dark_theme = theme.light_theme.clone();
    theme.font_family = FONT.into();
    theme.mono_font_family = MONO.into();
    cx.set_global(theme);
}

/// After init: v0 is dark only, the kit's default dark theme with our fonts.
pub fn dark(cx: &mut App) {
    let dark = with_fonts(ThemeRegistry::global(cx).default_dark_theme());
    Theme::global_mut(cx).dark_theme = dark;
    Theme::change(ThemeMode::Dark, None, cx);
    Theme::sync_base(cx);
}
