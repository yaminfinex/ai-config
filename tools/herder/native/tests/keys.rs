//! The key guard (ARCHITECTURE §4): no lens or zoom key fires while an Input or a Terminal has focus,
//! checked against the real binding table and GPUI's own matcher.

use gpui_kit::{KeyBinding, KeyContext, Keymap, Keystroke};
use herder_native::views;

fn fires(keymap: &Keymap, key: &str, stack: &[&str]) -> bool {
    let stack: Vec<KeyContext> = stack
        .iter()
        .map(|c| KeyContext::parse(c).unwrap())
        .collect();
    let key = Keystroke::parse(key).unwrap();
    !keymap.bindings_for_input(&[key], &stack).0.is_empty()
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

gpui_kit::actions!(u3, [ScrollDown]);

/// For U3: a transcript's own `j` (bound in a context deeper than `Lens`) must win over the lens's
/// `j`, which HOME's predicate still matches inside the zoom. GPUI ranks the deeper context first.
#[test]
fn a_deeper_context_binding_wins_over_home() {
    let mut bindings = views::bindings();
    bindings.push(KeyBinding::new("j", ScrollDown, Some("Transcript")));
    let keymap = Keymap::new(bindings);
    let stack: Vec<KeyContext> = ["Lens", "Space", "Transcript"]
        .iter()
        .map(|c| KeyContext::parse(c).unwrap())
        .collect();
    let (hits, _) = keymap.bindings_for_input(&[Keystroke::parse("j").unwrap()], &stack);
    assert!(
        hits.first()
            .is_some_and(|b| b.action().partial_eq(&ScrollDown))
    );
}
