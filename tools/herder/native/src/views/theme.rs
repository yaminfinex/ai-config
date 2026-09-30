//! The type scale: one app-wide factor (`Prefs::text_scale`, ⌘+ ⌘- ⌘0) that every view reads sizes
//! from. Nothing renders text with a hard-coded pixel size; code and terminal fonts scale with it.
//! Owner ruling (A0): the spike's sizes at 0.9× feel right, so scale 1.0 is exactly that: body
//! 12 × 0.9, code 13 × 0.9, meta 11 × 0.9. Fractional pixels are fine; GPUI does not round text.

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

/// Push the scale into the kit's theme so its own widgets follow it: `font_size` for inputs, lists and
/// markdown, `mono_font_size` for the code editor. The kit rebuilds its Base defaults only in
/// `sync_base`, and open windows only pick the change up when refreshed.
pub fn apply(scale: f32, cx: &mut App) {
    let t = type_scale(scale);
    {
        let theme = Theme::global_mut(cx);
        theme.font_size = t.body;
        theme.mono_font_size = t.code;
    }
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
