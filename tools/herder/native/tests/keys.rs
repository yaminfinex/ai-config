//! The key guard (ARCHITECTURE §4): no lens or zoom key fires while an Input or a Terminal has focus,
//! checked against the real binding table and GPUI's own matcher.

use gpui_kit::{KeyContext, Keymap, Keystroke};
use herder_native::views;

/// A lens or zoom binding fires: the composer's own chords in the box (U4) do not count.
fn fires(keymap: &Keymap, key: &str, stack: &[&str]) -> bool {
    let stack: Vec<KeyContext> = stack
        .iter()
        .map(|c| KeyContext::parse(c).unwrap())
        .collect();
    let key = Keystroke::parse(key).unwrap();
    let (hits, _) = keymap.bindings_for_input(&[key], &stack);
    hits.iter()
        .any(|b| !b.action().name().starts_with("composer::"))
}

#[test]
fn no_navigation_key_fires_inside_an_input_or_terminal() {
    let keymap = Keymap::new(views::bindings());
    let keys = [
        "h",
        "j",
        "k",
        "l",
        "left",
        "right",
        "1",
        "2",
        "3",
        "v",
        "m",
        "u",
        "t",
        "s",
        "?",
        "n",
        "shift-n",
        "enter",
        "escape",
        "[",
        "]",
        "tab",
        "shift-tab",
    ];
    let mut fired_somewhere = 0;
    for key in keys {
        let on_lens = fires(&keymap, key, &["Lens"]);
        let zoomed = fires(&keymap, key, &["Lens", "Space"]);
        fired_somewhere += usize::from(on_lens || zoomed);
        for guard in ["Input", "Terminal"] {
            for stack in [&["Lens", guard][..], &["Lens", "Space", guard][..]] {
                assert!(!fires(&keymap, key, stack), "`{key}` fires in {stack:?}");
            }
        }
    }
    assert_eq!(fired_somewhere, keys.len(), "every key is bound somewhere");
    // The zoom keys live only in the zoom.
    assert!(fires(&keymap, "]", &["Lens", "Space"]) && !fires(&keymap, "]", &["Lens"]));
}

/// U3: the transcript's scroll keys bind in the zoom and win over the lens's `j` / `k`, which HOME's
/// predicate still matches inside the zoom: GPUI ranks the deeper context (`Space`) first.
#[test]
fn transcript_scroll_keys_win_over_home_inside_the_zoom() {
    use views::transcript::Scroll;
    let keymap = Keymap::new(views::bindings());
    let stack: Vec<KeyContext> = ["Lens", "Space"]
        .iter()
        .map(|c| KeyContext::parse(c).unwrap())
        .collect();
    let keys = [
        ("j", Scroll::Lines(1)),
        ("k", Scroll::Lines(-1)),
        ("space", Scroll::Pages(1)),
        ("shift-space", Scroll::Pages(-1)),
        ("g", Scroll::Top),
        ("shift-g", Scroll::Bottom),
    ];
    for (key, scroll) in keys {
        let (hits, _) = keymap.bindings_for_input(&[Keystroke::parse(key).unwrap()], &stack);
        let first = hits.first().map(|b| b.action().partial_eq(&scroll));
        assert_eq!(first, Some(true), "`{key}` scrolls the transcript");
        for guard in ["Input", "Terminal"] {
            assert!(
                !fires(&keymap, key, &["Lens", "Space", guard]),
                "`{key}` in {guard}"
            );
        }
    }
    // On the lens, `j` / `k` still move between rows.
    let home: Vec<KeyContext> = vec![KeyContext::parse("Lens").unwrap()];
    let (hits, _) = keymap.bindings_for_input(&[Keystroke::parse("j").unwrap()], &home);
    assert!(
        hits.first()
            .is_some_and(|b| !b.action().partial_eq(&Scroll::Lines(1)))
    );
}

/// U4: in the box, `cmd-enter` / `cmd-shift-enter` / `escape` are the composer's (not the zoom's
/// `escape`), and `/` `r` focus it from the zoom but type into it once there.
#[test]
fn composer_chords_win_in_the_box_and_focus_keys_stay_in_the_zoom() {
    use views::composer::Compose;
    let keymap = Keymap::new(views::bindings());
    let parse = |s: &[&str]| -> Vec<KeyContext> {
        s.iter().map(|c| KeyContext::parse(c).unwrap()).collect()
    };
    let first = |key: &str, stack: &[&str]| {
        let (hits, _) = keymap.bindings_for_input(&[Keystroke::parse(key).unwrap()], &parse(stack));
        hits.first().map(|b| b.action().boxed_clone())
    };
    let boxed = ["Lens", "Space", "Input"];
    for (key, want) in [
        ("cmd-enter", Compose::Send),
        ("cmd-shift-enter", Compose::FileBack),
        ("escape", Compose::Leave),
    ] {
        let got = first(key, &boxed);
        assert!(
            got.is_some_and(|a| a.partial_eq(&want)),
            "`{key}` in the box"
        );
    }
    for key in ["/", "r"] {
        let got = first(key, &["Lens", "Space"]);
        assert!(
            got.is_some_and(|a| a.partial_eq(&Compose::Focus)),
            "`{key}` in the zoom"
        );
        assert!(first(key, &boxed).is_none(), "`{key}` types in the box");
        assert!(!fires(&keymap, key, &["Lens"]), "`{key}` is not a lens key");
    }
}
