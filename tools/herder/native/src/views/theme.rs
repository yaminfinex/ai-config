//! The palette and the type scale: one app-wide factor (`Prefs::text_scale`, ⌘+ ⌘- ⌘0) every size
//! comes from, text and layout alike. Owner ruling (A0): scale 1.0 is the spike's sizes at 0.9× (body
//! 12 × 0.9, code 13 × 0.9, meta 11 × 0.9). Fractional pixels are fine; GPUI does not round text.

use gpui_kit::base::TextViewStyle;
use gpui_kit::component::theme::{ThemeConfig, ThemeRegistry};
use gpui_kit::component::{Theme, ThemeMode};
use gpui_kit::{
    App, FontWeight, HighlightStyle, Hsla, Overflow, Pixels, StyleRefinement, Styled as _,
    WhiteSpace, px, relative, rems, rgb, transparent_black,
};
use std::rc::Rc;

/// The families are explicit so the kit never enumerates installed fonts to resolve `.SystemUIFont`
/// (~150 ms of the cold-start budget). U2 owns the choice: the lens, composer and notes stay on Menlo.
pub const FONT: &str = "Menlo";
/// The transcript's, as web's `system-ui` and `ui-monospace` (spec §0): SF Pro and SF Mono. Neither
/// resolves by name ("SF Mono" is Helvetica); these are CoreText's names for the system families.
/// Mono is also the kit's, which only the transcript's markdown uses (code blocks, inline code).
pub const SANS_T: &str = ".AppleSystemUIFont";
pub const MONO_T: &str = ".AppleSystemUIFontMonospaced";

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

    /// A length as web's CSS gives it, so the transcript matches web at scale 1.0 (spec §0).
    pub fn css(&self, web: f32) -> Pixels {
        px(web * self.scale)
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
    /// A selected note, and selected text outside the transcript (the composer, the notes).
    pub const SELECT: u32 = 0x31406B;
    /// Selected transcript text: Chromium's default, as web shows it (spec §1 Selection). The kit
    /// paints a selection over the glyphs, not under them, so it is `SELECTION_OVER` at half
    /// opacity, which on the ground is exactly this (2 × #375576 − #1b1c21).
    pub const SELECTION: u32 = 0x375576;
    pub const SELECTION_OVER: u32 = 0x538ECB;
    /// Web's `--link`, in the transcript.
    pub const LINK: u32 = 0xA9C4FF;
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
    /// Web's `--dimmer`: times and message ids.
    pub const DIMMER: u32 = 0x6A6D78;
    /// Web's `--green`, an operator's: a card's edge, a badge's ink.
    pub const OPERATOR: u32 = 0x3FB950;
    /// Web's `--operator-bg`, `--operator-strong` and `--human-bg`.
    pub const OPERATOR_GROUND: u32 = 0x1B2420;
    pub const OPERATOR_NAME: u32 = 0xB7DDB9;
    pub const HUMAN_GROUND: u32 = 0x1D2320;
    /// Header badges' border, ground and ink: web's operator badge, a request, any other intent, a
    /// thread.
    pub const BADGE_OPERATOR: (u32, u32, u32) = (0x31563A, 0x182E1D, OPERATOR);
    pub const BADGE_REQUEST: (u32, u32, u32) = (0x2C4370, 0x1B2438, BLUE);
    pub const BADGE_INTENT: (u32, u32, u32) = (EDGE, CHIP, SLATE);
    pub const BADGE_THREAD: (u32, u32, u32) = (0x40325C, 0x2A2138, PURPLE);
    /// The queued box: web's `--warning-border`, `--warning-bg`, `--amber`, `--queued-subtext`,
    /// `--queued-divider` and `--success-bg`.
    pub const QUEUE_EDGE: u32 = 0x5C4716;
    pub const QUEUE_GROUND: u32 = 0x3D2E12;
    pub const QUEUE_TITLE: u32 = 0xD29922;
    pub const QUEUE_SUB: u32 = 0xB7A678;
    pub const QUEUE_RULE: u32 = 0x443819;
    pub const QUEUE_OPERATOR: u32 = 0x152519;
    /// A queued message: web's 8% black (`--overlay-subtle`) over the box's ground.
    pub const QUEUE_ROW: u32 = 0x382A11;

    use crate::store::transcript::Tone;

    /// A run pill's border, ground and ink: web's dark `--pill-*` tokens, and `--info-*` for a status.
    pub fn chip(tone: Tone) -> (u32, u32, u32) {
        match tone {
            Tone::Tool => (0x2C4370, 0x1B2438, 0xA9C4FF),
            Tone::Thinking => (0x40325C, 0x2A2138, 0xD2B4FF),
            Tone::Message => (0x31563A, 0x182E1D, 0xB7DDB9),
            Tone::Other => (0x5C4716, 0x3D2E12, 0xE5C365),
            Tone::Status => (0x484B55, 0x26272E, 0xB6BAC4),
        }
    }
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

/// Transcript markdown as web sets it (spec §1 "Markdown prose"), as a whole base style so nothing
/// here reaches the kit's other text (the composer's and notes' inputs keep the app's selection):
/// ink, links #a9c4ff, selection reading #375576 on the ground; blocks 6 apart; headings at web's
/// sizes, bold, with web's bottom margins (no paragraph gap follows a heading, so its bottom
/// padding is the whole gap; above it is only the block before's own gap: the renderer cannot
/// collapse margins by neighbour); fenced code darker than the ground in SF Mono 11, unwrapped so
/// aligned columns stay aligned, scrolling sideways under a horizontal swipe only (a vertical wheel
/// still scrolls the transcript); tables transparent, so a card's ground shows through, cells
/// padded 4 8 and the header row semibold on the wash. Inline code can only take a ground
/// (`HighlightStyle`: no padding, border or radius); the kit sets it in mono at 0.875 of the text,
/// web's 11 of 13.
pub fn prose(t: TypeScale) -> TextViewStyle {
    let ink = |v: u32| Hsla::from(rgb(v));
    let mut code = StyleRefinement::default()
        .font_family(MONO_T)
        .bg(rgb(pal::CODE))
        .text_color(rgb(pal::CODE_INK))
        .text_size(t.css(11.))
        .line_height(relative(1.2))
        .rounded(t.css(5.))
        .p(t.css(9.));
    code.text.white_space = Some(WhiteSpace::Nowrap);
    code.overflow.x = Some(Overflow::Scroll);
    code.restrict_scroll_to_axis = Some(true);
    let chip = HighlightStyle {
        background_color: Some(ink(pal::CHIP)),
        ..Default::default()
    };
    let table = StyleRefinement::default()
        .bg(transparent_black())
        .rounded(px(0.));
    let head = StyleRefinement::default()
        .bg(rgb(pal::WASH))
        .text_color(rgb(pal::INK))
        .font_weight(FontWeight::SEMIBOLD);
    let cell = StyleRefinement::default().px(t.css(8.)).py(t.css(4.));
    let heading = move |level: u8| {
        let (size, bottom) = match level {
            1 => (26., 17.4),
            2 => (19.5, 16.2),
            3 => (15.2, 15.2),
            _ => (13., 17.3),
        };
        StyleRefinement::default()
            .text_size(t.css(size))
            .font_weight(FontWeight::BOLD)
            .pb(t.css(bottom))
    };
    // The gap is in rems, and the kit's root sets the rem to the theme's body size (`apply`).
    TextViewStyle::default()
        .with_foreground(ink(pal::INK))
        .with_muted_foreground(ink(pal::SLATE))
        .with_link(ink(pal::LINK))
        .with_selection(ink(pal::SELECTION_OVER).opacity(0.5))
        .with_code_background(ink(pal::CODE))
        .with_border(ink(pal::RULE))
        .with_paragraph_gap(rems(f32::from(t.css(6.)) / f32::from(t.body)))
        .with_heading(heading)
        .with_code_block(code)
        .with_inline_code(chip)
        .with_table(table)
        .with_table_head(head)
        .with_table_cell(cell)
        .with_dark(true)
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
    c.mono_font_family = Some(MONO_T.into());
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
    theme.mono_font_family = MONO_T.into();
    cx.set_global(theme);
}

/// After init: v0 is dark only, the kit's default dark theme with our fonts and palette.
pub fn dark(cx: &mut App) {
    let dark = ours(ThemeRegistry::global(cx).default_dark_theme());
    Theme::global_mut(cx).dark_theme = dark;
    Theme::change(ThemeMode::Dark, None, cx);
    Theme::sync_base(cx);
}
