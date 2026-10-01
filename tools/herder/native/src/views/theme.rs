//! The palette and the type scale: one app-wide factor (`Prefs::text_scale`, ⌘+ ⌘- ⌘0) every size
//! comes from, text and layout alike. Owner ruling (A0): scale 1.0 is the spike's sizes at 0.9× (body
//! 12 × 0.9, code 13 × 0.9, meta 11 × 0.9). Fractional pixels are fine; GPUI does not round text.

use gpui_kit::component::text::TextViewStyle;
use gpui_kit::component::theme::{ThemeConfig, ThemeRegistry};
use gpui_kit::component::{Theme, ThemeMode};
use gpui_kit::{
    App, HighlightStyle, Overflow, Pixels, StyleRefinement, Styled as _, WhiteSpace, px, relative,
    rems, rgb,
};
use std::rc::Rc;

/// The families are explicit so the kit never enumerates installed fonts to resolve `.SystemUIFont`
/// (~150 ms of the cold-start budget). U2 owns the choice.
pub const FONT: &str = "Menlo";
pub const MONO: &str = "Monaco";

/// The owner's factor on the spike's sizes; the spike's body was 12 px.
const OWNER: f32 = 0.9;

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

/// The palette as `rgb()` hex. Neutrals are herder web's dark theme (`styles.css`), so ink on ground
/// reads at web's ~12:1; status colours are navigation lights.
pub mod pal {
    pub const GROUND: u32 = 0x1B1C21;
    pub const PANEL: u32 = 0x212228;
    pub const INK: u32 = 0xD6D8DF;
    pub const SLATE: u32 = 0x8E919C;
    pub const RULE: u32 = 0x2E3037;
    pub const WASH: u32 = 0x26272E;
    /// Web's `--sidebar` (the notes strip) and `--border2` (a card's edge).
    pub const SIDEBAR: u32 = 0x17181D;
    pub const EDGE: u32 = 0x3A3C45;
    /// Web's `--accent`: a selected note's edge and a quoted note's bar.
    pub const BLUE: u32 = 0x5B93FF;
    /// Selected text.
    pub const SELECT: u32 = 0x31406B;
    /// Inline code spans.
    pub const CHIP: u32 = 0x2B2D35;
    /// Fenced code: darker than the ground, dimmer than ink.
    pub const CODE: u32 = 0x15161A;
    pub const CODE_INK: u32 = 0xB6BCC8;
    /// The compact divider's label and rules.
    pub const PURPLE: u32 = 0xB689F4;
    pub const PURPLE_RULE: u32 = 0x3A3050;
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

/// Transcript markdown as web sets it: paragraphs 6 px apart, inline code on a chip, and fenced code
/// darker than the ground in smaller, tighter, dimmer ink, unwrapped so aligned columns stay aligned, scrolling sideways
/// under a horizontal swipe only (a vertical wheel still scrolls the transcript).
pub fn prose(t: TypeScale) -> TextViewStyle {
    let mut code = StyleRefinement::default()
        .bg(rgb(pal::CODE))
        .text_color(rgb(pal::CODE_INK))
        .text_size(t.small)
        .line_height(relative(1.3))
        .p(t.px(10.));
    code.text.white_space = Some(WhiteSpace::Nowrap);
    code.overflow.x = Some(Overflow::Scroll);
    code.restrict_scroll_to_axis = Some(true);
    let chip = HighlightStyle {
        background_color: Some(rgb(pal::CHIP).into()),
        ..Default::default()
    };
    // The gap is in rems of the window's 16 px, so it is given from a design length to follow ⌘+.
    TextViewStyle::default()
        .paragraph_gap(rems(f32::from(t.px(7.)) / 16.))
        .code_block(code)
        .inline_code(chip)
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

/// Our fonts, and the kit's own text (markdown, inputs, lists) in our palette rather than its defaults.
fn ours(c: &ThemeConfig) -> Rc<ThemeConfig> {
    let mut c = c.clone();
    c.font_family = Some(FONT.into());
    c.mono_font_family = Some(MONO.into());
    let hex = |v: u32| Some(format!("#{v:06X}").into());
    let k = &mut c.colors;
    k.background = hex(pal::GROUND);
    k.foreground = hex(pal::INK);
    k.muted = hex(pal::CODE);
    k.muted_foreground = hex(pal::SLATE);
    k.border = hex(pal::RULE);
    k.link = hex(pal::ACC);
    k.selection = hex(pal::SELECT);
    // Markdown tables: the header row as web's `th`, the body on the panel (the kit's table surface is
    // the popover colour).
    k.table_head = hex(pal::WASH);
    k.table_head_foreground = hex(pal::INK);
    k.popover = hex(pal::PANEL);
    k.popover_foreground = hex(pal::INK);
    // The composer's and notes' inputs: border, focus ring and caret.
    k.input = hex(pal::RULE);
    k.ring = hex(pal::ACC);
    k.caret = hex(pal::INK);
    Rc::new(c)
}

/// Before `gpui_kit::init`: a theme global with explicit families, so init never enumerates fonts.
pub fn seed(cx: &mut App) {
    let mut theme = Theme::default();
    theme.light_theme = ours(&ThemeConfig::default());
    theme.dark_theme = theme.light_theme.clone();
    theme.font_family = FONT.into();
    theme.mono_font_family = MONO.into();
    cx.set_global(theme);
}

/// After init: v0 is dark only, the kit's default dark theme with our fonts and palette.
pub fn dark(cx: &mut App) {
    let dark = ours(ThemeRegistry::global(cx).default_dark_theme());
    Theme::global_mut(cx).dark_theme = dark;
    Theme::change(ThemeMode::Dark, None, cx);
    Theme::sync_base(cx);
}
