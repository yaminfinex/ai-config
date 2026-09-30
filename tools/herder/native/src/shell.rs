//! The app shell: owns the `Store`, the background threads, the one channel into the foreground, the
//! window, the keymap and the kit widget state. Views render from the store it holds; every change to
//! the store goes through `Shell::dispatch`, which runs `Store::apply` and then the effects.
//!
//! Boot order matters for the 300 ms budget: theme fonts are seeded before `gpui_kit::init` (the
//! kit otherwise enumerates every installed font to resolve `.SystemUIFont`, ~150 ms), then the window
//! opens and paints from the disk snapshot while the network catches up.

use crate::store::{Effect, Event, Store, TextScale};
use crate::views::theme;
use crate::{harness, local, platform_mac};
use gpui_kit::component::theme::{ThemeConfig, ThemeRegistry};
use gpui_kit::component::{ActiveTheme as _, Theme, ThemeMode};
use gpui_kit::*;

/// Window title and app name.
pub const APP_NAME: &str = "herder native";
const APP_ID: &str = "dev.herder.native";
/// The families are explicit so the kit never enumerates fonts (see the module doc). U2 owns the choice.
const FONT: &str = "Menlo";
const MONO: &str = "Monaco";

actions!(herder, [Quit, TextBigger, TextSmaller, TextReset]);

pub struct Shell {
    pub store: Store,
    focus: FocusHandle,
}

impl Shell {
    fn new(cx: &mut Context<Self>) -> Self {
        let mut store = Store::default();
        if let Some(prefs) = local::load_prefs() {
            store.apply(Event::PrefsLoaded(prefs));
        }
        theme::apply(store.prefs.text_scale, cx);
        Shell {
            store,
            focus: cx.focus_handle(),
        }
    }

    /// The only path to a state change: reduce, then run the effects.
    pub fn dispatch(&mut self, event: Event, cx: &mut Context<Self>) {
        for effect in self.store.apply(event) {
            match effect {
                Effect::Persist(prefs) => {
                    theme::apply(prefs.text_scale, cx);
                    cx.background_executor()
                        .spawn(async move { local::save_prefs(&prefs) })
                        .detach();
                }
            }
        }
        cx.notify();
    }
}

impl Render for Shell {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let colors = cx.theme();
        let (bg, fg) = (colors.background, colors.muted_foreground);
        let scale = self.store.prefs.text_scale;
        let t = theme::type_scale(scale);
        div()
            .id("root")
            .track_focus(&self.focus)
            .key_context("Lens")
            .on_action(|_: &Quit, _, cx| cx.quit())
            .on_action(cx.listener(|s, _: &TextBigger, _, cx| {
                s.dispatch(Event::TextScale(TextScale::Bigger), cx)
            }))
            .on_action(cx.listener(|s, _: &TextSmaller, _, cx| {
                s.dispatch(Event::TextScale(TextScale::Smaller), cx)
            }))
            .on_action(cx.listener(|s, _: &TextReset, _, cx| {
                s.dispatch(Event::TextScale(TextScale::Reset), cx)
            }))
            .size_full()
            .flex()
            .flex_col()
            .items_center()
            .justify_center()
            .gap(t.body)
            .bg(bg)
            .text_color(fg)
            .font_family(FONT)
            .text_size(t.body)
            .line_height(t.line)
            .child(
                div()
                    .text_size(t.title)
                    .child(format!("{APP_NAME} · skeleton (A0)")),
            )
            .child(
                div()
                    .text_size(t.small)
                    .child(format!("text ×{scale:.2} · ⌘+ ⌘- ⌘0")),
            )
    }
}

fn with_fonts(c: &ThemeConfig) -> std::rc::Rc<ThemeConfig> {
    let mut c = c.clone();
    c.font_family = Some(FONT.into());
    c.mono_font_family = Some(MONO.into());
    std::rc::Rc::new(c)
}

/// Seed explicit font families before `gpui_kit::init` so it never enumerates installed fonts.
fn seed_theme(cx: &mut App) {
    let mut theme = Theme::default();
    theme.light_theme = with_fonts(&ThemeConfig::default());
    theme.dark_theme = theme.light_theme.clone();
    theme.font_family = FONT.into();
    theme.mono_font_family = MONO.into();
    cx.set_global(theme);
}

pub fn run() {
    harness::start_clock();
    let script = harness::script();
    gpui_kit::application().run(move |cx| {
        seed_theme(cx);
        gpui_kit::init(cx);
        cx.set_app_identity(APP_ID, APP_NAME);
        // v0 is dark only: the kit's default dark theme, with our fonts.
        let dark = with_fonts(ThemeRegistry::global(cx).default_dark_theme());
        Theme::global_mut(cx).dark_theme = dark;
        Theme::change(ThemeMode::Dark, None, cx);
        Theme::sync_base(cx);
        cx.bind_keys([
            KeyBinding::new("cmd-q", Quit, Some("Lens")),
            KeyBinding::new("cmd-=", TextBigger, Some("Lens")),
            KeyBinding::new("cmd-shift-=", TextBigger, Some("Lens")),
            KeyBinding::new("cmd--", TextSmaller, Some("Lens")),
            KeyBinding::new("cmd-0", TextReset, Some("Lens")),
        ]);
        harness::metric("init done");

        let automated = script.is_some();
        let opts = WindowOptions {
            window_bounds: Some(WindowBounds::centered(size(px(1400.), px(900.)), cx)),
            titlebar: Some(TitlebarOptions {
                title: Some(APP_NAME.into()),
                ..Default::default()
            }),
            // An automated run never takes focus (settled decision 8).
            focus: !automated,
            ..Default::default()
        };
        let (handle, shell) =
            gpui_kit::open_window(opts, cx, |_, cx| cx.new(Shell::new)).expect("window");
        if automated {
            platform_mac::order_windows_back();
        } else {
            cx.activate(true);
        }
        let _ = handle.update(cx, |_, window, cx| {
            let focus = shell.read(cx).focus.clone();
            window.focus(&focus, cx);
            if let Some(script) = script.clone() {
                window
                    .spawn(cx, async move |cx| harness::run(script, cx).await)
                    .detach();
            }
        });
        harness::metric("window opened");
    });
}
