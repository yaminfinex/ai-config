//! The lens and zoom key logic against the fixture store: which moves reach the store, where the
//! selection and zoom go, and when the selected card is revealed. Drawing is the harness's job.

use crate::api::Entries;
use crate::store::spaces::{Move, Row, Space, Stop};
use crate::store::tests::{board, bump, dwell, fleet_frame, frame, loaded, space_of};
use crate::store::transcript::{Got, Step, What};
use crate::store::{Effect, Event, Fetch, Store};
use crate::views::dock::Ask;
use crate::views::lens::{self, Nav, State};
use crate::views::space::{self, Zoomed};
use crate::views::transcript::{self, Scroll};

/// A live store where mupu (slack) and orch-lega (herder) have unread turns.
fn store() -> Store {
    let mut store = loaded();
    let mut b = board();
    store.apply(fleet_frame(b.clone()));
    bump(&mut b, "mupu", 1);
    bump(&mut b, "orch-lega", 1);
    store.apply(fleet_frame(b));
    store
}

/// The `View` move expected for `agent` in `space`.
fn view(space: &Space, agent: &str) -> (String, Option<String>) {
    (space.id.clone(), Some(agent.to_string()))
}

/// The zoomed space and agent.
fn zoomed(ui: &State) -> Option<(&str, Option<&str>)> {
    let zoom = ui.zoom.as_ref();
    zoom.map(|z| (z.space.as_str(), z.agent.as_deref()))
}

/// What the store is told after an action: a `View` move it returned (a summon of the zoom it is
/// in), else the zoom it leaves, which the dock tells the store (`dock::sync`).
fn viewed(ui: &State, events: Vec<Event>) -> Vec<(String, Option<String>)> {
    let views = events.into_iter().filter_map(|e| match e {
        Event::Lens(Move::View { space, agent, .. }) => Some((space, agent)),
        _ => None,
    });
    let views: Vec<_> = views.collect();
    match (views.is_empty(), ui.zoom.as_ref()) {
        (true, Some(z)) => vec![(z.space.clone(), z.agent.clone())],
        _ => views,
    }
}

#[test]
fn zooming_in_views_the_agent_needing_you_and_tab_asks_the_dock() {
    let store = store();
    let herder = space_of(&store, "orch-lega").clone();
    let mut ui = State::default();
    let events = space::zoom_into(&store, &mut ui, &herder, None);
    assert_eq!(viewed(&ui, events), [view(&herder, "orch-lega")]);
    assert_eq!(zoomed(&ui), Some((herder.id.as_str(), Some("orch-lega"))));
    assert!(
        ui.anim.as_ref().is_some_and(|a| a.morphs()),
        "enter morphs from the card"
    );

    // `tab` is the focused group's next tab, which the dock knows (`dock::sync`).
    assert!(space::act(&store, &mut ui, Zoomed::Agent(1)).is_empty());
    assert_eq!(ui.asks, [Ask::Step(1)]);
    assert_eq!(zoomed(&ui), Some((herder.id.as_str(), Some("orch-lega"))));
    // `alt-u` toggles the zoomed agent read or unread (RM).
    let toggled = space::act(&store, &mut ui, Zoomed::Read);
    assert!(matches!(&toggled[..], [Event::Lens(Move::Toggle(a))] if a == "orch-lega"));

    let hide = |events: Vec<Event>| matches!(events.as_slice(), [Event::Transcript(_)]);
    assert!(
        hide(space::act(&store, &mut ui, Zoomed::Out)),
        "the transcript goes"
    );
    assert_eq!(zoomed(&ui), None);
    let leaving = ui.anim.as_ref().and_then(|a| a.leaving());
    assert_eq!(
        leaving.map(|z| z.space.as_str()),
        Some(herder.id.as_str()),
        "morphs back"
    );
}

#[test]
fn n_and_brackets_move_between_spaces_and_swipe_when_zoomed() {
    let store = store();
    let mut ui = State::default();
    let events = lens::act(&store, &mut ui, Nav::NextNeeding(false));
    assert!(events.is_empty(), "n only selects");
    let Some(Stop::Space(first)) = store.next_needing(None, false) else {
        panic!("a space needs you")
    };
    assert_eq!(ui.selected(&store).map(|s| &s.id), Some(&first.id));
    assert!(ui.reveal.get(), "the selection is revealed");

    lens::act(&store, &mut ui, Nav::ZoomIn);
    let Some(Stop::Space(next)) = store.next_needing(Some(Stop::Space(first)), true) else {
        panic!("another space needs you")
    };
    let events = lens::next_needing(&store, &mut ui, true);
    assert_eq!(zoomed(&ui).map(|z| z.0), Some(next.id.as_str()));
    assert_eq!(viewed(&ui, events).len(), 1);
    assert!(
        ui.anim.as_ref().is_some_and(|a| !a.morphs()),
        "a swipe inside the zoom"
    );

    let order = store.lens();
    let at = order.iter().position(|s| s.id == next.id).unwrap();
    space::act(&store, &mut ui, Zoomed::Space(1));
    let after = order[(at + 1) % order.len()];
    assert_eq!(zoomed(&ui).map(|z| z.0), Some(after.id.as_str()));
    assert_eq!(
        ui.selected(&store).map(|s| &s.id),
        Some(&after.id),
        "the lens follows"
    );
}

/// Owner ruling: after the spaces, `N` (and `n` zoomed) opens an agent needing you in no space alone,
/// as its notification's click does, then goes round to the first space; `n` on the lens passes it by.
#[test]
fn n_reaches_an_agent_in_no_space_after_the_spaces() {
    let mut store = store();
    let alone = "risk-framework-gezu";
    let mut b = board();
    for agent in ["mupu", "orch-lega", alone] {
        bump(&mut b, agent, 1);
    }
    store.apply(fleet_frame(b));
    assert!(store.agent_needs_you(alone) && store.home(alone).is_none());
    let order: Vec<String> = (store.lens().into_iter())
        .filter(|s| store.needs_you(s) > 0)
        .map(|s| s.id.clone())
        .collect();
    assert_eq!(order.len(), 2);
    let mut ui = State::default();
    ui.select(&order[1]);
    assert!(lens::act(&store, &mut ui, Nav::NextNeeding(false)).is_empty());
    assert_eq!(
        ui.selected(&store).map(|s| &s.id),
        Some(&order[0]),
        "n passes it by"
    );
    ui.select(&order[1]);
    let events = lens::act(&store, &mut ui, Nav::NextNeeding(true));
    assert_eq!(viewed(&ui, events), [(String::new(), Some(alone.into()))]);
    assert_eq!(zoomed(&ui), Some(("", Some(alone))));
    // Zoomed, `n` goes on round: the first space.
    let events = lens::next_needing(&store, &mut ui, false);
    assert_eq!(viewed(&ui, events).len(), 1);
    assert_eq!(zoomed(&ui).map(|z| z.0), Some(order[0].as_str()));
}

#[test]
fn membership_changing_while_zoomed() {
    let mut store = store();
    let herder = space_of(&store, "orch-lega").clone();
    let mut ui = State::default();
    space::zoom_into(&store, &mut ui, &herder, None);

    // The space goes: any zoom key morphs back to the lens.
    let at = store.spaces.iter().position(|s| s.id == herder.id).unwrap();
    store.spaces.remove(at);
    let events = space::act(&store, &mut ui, Zoomed::Space(1));
    assert!(matches!(events.as_slice(), [Event::Transcript(_)]));
    assert_eq!(zoomed(&ui), None);
}

#[test]
fn moves_and_placement_reveal_the_selection() {
    let mut store = store();
    let mut ui = State::default();
    for nav in [
        Nav::Step(1),
        Nav::Place(Row::Background),
        Nav::StepRow(-1),
        Nav::CardSize,
        Nav::CardText,
    ] {
        ui.reveal.set(false);
        for event in lens::act(&store, &mut ui, nav) {
            store.apply(event);
        }
        assert!(
            ui.reveal.get(),
            "{nav:?} reveals (card size and text reflow the rows)"
        );
    }
    let events = lens::act(&store, &mut ui, Nav::Unread);
    assert!(matches!(events.as_slice(), [Event::Lens(Move::Unread(_))]));
}

#[test]
fn placing_the_implicit_first_selection_keeps_it_selected() {
    let mut store = store();
    let mut ui = State::default();
    let first = ui.selected(&store).unwrap().id.clone();
    for event in lens::act(&store, &mut ui, Nav::Place(Row::Background)) {
        store.apply(event);
    }
    assert_ne!(store.lens()[0].id, first, "the space moved down the lens");
    assert_eq!(
        ui.selected(&store).unwrap().id,
        first,
        "and the selection with it"
    );
}

/// Owner ruling (c): a scroll key that leaves the bottom says so as it moves the list, so a fleet frame
/// drained before the next render does not land as seen.
#[test]
fn a_scroll_key_leaves_the_tail_before_the_next_render() {
    let mut store = loaded();
    let mut b = board();
    store.apply(fleet_frame(b.clone()));
    store.apply(Event::Front(true));
    let (space, agent) = (space_of(&store, "mupu").id.clone(), Some("mupu".into()));
    let effects = store.apply(Event::Lens(Move::View {
        space,
        agent,
        beside: Vec::new(),
    }));
    let read = effects.into_iter().find_map(|e| match e {
        Effect::Fetch(Fetch::Transcript(r)) if matches!(r.what, What::Page(_)) => Some(r),
        _ => None,
    });
    let tail = include_str!("../../testdata/agents/mupu/tail.json");
    let page: Entries = serde_json::from_str(tail).expect("entries fixture decodes");
    let got = Ok(Got::Page(Box::new(page)));
    store.apply(Event::Transcript(Step::Read(read.unwrap(), got)));
    // A render: the list mirrors the rows and follows the bottom, and says so.
    let view = transcript::View::default();
    view.sync(store.transcript.focused().unwrap(), &store);
    store.apply(view.tail(true));
    dwell(&mut store);
    bump(&mut b, "mupu", 1);
    store.apply(frame(&store, b.clone()));
    assert!(!store.agent_needs_you("mupu"), "watched as it lands");
    // `g`, then a fleet frame before any render.
    for event in transcript::scroll(&store, &view, Scroll::Top) {
        store.apply(event);
    }
    bump(&mut b, "mupu", 1);
    store.apply(frame(&store, b));
    assert!(store.agent_needs_you("mupu"), "scrolled off the bottom");
}

mod links {
    use crate::views::markdown::{Mentions, link, path_like, route, vscode, vscode_url};

    const WEB: &str = "http://h:4400/agents/riko";

    fn linked(text: &str) -> String {
        let names = ["native-kona", "native-bozo", "riko", "orch-lega"];
        link(text, &Mentions::new(names), WEB)
    }

    #[test]
    fn mentions_link_board_names_and_unique_base_names() {
        assert_eq!(
            linked("ask @kona, then native-bozo and riko."),
            "ask [@kona](herder-agent:native-kona), then [native-bozo](herder-agent:native-bozo) and [riko](herder-agent:riko)."
        );
        // Unknown names, a longer word, and names beside a path or an extension are not mentions.
        for plain in [
            "@zzzz is new",
            "konaville",
            "see ~/x/kona",
            "kona.md is a file",
            "herder@riko",
        ] {
            assert!(
                !linked(plain).contains("herder-agent:"),
                "{plain}: {}",
                linked(plain)
            );
        }
    }

    #[test]
    fn code_is_not_linked_except_a_path_in_a_code_span() {
        assert_eq!(linked("`@kona`"), "`@kona`");
        assert_eq!(linked("`self.items`"), "`self.items`");
        assert_eq!(
            linked("see `src/views/transcript.rs:12`"),
            "see [`src/views/transcript.rs:12`](<herder-path:src/views/transcript.rs:12>)"
        );
        let fenced = "```rust\nlet kona = \"src/main.rs\";\n```\n";
        assert_eq!(linked(fenced), fenced);
        let indented = "text\n\n    riko src/a/b.rs\n";
        assert_eq!(linked(indented), indented);
        assert_eq!(
            linked("[riko](https://x.y/src/a/b)"),
            "[riko](https://x.y/src/a/b)"
        );
        // The parser's boundaries: a longer fence holds a shorter one, a code span may span lines, a
        // reference link keeps its syntax.
        let long = "````md\n```\nriko src/a/b.rs\n````\nriko\n";
        assert_eq!(
            linked(long),
            "````md\n```\nriko src/a/b.rs\n````\n[riko](herder-agent:riko)\n"
        );
        let span = "a `riko\nsrc/a/b.rs` b";
        assert_eq!(linked(span), span);
        let reference = "see [riko][ref]\n\n[ref]: https://x.y\n";
        assert_eq!(linked(reference), reference);
    }

    #[test]
    fn mermaid_links_to_web_and_authored_paths_route_to_open_path() {
        let diagram = "before\n\n```mermaid\ngraph TD; a-->b\n```\n";
        assert_eq!(
            linked(diagram),
            format!("before\n\n[view diagram in web ↗](<{WEB}>)\n")
        );
        assert_eq!(
            route("docs/plan.md").as_deref(),
            Some("herder-path:docs/plan.md")
        );
        assert_eq!(
            route("/etc/hosts").as_deref(),
            Some("herder-path:/etc/hosts")
        );
        assert_eq!(
            route("herder-agent:riko").as_deref(),
            Some("herder-agent:riko")
        );
        for elsewhere in ["https://x.y/a", "mailto:a@b", "#heading", ""] {
            assert_eq!(route(elsewhere), None, "{elsewhere}");
        }
    }

    #[test]
    fn paths_exclude_trailing_punctuation_and_prose_look_alikes() {
        assert_eq!(
            linked("Read ARCHITECTURE.md. Then (src/store/mod.rs:40), done"),
            "Read [ARCHITECTURE.md](<herder-path:ARCHITECTURE.md>). Then ([src/store/mod.rs:40](<herder-path:src/store/mod.rs:40>)), done"
        );
        assert_eq!(
            linked("**~/x/y.toml**"),
            "**[~/x/y.toml](<herder-path:~/x/y.toml>)**"
        );
        for word in [
            "e.g.",
            "and/or",
            "/compact",
            "v0.3.7",
            "3.14",
            "12:30",
            "https://a.b/c/d/e",
            "a=b/c/d",
        ] {
            assert!(!path_like(word, false), "{word}");
        }
        for word in [
            "./run.sh",
            "~/notes",
            "/etc/hosts",
            "a/b/c",
            "lib.rs",
            "x.rs:12",
        ] {
            assert!(path_like(word, false), "{word}");
        }
        assert!(path_like("src/store", true) && !path_like("src/store", false));
    }

    #[test]
    fn vscode_urls_encode_each_segment() {
        let url = vscode_url("superset", "/home/u/a b/ü.rs", Some(7));
        assert_eq!(
            url.as_deref(),
            Some("vscode://vscode-remote/ssh-remote+superset/home/u/a%20b/%C3%BC.rs:7")
        );
        assert_eq!(vscode_url("bad host", "/x", None), None);
        assert_eq!(vscode_url("superset", "relative", None), None);
    }

    /// G3b: two calls, the root opened as VS Code's folder (a URI, each segment encoded) and then the
    /// file in that window at its line, one argument each, spaces kept; a folder alone is the first
    /// call. The URL is for when the tool is missing (a file at line 1 at least, so not as a folder).
    #[test]
    fn vscode_opens_the_folder_then_goes_to_the_file() {
        let (calls, url) =
            vscode("superset", "/home/u/my repo/", Some("a b/x.rs"), Some(7)).unwrap();
        assert_eq!(
            calls,
            [
                vec![
                    "--folder-uri",
                    "vscode-remote://ssh-remote+superset/home/u/my%20repo"
                ],
                vec![
                    "-r",
                    "--remote",
                    "ssh-remote+superset",
                    "-g",
                    "/home/u/my repo/a b/x.rs:7"
                ],
            ]
        );
        assert_eq!(
            url,
            "vscode://vscode-remote/ssh-remote+superset/home/u/my%20repo/a%20b/x.rs:7"
        );
        let (calls, url) = vscode("superset", "/r", Some("x.rs"), None).unwrap();
        assert_eq!(calls[1][3..], ["-g", "/r/x.rs"]);
        assert!(url.ends_with("/r/x.rs:1"));
        let (calls, url) = vscode("superset", "/r", None, Some(3)).unwrap();
        assert_eq!(
            calls,
            [["--folder-uri", "vscode-remote://ssh-remote+superset/r"]]
        );
        assert_eq!(url, "vscode://vscode-remote/ssh-remote+superset/r");
        assert_eq!(vscode("bad host", "/r", None, None), None);
        assert_eq!(vscode("superset", "relative", None, None), None);
    }
}

/// A notification's click (`space::summon`, U6).
mod summon {
    use super::*;
    use crate::views::space::Zoom;

    #[test]
    fn an_agent_opens_in_its_first_space_from_anywhere() {
        let store = store();
        let slack = space_of(&store, "mupu").clone();
        let chief = space_of(&store, "chief-mihe").clone();
        let mut ui = State::default();
        let events = space::summon(&store, &mut ui, "agent:mupu");
        assert_eq!(viewed(&ui, events), [view(&slack, "mupu")]);
        assert_eq!(zoomed(&ui), Some((slack.id.as_str(), Some("mupu"))));

        // Open elsewhere (a preview in another space): it moves to its own space.
        ui.zoom = Some(Zoom {
            space: chief.id.clone(),
            agent: Some("mupu".into()),
        });
        let events = space::summon(&store, &mut ui, "agent:mupu");
        assert_eq!(viewed(&ui, events), [view(&slack, "mupu")]);
        assert_eq!(zoomed(&ui), Some((slack.id.as_str(), Some("mupu"))));
    }

    #[test]
    fn an_agent_already_open_is_still_seen() {
        let store = store();
        let slack = space_of(&store, "mupu").clone();
        let mut ui = State::default();
        space::summon(&store, &mut ui, "agent:mupu");
        let events = space::summon(&store, &mut ui, "agent:mupu");
        assert_eq!(
            viewed(&ui, events),
            [view(&slack, "mupu")],
            "its new turn is seen"
        );
        assert_eq!(zoomed(&ui), Some((slack.id.as_str(), Some("mupu"))));
    }

    /// Owner ruling: an agent in no space opens alone, a preview in a zoom of no space; only `escape`
    /// does anything there, back to the lens, and no space gains a member.
    #[test]
    fn an_agent_in_no_space_opens_alone() {
        let store = store();
        let alone = "risk-framework-gezu";
        let mut ui = State::default();
        let before = ui.selected(&store).map(|s| s.id.clone());
        let events = space::summon(&store, &mut ui, &format!("agent:{alone}"));
        assert_eq!(viewed(&ui, events), [(String::new(), Some(alone.into()))]);
        assert_eq!(zoomed(&ui), Some(("", Some(alone))));
        assert!(ui.zoom.as_ref().is_some_and(Zoom::alone));
        assert_eq!(
            crate::views::probe::shown(&store, &ui),
            format!("{alone} preview")
        );
        assert_eq!(ui.selected(&store).map(|s| s.id.clone()), before);
        // Summoned again while open: still seen.
        let events = space::summon(&store, &mut ui, &format!("agent:{alone}"));
        assert_eq!(viewed(&ui, events), [(String::new(), Some(alone.into()))]);
        for key in [Zoomed::Agent(1), Zoomed::Space(1), Zoomed::Space(-1)] {
            assert!(space::act(&store, &mut ui, key).is_empty());
            assert_eq!(zoomed(&ui), Some(("", Some(alone))));
        }
        let toggled = space::act(&store, &mut ui, Zoomed::Read);
        assert!(matches!(&toggled[..], [Event::Lens(Move::Toggle(a))] if a == alone));
        space::act(&store, &mut ui, Zoomed::Out);
        assert_eq!(zoomed(&ui), None);
        assert!(store.spaces.iter().all(|s| s.agents().all(|a| a != alone)));
    }

    #[test]
    fn a_summary_or_the_chord_comes_back_to_the_lens() {
        let store = store();
        let herder = space_of(&store, "orch-lega").clone();
        let mut ui = State::default();
        space::summon(&store, &mut ui, "agent:mupu");
        let events = space::summon(&store, &mut ui, &format!("space:{}", herder.id));
        assert!(viewed(&ui, events).is_empty());
        assert_eq!(zoomed(&ui), None);
        assert_eq!(ui.selected(&store).map(|s| &s.id), Some(&herder.id));
        space::summon(&store, &mut ui, "agent:mupu");
        space::summon(&store, &mut ui, "");
        assert_eq!(zoomed(&ui), None);
        space::summon(&store, &mut ui, "agent:risk-framework-gezu");
        space::summon(&store, &mut ui, "lens");
        assert_eq!(zoomed(&ui), None);
    }
}

/// A synthetic wheel over a fenced block in a transcript row (F1): a mostly sideways gesture scrolls the
/// block and leaves the list alone; a mostly vertical one scrolls the list.
mod wheel {
    use crate::views::theme;
    use crate::views::transcript::sideways;
    use gpui_kit::base::TextView;
    use gpui_kit::test::{TestSupportExt as _, TestWindowExt as _};
    use gpui_kit::{
        Context, Empty, InteractiveElement as _, IntoElement, ListAlignment, ListState,
        ParentElement as _, Render, ScrollDelta, Styled as _, TestAppContext, VisualTestContext,
        Window, div, list, point, px,
    };

    struct Rows(ListState);

    impl Render for Rows {
        fn render(&mut self, _: &mut Window, _: &mut Context<Self>) -> impl IntoElement {
            let fence: String = format!("```\n{}\n```", "wide ".repeat(80));
            let row = move |ix: usize, _: &mut Window, _: &mut gpui_kit::App| {
                let md = TextView::markdown(("md", ix), fence.clone())
                    .style(theme::prose(theme::type_scale(1.)))
                    .code_block_actions(|_, _, _| Empty);
                let target = div().id(("row", ix)).w(px(300.)).child(md).test_support();
                sideways(div().child(target)).into_any_element()
            };
            div()
                .size_full()
                .child(list(self.0.clone(), row).size_full())
        }
    }

    fn rows(cx: &mut TestAppContext) -> (ListState, &mut VisualTestContext) {
        cx.update(|cx| {
            theme::seed(cx);
            gpui_kit::init(cx);
            theme::dark(cx);
        });
        let state = ListState::new(40, ListAlignment::Top, px(400.));
        let view = state.clone();
        let (_, cx) = cx.add_window_view(move |_, _| Rows(view));
        cx.run_until_parked();
        cx.update(|window, cx| window.render_frame(cx));
        (state, cx)
    }

    fn top(state: &ListState) -> (usize, f32) {
        let top = state.logical_scroll_top();
        (top.item_ix, f32::from(top.offset_in_item))
    }

    #[gpui_kit::test]
    fn a_sideways_gesture_over_a_fence_leaves_the_list(cx: &mut TestAppContext) {
        let (state, cx) = rows(cx);
        let swipe = ScrollDelta::Pixels(point(px(-40.), px(-10.)));
        cx.update(|window, cx| window.scroll(("row", 1usize), swipe, cx));
        assert_eq!(top(&state), (0, 0.));
    }

    #[gpui_kit::test]
    fn a_vertical_gesture_over_a_fence_scrolls_the_list(cx: &mut TestAppContext) {
        let (state, cx) = rows(cx);
        let swipe = ScrollDelta::Pixels(point(px(-10.), px(-40.)));
        cx.update(|window, cx| window.scroll(("row", 1usize), swipe, cx));
        assert_ne!(top(&state), (0, 0.));
    }
}

/// F6: the notes list's selection (web's `notesListModel`) and what `cmd-c` copies.
mod notes_list {
    use crate::api::{StateRow, StateRows};
    use crate::store::sync::{Ns, Step};
    use crate::store::tests::loaded;
    use crate::store::{Event, notes::transfer_text};
    use crate::views::notes_list::{Picked, copied};

    fn ids() -> Vec<String> {
        ["a", "b", "c", "d"].map(String::from).to_vec()
    }

    /// The selection, in list order, and the cursor.
    fn got(p: &Picked) -> (Vec<String>, Option<&str>) {
        (p.chosen(&ids()), p.cursor.as_deref())
    }

    fn want(selected: &[&str], cursor: &str) -> (Vec<String>, Option<&'static str>) {
        let cursor: &'static str = ["a", "b", "c", "d"]
            .into_iter()
            .find(|c| *c == cursor)
            .unwrap();
        (
            selected.iter().map(|s| s.to_string()).collect(),
            Some(cursor),
        )
    }

    #[test]
    fn arrows_move_the_cursor_and_shift_extends_from_the_anchor() {
        let ids = ids();
        let mut p = Picked::default();
        p.step(&ids, -1, false);
        assert_eq!(got(&p), want(&["d"], "d"), "up with no cursor: the last");
        let mut p = Picked::default();
        p.step(&ids, 1, false);
        assert_eq!(got(&p), want(&["a"], "a"), "down with no cursor: the first");
        p.step(&ids, -1, false);
        assert_eq!(got(&p), want(&["a"], "a"), "held at the top");
        p.step(&ids, 1, false);
        p.step(&ids, 1, true);
        p.step(&ids, 1, true);
        assert_eq!(got(&p), want(&["b", "c", "d"], "d"));
        p.step(&ids, -1, true);
        p.step(&ids, -1, true);
        p.step(&ids, -1, true);
        assert_eq!(got(&p), want(&["a", "b"], "a"), "back over the anchor");
        p.step(&ids, 1, false);
        assert_eq!(got(&p), want(&["b"], "b"), "a plain move collapses it");
    }

    #[test]
    fn clicks_pick_one_toggle_with_command_and_take_a_range_with_shift() {
        let ids = ids();
        let mut p = Picked::default();
        p.click(&ids, "c", false, true);
        assert_eq!(got(&p), want(&["c"], "c"), "shift with no anchor");
        p.click(&ids, "b", false, false);
        assert_eq!(got(&p), want(&["b"], "b"));
        p.click(&ids, "d", true, false);
        assert_eq!(got(&p), want(&["b", "d"], "d"));
        p.click(&ids, "b", true, false);
        assert_eq!(got(&p), want(&["d"], "b"), "toggled off, the cursor there");
        p.click(&ids, "a", false, true);
        assert_eq!(
            got(&p),
            want(&["a", "b"], "a"),
            "the range from the last click"
        );
    }

    #[test]
    fn all_keeps_the_cursor_and_a_removal_selects_the_next_note() {
        let ids = ids();
        let mut p = Picked::default();
        p.click(&ids, "c", false, false);
        p.all(&ids);
        assert_eq!(got(&p), want(&["a", "b", "c", "d"], "c"));
        p.prune(&ids[..2]);
        assert_eq!(p.chosen(&ids), ["a", "b"]);
        assert_eq!(p.cursor, None, "the cursor's note went");
        p.all(&ids);
        assert_eq!(p.cursor.as_deref(), Some("a"), "no cursor: the first");
        // The selection deleted: the note after the last one removed, else the one before.
        p.click(&ids, "b", false, false);
        p.click(&ids, "c", false, true);
        p.removed(&ids, &["b".into(), "c".into()]);
        assert_eq!(got(&p), want(&["d"], "d"));
        p.removed(&ids, &["d".into()]);
        assert_eq!(got(&p), want(&["c"], "c"));
        // A note not selected: the selection stays.
        p.removed(&ids, &["a".into()]);
        assert_eq!(got(&p), want(&["c"], "c"));
        p.removed(&ids, &ids);
        assert_eq!(p, Picked::default());
    }

    /// `e`: web's `selection.cursor ?? selectedNotes[0]`, nothing with nothing chosen.
    #[test]
    fn e_edits_the_cursor_chosen_or_not_else_the_first_chosen() {
        let ids = ids();
        let mut p = Picked::default();
        assert_eq!(p.editing(&ids), None);
        p.click(&ids, "a", false, false);
        p.click(&ids, "b", true, false);
        p.click(&ids, "b", true, false);
        assert_eq!(got(&p), want(&["a"], "b"));
        assert_eq!(p.editing(&ids).as_deref(), Some("b"));
        p.prune(&ids[..1]);
        assert_eq!(
            p.editing(&ids).as_deref(),
            Some("a"),
            "the cursor's note went"
        );
        p.click(&ids, "a", true, false);
        assert_eq!(p.editing(&ids), None, "nothing chosen");
    }

    #[test]
    fn copy_is_webs_hand_off_text_of_the_chosen_notes() {
        let web: serde_json::Value =
            serde_json::from_str(include_str!("../../testdata/notes-web.json")).unwrap();
        let rows: Vec<StateRow> = serde_json::from_value(web["rows"].clone()).unwrap();
        let mut store = loaded();
        let rows = StateRows {
            rows: rows[..2].to_vec(),
            rev: 1,
        };
        let step = Step::Pulled(rows);
        store.apply(Event::Sync {
            ns: Ns::Notes,
            step,
        });
        let notes: Vec<_> = store.notes_of("mupu").collect();
        let ids: Vec<String> = notes.iter().map(|n| n.id.clone()).collect();
        let mut p = Picked::default();
        assert_eq!(copied(&store, "mupu", &p), None);
        p.click(&ids, &ids[1], false, false);
        let one = (transfer_text(notes[1]), "Copied 1 note.".to_string());
        assert_eq!(copied(&store, "mupu", &p), Some(one));
        p.all(&ids);
        // Web's per-note texts are in row order; its list (and copy) is newest-updated first.
        let mut want: Vec<String> = serde_json::from_value(web["handoff"].clone()).unwrap();
        want.reverse();
        let both = (want.join("\n\n"), "Copied 2 notes.".to_string());
        assert_eq!(copied(&store, "mupu", &p), Some(both));
    }
}

/// F6: the notes list in a headless window, driven by real pointer and key events through the handlers
/// the zoom installs (`views::space`), so a nested control's click and the key capture are what run.
mod notes_events {
    use crate::api::{StateRow, StateRows};
    use crate::store::notes::Step as N;
    use crate::store::sync::{Ns, Step as Sync};
    use crate::store::tests::composer::zoomed;
    use crate::store::{Effect, Event, Store};
    use crate::views::lens::Ui;
    use crate::views::notes::{self, Notes};
    use crate::views::notes_list::{self, Card};
    use crate::views::space::Zoom;
    use crate::views::{Host, bind, composer, dock, on, theme};
    use gpui_kit::test::TestWindowExt as _;
    use gpui_kit::{
        Context, ElementId, Entity, InteractiveElement as _, IntoElement, Modifiers,
        ParentElement as _, Render, Styled as _, TestAppContext, VisualTestContext, Window, div,
    };
    use serde_json::json;

    struct Shell {
        store: Store,
        ui: Ui,
        copied: Vec<String>,
    }

    impl Host for Shell {
        fn parts(&mut self) -> (&Store, &mut Ui) {
            (&self.store, &mut self.ui)
        }

        fn view(&self) -> (&Store, &Ui) {
            (&self.store, &self.ui)
        }

        /// The store, and the one effect the list answers (as `shell::Shell::run` does).
        fn dispatch(&mut self, event: Event, cx: &mut Context<Self>) {
            for effect in self.store.apply(event) {
                if let Effect::HandedOff {
                    agent,
                    order,
                    removed,
                } = effect
                {
                    notes_list::handed_off(&mut self.ui, &agent, &order, &removed);
                }
            }
            cx.notify();
        }

        fn copy(&mut self, text: String, _: &mut Context<Self>) {
            self.copied.push(text);
        }
    }

    impl Render for Shell {
        fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
            let _ = dock::sync(&mut self.ui, &self.store, window, cx);
            composer::sync(&mut self.ui, &self.store, window, cx);
            notes::sync(&mut self.ui, &self.store, window, cx);
            let (store, ui, t) = (&self.store, &self.ui, theme::type_scale(1.));
            let panel = div().track_focus(&ui.panel().unwrap().focus).size_full();
            let panel = panel
                .flex()
                .flex_col()
                .children(notes::render(store, ui, "mupu", t, cx))
                .child(composer::render(store, ui, "mupu", t, cx));
            let zoom = div()
                .id("space")
                .key_context("Space")
                .track_focus(&ui.zoom_focus)
                .on_action(on(cx, |store, ui, n: &Notes| notes::act(store, ui, n)))
                .on_action(on(cx, |store, ui, c: &Card| notes_list::act(store, ui, c)))
                .size_full()
                .child(panel);
            div().size_full().key_context("Lens").child(zoom)
        }
    }

    /// A note on `group`, `updated` at `at` (the list is newest-updated first).
    fn row(id: &str, group: &str, at: i64) -> StateRow {
        let value = json!({"id": id, "group": group, "text": format!("note {id}"), "created": at});
        StateRow {
            key: id.into(),
            value,
            updated: at,
            write_id: format!("w-{id}"),
            deleted: false,
        }
    }

    fn pulled(shell: &mut Shell, rows: Vec<StateRow>, rev: u64, cx: &mut Context<Shell>) {
        let step = Sync::Pulled(StateRows { rows, rev });
        shell.dispatch(
            Event::Sync {
                ns: Ns::Notes,
                step,
            },
            cx,
        );
    }

    /// Zoomed on a writable mupu with notes `ids`, listed in that order.
    fn open<'a>(
        cx: &'a mut TestAppContext,
        ids: &[&str],
    ) -> (Entity<Shell>, &'a mut VisualTestContext) {
        cx.update(|cx| {
            theme::seed(cx);
            gpui_kit::init(cx);
            theme::dark(cx);
            bind(cx);
        });
        let rows: Vec<StateRow> = (ids.iter().rev().enumerate())
            .map(|(i, id)| row(id, "mupu", 1_000 + i as i64))
            .collect();
        let (shell, cx) = cx.add_window_view(move |_, cx| {
            let store = zoomed("mupu", Some("listening"));
            let mut ui = Ui::new(cx);
            let space = store.spaces[0].id.clone();
            ui.zoom = Some(Zoom {
                space,
                agent: Some("mupu".into()),
            });
            let mut shell = Shell {
                store,
                ui,
                copied: Vec::new(),
            };
            pulled(&mut shell, rows, 1, cx);
            shell
        });
        cx.update(|window, cx| window.render_frame(cx));
        (shell, cx)
    }

    fn id(name: String) -> ElementId {
        ElementId::Name(name.into())
    }

    /// A real click (down and up through hit testing) on note `n`'s card, with `modifiers` held.
    fn click(cx: &mut VisualTestContext, n: &str, modifiers: Modifiers) {
        cx.update(|window, cx| window.render_frame(cx));
        let at = cx.update(|window, _| window.find(id(format!("note-{n}"))).bounds());
        cx.simulate_click(
            at.origin + gpui_kit::point(gpui_kit::px(20.), gpui_kit::px(8.)),
            modifiers,
        );
        cx.run_until_parked();
    }

    /// The list's selection in list order, its cursor, and the strip's line.
    fn state(
        shell: &Entity<Shell>,
        cx: &mut VisualTestContext,
    ) -> (Vec<String>, Option<String>, Option<String>) {
        shell.read_with(cx, |s, _| {
            let ids = notes::ids(&s.store, "mupu");
            let picked = &s.ui.panel().unwrap().notes.list.picked;
            (
                picked.chosen(&ids),
                picked.cursor.clone(),
                s.ui.panel().unwrap().notes.said(),
            )
        })
    }

    fn texts(shell: &Entity<Shell>, cx: &mut VisualTestContext) -> Vec<String> {
        shell.read_with(cx, |s, _| {
            s.store.notes_of("mupu").map(|n| n.text.clone()).collect()
        })
    }

    #[gpui_kit::test]
    fn a_click_on_a_notes_x_arms_it_and_the_second_deletes_it_without_picking_the_card(
        cx: &mut TestAppContext,
    ) {
        let (shell, cx) = open(cx, &["a", "b"]);
        click(cx, "b", Modifiers::none());
        cx.update(|window, cx| window.click(id("del-a".into()), cx));
        cx.run_until_parked();
        let armed = Some("⌫ again to delete 1 note".to_string());
        assert_eq!(
            state(&shell, cx),
            (vec!["b".into()], Some("b".into()), armed),
            "the card did not pick"
        );
        cx.update(|window, cx| window.click(id("del-a".into()), cx));
        cx.run_until_parked();
        assert_eq!(texts(&shell, cx), ["note b"]);
        assert_eq!(state(&shell, cx).2.as_deref(), Some("Deleted 1 note."));
    }

    #[gpui_kit::test]
    fn clicks_in_the_card_editor_stay_in_it_and_it_outlives_a_remote_delete(
        cx: &mut TestAppContext,
    ) {
        let (shell, cx) = open(cx, &["a", "b"]);
        cx.update(|window, cx| window.double_click(id("note-a".into()), cx));
        cx.run_until_parked();
        let editing = |cx: &mut VisualTestContext| {
            cx.update(|window, cx| {
                let s = shell.read(cx);
                let focused =
                    s.ui.panel()
                        .unwrap()
                        .notes
                        .focus_handle(cx)
                        .is_focused(window);
                (
                    s.ui.panel().unwrap().notes.editing.is_some(),
                    focused,
                    s.ui.panel().unwrap().notes.text.clone(),
                )
            })
        };
        assert_eq!(editing(cx), (true, true, "note a".into()));
        cx.update(|window, cx| window.press("cmd-a", cx));
        cx.update(|window, cx| window.input("note a more", cx));
        // A click, then a double-click, in the editor: no pick takes focus, no reopen reloads the text.
        cx.update(|window, cx| window.click(id("note-editor".into()), cx));
        cx.update(|window, cx| window.double_click(id("note-editor".into()), cx));
        cx.run_until_parked();
        assert_eq!(editing(cx), (true, true, "note a more".into()));
        // Web deletes it meanwhile: the editor stays, and a save writes it again, newer than the tombstone.
        let mut gone = row("a", "mupu", 9_000);
        (gone.value, gone.deleted) = (json!({"id": "a"}), true);
        shell.update(cx, |s, cx| pulled(s, vec![gone], 2, cx));
        cx.update(|window, cx| window.render_frame(cx));
        assert_eq!(texts(&shell, cx), ["note b"]);
        assert!(cx.update(|window, _| window.try_find(id("note-editor".into())).is_some()));
        assert_eq!(editing(cx), (true, true, "note a more".into()));
        cx.update(|window, cx| window.press("enter", cx));
        cx.run_until_parked();
        assert_eq!(texts(&shell, cx), ["note a more", "note b"]);
        let row = shell.read_with(cx, |s, _| s.store.sync[&Ns::Notes].rows["a"].clone());
        assert!(!row.deleted && row.updated > 9_000);
    }

    #[gpui_kit::test]
    fn the_card_editor_outlives_its_note_moving_to_another_agent(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx, &["a", "b"]);
        cx.update(|window, cx| window.double_click(id("note-b".into()), cx));
        cx.run_until_parked();
        cx.update(|window, cx| window.press("cmd-a", cx));
        cx.update(|window, cx| window.input("note b kept", cx));
        shell.update(cx, |s, cx| {
            pulled(s, vec![row("b", "orch-lega", 9_000)], 2, cx)
        });
        cx.update(|window, cx| window.render_frame(cx));
        assert_eq!(texts(&shell, cx), ["note a"]);
        assert!(cx.update(|window, _| window.try_find(id("note-editor".into())).is_some()));
        cx.update(|window, cx| window.press("enter", cx));
        cx.run_until_parked();
        let saved = shell.read_with(cx, |s, _| {
            s.store
                .notes
                .iter()
                .find(|n| n.id == "b")
                .map(|n| n.text.clone())
        });
        assert_eq!(saved.as_deref(), Some("note b kept"));
    }

    #[gpui_kit::test]
    fn any_other_key_a_copy_or_focus_leaving_disarms_a_delete(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx, &["a", "b"]);
        click(cx, "a", Modifiers::none());
        let press = |cx: &mut VisualTestContext, key: &str| {
            cx.update(|window, cx| window.press(key, cx));
            cx.run_until_parked();
        };
        let armed = |shell: &Entity<Shell>, cx: &mut VisualTestContext| {
            state(shell, cx).2.is_some_and(|s| s.starts_with("⌫ again"))
        };
        for between in ["cmd-c", "x", "down"] {
            click(cx, "a", Modifiers::none());
            press(cx, "backspace");
            assert!(armed(&shell, cx), "armed before `{between}`");
            press(cx, between);
            assert!(!armed(&shell, cx), "`{between}` disarms");
            click(cx, "a", Modifiers::none());
            press(cx, "backspace");
            assert_eq!(
                texts(&shell, cx),
                ["note a", "note b"],
                "after `{between}`, a fresh confirmation"
            );
        }
        assert_eq!(shell.read_with(cx, |s, _| s.copied.clone()), ["note a"]);
        // Focus leaving the list disarms too.
        cx.update(|window, cx| {
            let box_ = shell.read(cx).ui.panel().unwrap().composer.focus_handle(cx);
            window.focus(&box_, cx);
            window.render_frame(cx);
        });
        assert!(!armed(&shell, cx), "focus left");
        click(cx, "a", Modifiers::none());
        press(cx, "backspace");
        press(cx, "backspace");
        assert_eq!(texts(&shell, cx), ["note b"]);
    }

    #[gpui_kit::test]
    fn e_edits_the_cursors_note_even_when_it_is_not_chosen(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx, &["a", "b"]);
        click(cx, "a", Modifiers::none());
        click(cx, "b", Modifiers::command());
        click(cx, "b", Modifiers::command());
        assert_eq!(state(&shell, cx).0, ["a"]);
        assert_eq!(state(&shell, cx).1.as_deref(), Some("b"));
        cx.update(|window, cx| window.press("e", cx));
        cx.run_until_parked();
        let edited = shell.read_with(cx, |s, _| {
            let editing = s.ui.panel().unwrap().notes.editing.as_ref();
            editing.and_then(|e| e.note.as_ref()).map(|n| n.id.clone())
        });
        assert_eq!(edited.as_deref(), Some("b"));
    }

    #[gpui_kit::test]
    fn a_partial_hand_off_selects_the_next_note_once_it_lands_and_not_when_it_fails(
        cx: &mut TestAppContext,
    ) {
        let (shell, cx) = open(cx, &["a", "b", "c", "d"]);
        shell.update(cx, |s, _| s.ui.panel_mut().unwrap().notes.open = true);
        let hand_off = |cx: &mut VisualTestContext, saved: Result<(), String>| {
            click(cx, "b", Modifiers::none());
            cx.update(|window, cx| window.press("enter", cx));
            cx.run_until_parked();
            let landed = N::Landed {
                agent: "mupu".into(),
                saved,
            };
            shell.update(cx, |s, cx| s.dispatch(Event::Note(landed), cx));
            cx.update(|window, cx| window.render_frame(cx));
        };
        hand_off(cx, Err("disk full".into()));
        assert_eq!(texts(&shell, cx).len(), 4, "a failed save keeps them");
        assert_eq!(&state(&shell, cx).0, &["b".to_string()]);
        shell.update(cx, |s, _| s.store.prefs.drafts.clear());
        hand_off(cx, Ok(()));
        assert_eq!(texts(&shell, cx), ["note a", "note c", "note d"]);
        let (chosen, cursor, _) = state(&shell, cx);
        assert_eq!(
            (chosen, cursor.as_deref()),
            (vec!["c".to_string()], Some("c"))
        );
        // Back into the list from the emptied box: the cursor is still on c.
        shell.update(cx, |s, _| s.store.prefs.drafts.clear());
        cx.update(|window, cx| {
            let box_ = shell.read(cx).ui.panel().unwrap().composer.focus_handle(cx);
            window.focus(&box_, cx);
        });
        cx.update(|window, cx| window.press("up", cx));
        cx.run_until_parked();
        assert_eq!(state(&shell, cx).1.as_deref(), Some("c"));
    }
}

/// F2: where regrouped rows splice into the list, and which runs stay open as pages land.
mod runs {
    use crate::store::Store;
    use crate::store::condense::{self, Row};
    use crate::store::tests::loaded;
    use crate::store::tests::transcript_pages::{drive, history, items, open, reference, wake};
    use crate::store::transcript::Key;
    use crate::views::transcript::{View, plan};

    /// The rows of `agent`'s history from `a` to `b`, read whole.
    fn rows(agent: &str, a: usize, b: usize) -> Vec<Row> {
        condense::rows(&reference(agent, &history(agent)[a..b]))
    }

    fn fits(old: &[Row], new: &[Row]) -> (usize, usize) {
        let (before, after) =
            plan(old, new).unwrap_or_else(|| panic!("a splice: {old:?} into {new:?}"));
        assert_eq!(before + old.len() + after, new.len());
        let m = old.len();
        if m > 1 {
            assert_eq!(
                old[1..m - 1],
                new[before + 1..before + m - 1],
                "rows between unchanged"
            );
        }
        (before, after)
    }

    #[test]
    fn earlier_pages_splice_in_before_even_when_they_join_a_run() {
        let mut store = loaded();
        let all = history("mupu");
        let effects = open(&mut store, "mupu");
        drive(&mut store, effects, &all, 7);
        let mut joined = 0;
        loop {
            let old = condense::rows(items(&store));
            let effects = store.apply(crate::store::tests::older(&store));
            if effects.is_empty() {
                break;
            }
            drive(&mut store, effects, &all, 7);
            let new = condense::rows(items(&store));
            let (before, after) = fits(&old, &new);
            assert_eq!(after, 0, "a page before adds nothing after");
            joined += usize::from(new[before] != old[0]);
        }
        assert!(joined > 0, "some page joined the run at the top");
    }

    #[test]
    fn live_entries_splice_in_after_and_both_ends_at_once() {
        let n = history("mupu").len();
        for b in n - 12..n {
            let (before, _) = fits(&rows("mupu", 10, b), &rows("mupu", 10, b + 1));
            assert_eq!(before, 0, "an entry after adds nothing before");
            fits(&rows("mupu", 10, b), &rows("mupu", 7, b + 1));
        }
    }

    #[test]
    fn a_row_in_the_middle_resets() {
        let new = rows("mupu", 0, history("mupu").len());
        let mut old = new.clone();
        old.remove(new.len() / 2);
        assert_eq!(plan(&old, &new), None);
        assert_eq!(plan(&[], &new), None);
    }

    fn sync(view: &View, store: &Store) {
        view.sync(store.transcript.focused().unwrap(), store);
    }

    /// The first and last runs among the view's rows, with their row indices.
    fn ends(store: &Store) -> [((Key, Key), usize); 2] {
        ends_of(&condense::rows(items(store))).unwrap()
    }

    fn ends_of(rows: &[Row]) -> Option<[((Key, Key), usize); 2]> {
        let mut runs = rows.iter().enumerate().filter_map(|(ix, r)| match *r {
            Row::Run(first, last) => Some(((first, last), ix)),
            Row::One(_) => None,
        });
        let top = runs.next()?;
        Some([top, runs.next_back().unwrap_or(top)])
    }

    #[test]
    fn open_runs_stay_open_as_they_grow_and_close_whole() {
        let all = history("mupu");
        // Cut the live tail inside mupu's last run, so a wake grows it.
        let whole = rows("mupu", 0, all.len());
        let last = whole.last().unwrap();
        assert!(matches!(last, Row::Run(..)), "mupu ends in a run");
        let cut = (1..all.len())
            .rev()
            .find(|&b| {
                all[b].byte_offset > last.first().0 && all[b - 1].byte_offset >= last.first().0
            })
            .unwrap();
        // A page size whose first page holds two runs, the top one cut short.
        let split = |a: usize| {
            let key = (all[a].byte_offset, 0);
            let run = |r: &&Row| matches!(r, Row::Run(f, l) if *f < key && key <= *l);
            whole.iter().any(|r| run(&r))
        };
        let two = |a: usize| ends_of(&rows("mupu", a, cut)).is_some_and(|[t, b]| t != b);
        let page = (20..cut).find(|&n| split(cut - n) && two(cut - n)).unwrap();
        let mut store = loaded();
        let effects = open(&mut store, "mupu");
        drive(&mut store, effects, &all[..cut], page);
        let view = View::default();
        sync(&view, &store);
        let [top, bottom] = ends(&store);
        assert_ne!(top, bottom, "two runs on the first page");
        for (run, ix) in [top, bottom] {
            view.toggle(run, ix);
        }
        assert_eq!(view.census().2, 2, "{:?}", view.census());
        // Pages before join the top run, the wake grows the bottom one.
        let mut joined = false;
        loop {
            let effects = store.apply(crate::store::tests::older(&store));
            if effects.is_empty() {
                break;
            }
            drive(&mut store, effects, &all, page);
            sync(&view, &store);
            assert_eq!(view.census().2, 2, "still open after a page before");
            let first = ends(&store)[0].0.0;
            joined |= first < (top.0).0
                && condense::rows(items(&store)).first() == Some(&Row::Run(first, (top.0).1));
        }
        assert!(joined, "a page before joined the open top run");
        let effects = store.apply(wake(&store, "mupu"));
        drive(&mut store, effects, &all, page);
        sync(&view, &store);
        assert_eq!(items(&store), &reference("mupu", &all), "read to both ends");
        let (rows, runs, open) = view.census();
        assert_eq!((rows, open), (whole.len(), 2), "{runs} runs");
        // Close the grown runs that hold the keys opened.
        let now = condense::rows(items(&store));
        for key in [(top.0).0, (bottom.0).0] {
            let ix = now
                .iter()
                .position(|r| r.first() <= key && key <= r.last())
                .unwrap();
            view.toggle((now[ix].first(), now[ix].last()), ix);
        }
        assert_eq!(view.census().2, 0, "a closed run holds no open key");
    }

    #[test]
    fn ages_round_in_webs_units() {
        let ago = crate::views::transcript::ago;
        let cases = [
            (0, "1s"),
            (59, "59s"),
            (60, "1m"),
            (89, "1m"),
            (90, "2m"),
            (3599, "60m"),
        ];
        let cases = cases
            .into_iter()
            .chain([(3600, "1h"), (86_399, "24h"), (86_400, "1d")]);
        for (secs, want) in cases {
            assert_eq!(ago(secs), want, "{secs}s");
        }
    }
}

/// A2: answers' parts, entry headers and cards.
mod entries {
    use crate::api::AgentDetail;
    use crate::store::Store;
    use crate::store::condense::{self, Seg};
    use crate::store::tests::loaded;
    use crate::store::tests::transcript_pages::{drive, history, items, open};
    use crate::store::transcript::{Item, Key};
    use crate::views::entries::{Bit, Card, bits, queued_age, stamp, waited};
    use crate::views::transcript::{Fold, View, long};

    /// The last answer holding a part `is` picks, and that part.
    fn part(store: &Store, is: fn(&Seg) -> bool) -> Option<Fold> {
        let answers = items(store)
            .iter()
            .rev()
            .filter_map(|(&key, item)| match item {
                Item::Assistant(segs) => Some((key, segs.iter().position(is)?)),
                _ => None,
            });
        answers.map(|(key, at)| Fold(key, at)).next()
    }

    fn row_of(store: &Store, key: Key) -> usize {
        let rows = condense::rows(items(store));
        rows.partition_point(|r| r.last() < key)
    }

    #[test]
    fn an_open_status_chip_and_note_stay_open_as_pages_regroup_the_rows() {
        let all = history("mupu");
        let mut store = loaded();
        let effects = open(&mut store, "mupu");
        drive(&mut store, effects, &all, 7);
        let view = View::default();
        let status = |s: &Seg| matches!(s, Seg::Status(s) if long(s));
        let note = |s: &Seg| matches!(s, Seg::Internal(_));
        let (mut opened, mut was) = (Vec::new(), Vec::new());
        let mut regrouped = false;
        loop {
            view.sync(store.transcript.focused().unwrap(), &store);
            for is in [status as fn(&Seg) -> bool, note] {
                // The last such answer: pages before only add earlier ones.
                if let Some(fold) = part(&store, is).filter(|f| !opened.contains(f)) {
                    view.fold(fold);
                    opened.push(fold);
                    was.push(row_of(&store, fold.0));
                }
            }
            let want = (
                usize::from(!opened.is_empty()),
                usize::from(opened.len() > 1),
            );
            assert_eq!(
                view.parts(items(&store)),
                want,
                "still open after a page before"
            );
            for (fold, at) in opened.iter().zip(&was) {
                regrouped |= row_of(&store, fold.0) != *at;
            }
            let effects = store.apply(crate::store::tests::older(&store));
            if effects.is_empty() {
                break;
            }
            drive(&mut store, effects, &all, 7);
        }
        assert_eq!(
            opened.len(),
            2,
            "mupu has a cut status and an internal note"
        );
        assert!(regrouped, "pages before moved the answers' rows");
        for fold in opened {
            view.fold(fold);
        }
        assert_eq!(view.parts(items(&store)), (0, 0), "a second click closes");
    }

    #[test]
    fn headers_say_who_and_when_as_web() {
        let cases = [(None, "time unknown"), (Some(1000), "1s ago")];
        for (at, want) in cases {
            assert_eq!(stamp(at, 1000), want);
        }
        assert_eq!(stamp(Some(100_000 - 12_700), 100_000), "4h ago");
        let waits = [
            (0, "0s ago"),
            (59, "59s ago"),
            (119, "1m ago"),
            (3599, "59m ago"),
        ];
        for (secs, want) in waits.into_iter().chain([(7300, "2h ago")]) {
            assert_eq!(waited(secs), want, "{secs}s");
        }
        // A queued message's `sent_at` as hcom writes it (`hcomevents` keeps its `ts`), and as Z: one age.
        let detail: AgentDetail = serde_json::from_str(
            r#"{"name": "mupu", "queued": [
                {"id": 1, "sender": "kona", "preview": "a", "sent_at": "2026-09-30T00:07:16.868123+00:00"},
                {"id": 2, "sender": "kona", "preview": "b", "sent_at": "2026-09-30T00:07:16.868Z"}]}"#,
        )
        .unwrap();
        let now = condense::epoch("2026-09-30T00:08:51Z").unwrap();
        let ages: Vec<String> = detail
            .queued
            .unwrap()
            .iter()
            .map(|q| queued_age(&q.sent_at, now))
            .collect();
        assert_eq!(ages, ["1m ago", "1m ago"]);
        assert_eq!(
            queued_age("yesterday", now),
            "yesterday",
            "as sent when it does not parse"
        );
        assert_eq!(condense::group(219_914), "219,914");
        assert_eq!(condense::group(5998), "5,998");
        assert_eq!(condense::group(999), "999");
        assert_eq!(condense::group(1_000_000), "1,000,000");
        // A fixture's operator note and another agent's message, as web's card headers.
        let all: Vec<Item> = ["mupu", "grill-confirm-lubo", "conductor-line", "riko"]
            .into_iter()
            .flat_map(history)
            .flat_map(|e| condense::condense(&e))
            .collect();
        let delivery = |operator: bool| {
            let mut found = all.iter().filter(|i| {
                matches!(i, Item::Delivery { operator: o, head, .. } if *o == operator && !head.thread.is_empty())
            });
            found
                .next()
                .unwrap_or_else(|| panic!("a delivery, operator {operator}, in a thread"))
        };
        let names = |bits: Vec<Bit>| {
            bits.into_iter()
                .map(|b| format!("{b:?}").split('(').next().unwrap().to_string())
                .collect::<Vec<_>>()
        };
        let plain = bits(delivery(false), "mupu");
        assert_eq!(names(plain), ["Name", "To", "Intent", "Id", "Thread"]);
        let answer = Item::Assistant(vec![Seg::Text("hi".into())]);
        assert_eq!(bits(&answer, "mupu"), [Bit::Name("mupu".into())]);
        let prompt = Item::Prompt("hi".into());
        assert_eq!(
            bits(&prompt, "mupu"),
            [Bit::Name("owner (terminal)".into())]
        );
        let operator = all
            .iter()
            .find(|i| matches!(i, Item::Delivery { operator: true, .. }));
        let operator = bits(operator.expect("an operator note"), "mupu");
        assert_eq!(operator[1], Bit::Operator, "{operator:?}");
    }

    #[test]
    fn cards_are_web_s_three_kinds() {
        let delivery = |operator: bool| Item::Delivery {
            sender: "kona".into(),
            text: "hi".into(),
            operator,
            head: Box::default(),
        };
        assert_eq!(Card::of(&delivery(false)), Some(Card::Hcom));
        assert_eq!(Card::of(&delivery(true)), Some(Card::Operator));
        assert_eq!(Card::of(&Item::Prompt("hi".into())), Some(Card::Human));
        let none = [
            Item::Assistant(vec![Seg::Text("hi".into())]),
            Item::SystemChip("model switched".into()),
            Item::CompactDivider("context compacted".into()),
            Item::CompactSummary("summary".into()),
        ];
        assert!(none.iter().all(|i| Card::of(i).is_none()));
        // An unnamed sender and recipient read as web's.
        let bits = bits(&delivery(false), "mupu");
        assert_eq!(
            bits,
            [
                Bit::Name("kona".into()),
                Bit::To("unknown recipient".into())
            ]
        );
    }
}

/// F2 review: the real transcript body laid out headless. A page that grows the run at the viewport's
/// top leaves what is read where it was; `o` toggles only a run on screen.
mod layout {
    use crate::api::types::Entry;
    use crate::store::condense::{self, Row};
    use crate::store::spaces::Move;
    use crate::store::tests::loaded;
    use crate::store::tests::transcript_pages::{self, drive, history, items, open, reference};
    use crate::store::tests::{board, bump, frame};
    use crate::store::transcript::{Item, Key};
    use crate::store::{Effect, Event, Store};
    use crate::views::lens::Ui;
    use crate::views::space::Zoom;
    use crate::views::transcript::{self, Fold, Mark, OpenLink};
    use crate::views::{Host, dock, theme};
    use gpui_kit::base::ScrollbarHandle;
    use gpui_kit::test::TestWindowExt as _;
    use gpui_kit::{
        Bounds, Context, ElementId, Entity, IntoElement, ListOffset, Modifiers, ParentElement as _,
        Pixels, Render, ScrollDelta, ScrollWheelEvent, Styled as _, TestAppContext, TouchPhase,
        VisualTestContext, Window, div, point, px, size,
    };

    struct Body {
        store: Store,
        ui: Ui,
        /// Reduce what the view dispatches, as the shell does (else the test reads the pages itself),
        /// keeping the effects for the test to answer.
        reduce: bool,
        effects: Vec<Effect>,
        /// The links clicked through to the shell (`OpenLink`).
        links: Vec<String>,
    }

    impl Host for Body {
        fn parts(&mut self) -> (&Store, &mut Ui) {
            (&self.store, &mut self.ui)
        }

        fn view(&self) -> (&Store, &Ui) {
            (&self.store, &self.ui)
        }

        fn dispatch(&mut self, event: Event, cx: &mut Context<Self>) {
            // The dock keeping its layout is not what these read.
            if self.reduce && !matches!(event, Event::Layout { .. }) {
                let effects = transcript::reduce(&mut self.store, &self.ui, event);
                self.effects.extend(effects);
                cx.notify();
            }
        }

        fn copy(&mut self, _: String, _: &mut Context<Self>) {}
    }

    impl Render for Body {
        fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
            let _ = dock::sync(&mut self.ui, &self.store, window, cx);
            let zoom = self.ui.zoom.clone().unwrap();
            let (agent, t) = (zoom.agent.unwrap(), theme::type_scale(1.));
            let body = transcript::render(&self.store, &self.ui, &agent, t, cx);
            let open = cx.listener(|b, link: &OpenLink, _, _| b.links.push(link.0.to_string()));
            use gpui_kit::InteractiveElement as _;
            div()
                .size_full()
                .flex()
                .flex_col()
                .on_action(open)
                // The window's selection layer, as the app's root (the kit's `Root`) draws it.
                .child(gpui_kit::base::TextSelectionLayer)
                .child(body)
        }
    }

    /// `agent`'s first `served` entries, its tail read `limit` entries a page, in a window this size.
    fn body<'a>(
        cx: &'a mut TestAppContext,
        agent: &'static str,
        (served, limit): (usize, usize),
        (width, height): (f32, f32),
    ) -> (Entity<Body>, &'a mut VisualTestContext) {
        cx.update(|cx| {
            theme::seed(cx);
            gpui_kit::init(cx);
            theme::dark(cx);
        });
        let mut store = loaded();
        let effects = open(&mut store, agent);
        drive(&mut store, effects, served_of(agent, served), limit);
        let (body, cx) = cx.add_window_view(move |_, cx| {
            let mut ui = Ui::new(cx);
            let (space, agent) = ("none".into(), Some(agent.into()));
            ui.zoom = Some(Zoom { space, agent });
            Body {
                store,
                ui,
                reduce: false,
                effects: Vec::new(),
                links: Vec::new(),
            }
        });
        cx.simulate_resize(size(px(width), px(height)));
        draw(cx);
        (body, cx)
    }

    fn served_of(agent: &str, served: usize) -> &'static [Entry] {
        let all = Box::leak(history(agent).into_boxed_slice());
        &all[..served.min(all.len())]
    }

    fn draw(cx: &mut VisualTestContext) {
        cx.run_until_parked();
        cx.update(|window, cx| window.render_frame(cx));
    }

    fn rows(body: &Entity<Body>, cx: &mut VisualTestContext) -> Vec<Row> {
        body.read_with(cx, |b, _| condense::rows(items(&b.store)))
    }

    fn row(body: &Entity<Body>, ix: usize, cx: &mut VisualTestContext) -> Bounds<Pixels> {
        body.read_with(cx, |b, _| {
            b.ui.panel().unwrap().transcript.painted.borrow().rows[&ix]
        })
    }

    /// Where `was` (a member, or a pill holding its key) last laid out.
    fn mark(body: &Entity<Body>, was: Mark, cx: &mut VisualTestContext) -> Option<Bounds<Pixels>> {
        body.read_with(cx, |b, _| {
            let painted = b.ui.panel().unwrap().transcript.painted.borrow();
            painted
                .marks
                .iter()
                .find(|(m, _)| m.is(was))
                .map(|(_, b)| *b)
        })
    }

    fn screen(body: &Entity<Body>, cx: &mut VisualTestContext) -> Bounds<Pixels> {
        body.read_with(cx, |b, _| {
            b.ui.panel().unwrap().transcript.list.viewport_bounds()
        })
    }

    /// Read the page before and then, when `wake`, what the served file has after; then lay out once.
    fn older(
        body: &Entity<Body>,
        agent: &str,
        (served, limit): (usize, usize),
        wake: bool,
        cx: &mut VisualTestContext,
    ) {
        body.update(cx, |b, cx| {
            let all = served_of(agent, served);
            let effects = b.store.apply(crate::store::tests::older(&b.store));
            drive(&mut b.store, effects, all, limit);
            if wake {
                let effects = b.store.apply(transcript_pages::wake(&b.store, agent));
                drive(&mut b.store, effects, all, limit);
            }
            cx.notify();
        });
        draw(cx);
    }

    /// The rows of `all[a..b]` read whole, and how many members each run has.
    fn window(agent: &str, all: &[Entry], (a, b): (usize, usize)) -> (Vec<Row>, Vec<usize>) {
        let items = reference(agent, &all[a..b]);
        let rows = condense::rows(&items);
        let size = |r: &Row| items.range(r.first()..=r.last()).count();
        let sizes = rows.iter().map(size).collect();
        (rows, sizes)
    }

    /// A page size whose tail is several rows, the first a run that the page before grows.
    fn split(agent: &str) -> usize {
        let all = history(agent);
        let whole = condense::rows(&reference(agent, &all));
        let inside = |n: usize| {
            let key = (all[all.len() - n].byte_offset, 0);
            whole
                .iter()
                .any(|r| matches!(*r, Row::Run(f, l) if f < key && key <= l))
        };
        // Its top run has a strip that wraps, and rows enough to scroll.
        let fits = |n: usize| {
            let (rows, sizes) = window(agent, &all, (all.len() - n, all.len()));
            rows.len() >= 8 && matches!(rows[0], Row::Run(..)) && sizes[0] >= 6
        };
        (20..all.len()).find(|&n| inside(n) && fits(n)).unwrap()
    }

    /// Scroll the top row so the viewport's top is `y` into it.
    fn scroll(body: &Entity<Body>, y: Pixels, cx: &mut VisualTestContext) {
        body.read_with(cx, |b, _| {
            let list = &b.ui.panel().unwrap().transcript.list;
            list.scroll_to(ListOffset {
                item_ix: 0,
                offset_in_item: y,
            });
        });
        draw(cx);
    }

    /// Open the top run and put the viewport's top 10px above a member: the third, or the latest of
    /// the first three the list can scroll to. Returns that member and its y.
    fn read_third(body: &Entity<Body>, cx: &mut VisualTestContext) -> (Mark, Pixels) {
        let Row::Run(first, last) = rows(body, cx)[0] else {
            panic!("the top row is a run")
        };
        body.read_with(cx, |b, _| {
            b.ui.panel().unwrap().transcript.toggle((first, last), 0)
        });
        scroll(body, px(0.), cx);
        let keys: Vec<Key> = body.read_with(cx, |b, _| {
            items(&b.store)
                .range(first..=last)
                .map(|(k, _)| *k)
                .collect()
        });
        let most = body.read_with(cx, |b, _| {
            b.ui.panel()
                .unwrap()
                .transcript
                .list
                .max_offset_for_scrollbar()
                .y
        });
        let top = row(body, 0, cx).top();
        let mut reach = Vec::new();
        for &key in keys.iter().take(3) {
            let y = mark(body, Mark::Member(key), cx).unwrap().top() - top;
            reach.extend((y - px(10.) <= most).then_some((key, y)));
        }
        let (key, y) = *reach.last().expect("a member the list scrolls to");
        let read = Mark::Member(key);
        scroll(body, y - px(10.), cx);
        let was = mark(body, read, cx).unwrap().top();
        let top = screen(body, cx).top();
        assert!(
            (was - top - px(10.)).abs() < px(0.5),
            "scrolled to it: {was:?}"
        );
        (read, was)
    }

    fn held(body: &Entity<Body>, read: Mark, was: Pixels, cx: &mut VisualTestContext) {
        let now = mark(body, read, cx).expect("still laid out").top();
        assert!(
            (now - was).abs() < px(0.5),
            "{read:?} moved from {was:?} to {now:?}"
        );
    }

    #[gpui_kit::test]
    fn a_page_growing_the_open_top_run_leaves_its_members_in_place(cx: &mut TestAppContext) {
        let limit = split("mupu");
        let (body, cx) = body(cx, "mupu", (usize::MAX, limit), (420., 320.));
        let Row::Run(first, last) = rows(&body, cx)[0] else {
            panic!("the top row is a run")
        };
        let (read, was) = read_third(&body, cx);
        older(&body, "mupu", (usize::MAX, limit), false, cx);
        let grown = rows(&body, cx);
        assert!(
            grown
                .iter()
                .skip(1)
                .any(|r| matches!(*r, Row::Run(f, l) if f < first && l == last)),
            "rows went in above the grown run"
        );
        held(&body, read, was, cx);
    }

    #[gpui_kit::test]
    fn a_page_growing_only_the_open_top_run_leaves_its_members_in_place(cx: &mut TestAppContext) {
        // The page before adds members to the top run and no row: `before` is 0. With rows under
        // the run to scroll by.
        let only = |agent: &'static str| {
            let all = history(agent);
            let n = all.len();
            let fits = |k: usize| {
                let (old, sizes) = window(agent, &all, (n - k, n));
                let (new, _) = window(agent, &all, (n - 2 * k, n));
                let head =
                    matches!((old[0], new[0]), (Row::Run(f, l), Row::Run(g, m)) if g < f && m == l);
                head && sizes[0] >= 3
                    && old.len() >= 5
                    && old.len() == new.len()
                    && old[1..] == new[1..]
            };
            (3..n / 2).find(|&k| fits(k)).map(|k| (agent, k))
        };
        let agents = ["mupu", "conductor-line", "grill-confirm-lubo"];
        let (agent, limit) = agents
            .into_iter()
            .find_map(only)
            .expect("a page that only grows the top run");
        let (body, cx) = body(cx, agent, (usize::MAX, limit), (420., 160.));
        let (read, was) = read_third(&body, cx);
        let count = rows(&body, cx).len();
        older(&body, agent, (usize::MAX, limit), false, cx);
        assert_eq!(rows(&body, cx).len(), count, "no row before");
        held(&body, read, was, cx);
    }

    #[gpui_kit::test]
    fn a_run_grown_at_both_ends_at_once_leaves_its_members_in_place(cx: &mut TestAppContext) {
        // The list is one open run; a page before and a live entry both grow it before one layout.
        let all = history("mupu");
        let n = all.len();
        let both = |(b, k): (usize, usize)| {
            let (old, sizes) = window("mupu", &all, (b - k, b));
            let (new, _) = window("mupu", &all, (b.saturating_sub(2 * k), (b + 1).min(n)));
            let [Row::Run(f, l)] = old[..] else {
                return false;
            };
            sizes[0] >= 6
                && new
                    .iter()
                    .any(|r| matches!(*r, Row::Run(g, m) if g < f && m > l))
        };
        let pairs = (2 * 3..n).flat_map(|b| (3..b / 2).map(move |k| (b, k)));
        let (b, k) = pairs
            .filter(|&(b, _)| b < n)
            .find(|&p| both(p))
            .expect("one run, grown at both ends");
        let (body, cx) = body(cx, "mupu", (b, k), (420., 90.));
        let (read, was) = read_third(&body, cx);
        older(&body, "mupu", (b + 1, k), true, cx);
        let grown = rows(&body, cx);
        let (f, l) = match read {
            Mark::Member(key) => (key, key),
            Mark::Pill(..) => unreachable!(),
        };
        assert!(
            grown
                .iter()
                .any(|r| r.first() < f && r.last() > l && matches!(r, Row::Run(..))),
            "the run grew at both ends: {grown:?}"
        );
        held(&body, read, was, cx);
    }

    #[gpui_kit::test]
    fn a_page_growing_the_closed_top_strip_leaves_the_pill_read_in_place(cx: &mut TestAppContext) {
        let limit = split("mupu");
        let (body, cx) = body(cx, "mupu", (usize::MAX, limit), (420., 320.));
        let Row::Run(first, last) = rows(&body, cx)[0] else {
            panic!("the top row is a run")
        };
        scroll(&body, px(0.), cx);
        // The first pill on the strip's last line (what the viewport's top reads, of a line), the
        // viewport's top just above it.
        let pills = body.read_with(cx, |b, _| {
            b.ui.panel()
                .unwrap()
                .transcript
                .painted
                .borrow()
                .marks
                .clone()
        });
        let inside = |m: &Mark| matches!(*m, Mark::Pill(f, l) if first <= f && l <= last);
        let pills: Vec<_> = pills.into_iter().filter(|(m, _)| inside(m)).collect();
        let (read, b) = *pills
            .iter()
            .max_by(|a, b| {
                (a.1.top(), b.1.left())
                    .partial_cmp(&(b.1.top(), a.1.left()))
                    .unwrap()
            })
            .unwrap();
        let line = pills
            .iter()
            .map(|(_, b)| b.top())
            .fold(b.top(), Pixels::min);
        assert!(b.top() > line, "the strip wraps");
        scroll(&body, b.top() - row(&body, 0, cx).top() - px(4.), cx);
        let was = mark(&body, read, cx).unwrap().top();
        let top = screen(&body, cx).top();
        assert!(
            (was - top - px(4.)).abs() < px(0.5),
            "scrolled to it: {was:?}"
        );
        older(&body, "mupu", (usize::MAX, limit), false, cx);
        let grown = rows(&body, cx);
        assert!(
            grown
                .iter()
                .any(|r| matches!(*r, Row::Run(f, l) if f < first && l == last))
        );
        held(&body, read, was, cx);
    }

    #[gpui_kit::test]
    fn a_page_landing_before_a_scroll_is_painted_keeps_the_scroll(cx: &mut TestAppContext) {
        // A key (or the harness's `find`) scrolls, and a page lands before the next frame: what is
        // read is where the list now is, not what was last painted (the tail).
        let limit = split("mupu");
        let (body, cx) = body(cx, "mupu", (usize::MAX, limit), (420., 320.));
        let key = rows(&body, cx)[2].first();
        body.read_with(cx, |b, _| {
            let list = &b.ui.panel().unwrap().transcript.list;
            list.scroll_to(ListOffset {
                item_ix: 2,
                offset_in_item: px(0.),
            });
        });
        older(&body, "mupu", (usize::MAX, limit), false, cx);
        let ix = rows(&body, cx).iter().position(|r| r.first() == key);
        let ix = ix.expect("the row read is still a row");
        assert!(ix > 2, "rows went in above it");
        let (now, top) = (row(&body, ix, cx).top(), screen(&body, cx).top());
        assert!(
            (now - top).abs() < px(0.5),
            "row {ix} at {now:?}, top {top:?}"
        );
    }

    #[gpui_kit::test]
    fn a_page_landing_before_a_scroll_within_one_row_is_painted_keeps_the_scroll(
        cx: &mut TestAppContext,
    ) {
        // The row painted at the top is still the top row, but the list has moved inside it: the
        // painted offset is stale too.
        let limit = split("mupu");
        let (body, cx) = body(cx, "mupu", (usize::MAX, limit), (420., 320.));
        let to = |ix: usize, y: f32, body: &Entity<Body>, cx: &mut VisualTestContext| {
            body.read_with(cx, |b, _| {
                let at = ListOffset {
                    item_ix: ix,
                    offset_in_item: px(y),
                };
                b.ui.panel().unwrap().transcript.list.scroll_to(at);
            });
        };
        let count = rows(&body, cx).len();
        let tall = (1..count).find(|&ix| {
            to(ix, 0., &body, cx);
            draw(cx);
            row(&body, ix, cx).size.height > px(80.)
        });
        let ix = tall.expect("a row taller than 80");
        let key = rows(&body, cx)[ix].first();
        to(ix, 40., &body, cx);
        older(&body, "mupu", (usize::MAX, limit), false, cx);
        let now = rows(&body, cx).iter().position(|r| r.first() == key);
        let now = now.expect("the row read is still a row");
        assert!(now > ix, "rows went in above it");
        let (at, top) = (row(&body, now, cx).top(), screen(&body, cx).top());
        assert!(
            (at - (top - px(40.))).abs() < px(0.5),
            "row {now} at {at:?}, 40 above the top {top:?}"
        );
    }

    #[gpui_kit::test]
    fn the_scrollbar_leaves_the_tail_before_the_next_render(cx: &mut TestAppContext) {
        let (body, cx) = body(cx, "mupu", (usize::MAX, usize::MAX), (420., 320.));
        let mut board = board();
        body.update(cx, |b, _| {
            let live = frame(&b.store, board.clone());
            b.store.apply(live);
            b.store.apply(Event::Front(true));
            b.store.apply(b.ui.panel().unwrap().transcript.tail(true));
            crate::store::tests::dwell(&mut b.store);
            bump(&mut board, "mupu", 1);
            let landed = frame(&b.store, board.clone());
            transcript::reduce(&mut b.store, &b.ui, landed);
            assert!(!b.store.agent_needs_you("mupu"), "watched as it lands");
            // The scrollbar's handle moves the list without its scroll handler.
            let list = &b.ui.panel().unwrap().transcript.list;
            ScrollbarHandle::set_offset(list, point(px(0.), px(0.)));
            assert!(!list.is_following_tail(), "dragged to the top");
            bump(&mut board, "mupu", 1);
            let landed = frame(&b.store, board.clone());
            transcript::reduce(&mut b.store, &b.ui, landed);
            assert!(b.store.agent_needs_you("mupu"), "scrolled off the bottom");
        });
    }

    #[gpui_kit::test]
    fn o_at_the_tail_opens_the_last_run_when_it_shows(cx: &mut TestAppContext) {
        let (body, cx) = body(cx, "mupu", (usize::MAX, usize::MAX), (900., 700.));
        body.read_with(cx, |b, _| {
            transcript::toggle_lowest(&b.ui.panel().unwrap().transcript)
        });
        let (rows, _, open) = body.read_with(cx, |b, _| b.ui.panel().unwrap().transcript.census());
        assert_eq!(open, 1);
        let last = row(&body, rows - 1, cx);
        assert!(last.size.height > px(0.));
    }

    #[gpui_kit::test]
    fn o_at_the_tail_leaves_a_run_above_a_tall_answer(cx: &mut TestAppContext) {
        // conductor-line ends in an answer after a run; a short window shows only the answer.
        let (body, cx) = body(cx, "conductor-line", (usize::MAX, usize::MAX), (900., 160.));
        let rows = rows(&body, cx);
        let n = rows.len();
        assert!(matches!(rows[n - 1], Row::One(_)) && matches!(rows[n - 2], Row::Run(..)));
        let screen = body.read_with(cx, |b, _| {
            b.ui.panel().unwrap().transcript.list.viewport_bounds()
        });
        let run = body.read_with(cx, |b, _| {
            b.ui.panel()
                .unwrap()
                .transcript
                .painted
                .borrow()
                .rows
                .get(&(n - 2))
                .copied()
        });
        assert!(
            run.is_none_or(|b| b.bottom() <= screen.top()),
            "the run is off screen"
        );
        body.read_with(cx, |b, _| {
            transcript::toggle_lowest(&b.ui.panel().unwrap().transcript)
        });
        assert_eq!(
            body.read_with(cx, |b, _| b.ui.panel().unwrap().transcript.census().2),
            0
        );
    }

    /// G1: right after a drag selects text (one across the link itself, which opens nothing), a real
    /// click on each kind of link, pressed and let go within the frame that still shows the selection,
    /// routes it once as before: a URL to the browser, a name to its preview tab, a path to be
    /// resolved and opened; in an answer, an operator's card, and another agent's card shown as the
    /// latest activity. Without `transcript::replay` the click is dropped.
    #[gpui_kit::test]
    fn a_click_on_a_link_after_a_selection_routes_it_once(cx: &mut TestAppContext) {
        use crate::store::condense::Seg;
        use gpui_kit::base::TextSelection;
        use gpui_kit::{MouseButton, MouseDownEvent, MouseUpEvent, PlatformInput};
        let long = |s: &str| s.repeat(40);
        let answer = |text: String| Item::Assistant(vec![Seg::Text(text)]);
        let card = |text: String, operator| Item::Delivery {
            sender: "kono".into(),
            text,
            operator,
            head: Box::default(),
        };
        let tool = Item::Tool {
            name: "Bash".into(),
            summary: "ls".into(),
            input: "{}".into(),
            result: None,
        };
        // (items appended at the tail; where to click: across, and down from the last row's top, or up
        // from the latest block's bottom (negative: its 4, the card's 1 + 9 and the text's 6, then
        // half a line); what opens). A long link fills its lines; the name starts the answer at 29.
        let url = |n| format!("[{}](https://example.com/pull/{n})", long("Pull "));
        let path = format!("[{}](src/views/transcript.rs)", long("transcript "));
        let name = "confirm-fresh-kono asked.".to_string();
        let to = "link herder-path:src/views/transcript.rs";
        let cases: [(Vec<Item>, (f32, f32), &str); 5] = [
            (
                vec![answer(url(1))],
                (700., 42.),
                "url https://example.com/pull/1",
            ),
            (
                vec![answer(name)],
                (36., 42.),
                "link herder-agent:confirm-fresh-kono",
            ),
            (vec![answer(path.clone())], (700., 42.), to),
            (vec![card(path, true)], (700., 42.5), to),
            (
                vec![tool, card(url(2), false)],
                (100., -30.),
                "url https://example.com/pull/2",
            ),
        ];
        // The test platform keeps the last URL it was asked to open.
        let mut opened = None;
        for (items, (x, y), want) in cases {
            let (body, cx) = body(cx, "mupu", (usize::MAX, 100), (1400., 900.));
            body.update(cx, |b, cx| {
                // The board, whose names the mentions link.
                let event = frame(&b.store, board());
                drop(b.store.apply(event));
                let tr = b.store.transcript.open.values_mut().next().unwrap();
                for (sub, item) in items.into_iter().enumerate() {
                    tr.items.insert((u64::MAX - 1, sub as u16), item);
                }
                cx.notify();
            });
            draw(cx);
            draw(cx);
            let n = rows(&body, cx).len();
            let at = row(&body, n - 1, cx);
            let latest = mark(&body, Mark::Member((u64::MAX - 1, 1)), cx);
            let y = match y > 0. {
                true => at.top() + px(y),
                false => latest.expect("the latest block").bottom() + px(y - 10.),
            };
            let p = point(at.left() + px(x), y);
            // First a drag across the line, the link with it: it selects, and opens nothing.
            let m = Modifiers::default();
            let to = point(p.x + px(250.), p.y);
            cx.simulate_mouse_move(p, None, m);
            cx.simulate_event(MouseDownEvent {
                button: MouseButton::Left,
                position: p,
                modifiers: m,
                click_count: 1,
                first_mouse: false,
            });
            cx.simulate_mouse_move(to, Some(MouseButton::Left), m);
            cx.simulate_event(MouseUpEvent {
                button: MouseButton::Left,
                position: to,
                modifiers: m,
                click_count: 1,
            });
            draw(cx);
            let selected = cx.update(TextSelection::selected_text);
            assert!(!selected.is_empty(), "the drag selected");
            let dragged = body.read_with(cx, |b, _| b.links.len());
            assert_eq!(
                (dragged, cx.opened_url()),
                (0, opened.clone()),
                "a drag opens nothing"
            );
            // Then one click on the link, pressed and let go within the frame that shows the selection
            // (a tap): no frame is drawn between.
            cx.update(|window, cx| {
                let press = MouseDownEvent {
                    button: MouseButton::Left,
                    position: p,
                    modifiers: m,
                    click_count: 1,
                    first_mouse: false,
                };
                window.dispatch_event(PlatformInput::MouseDown(press), cx);
                let release = MouseUpEvent {
                    button: MouseButton::Left,
                    position: p,
                    modifiers: m,
                    click_count: 1,
                };
                window.dispatch_event(PlatformInput::MouseUp(release), cx);
            });
            cx.run_until_parked();
            let mut got = body.read_with(cx, |b, _| b.links.clone());
            got = got.into_iter().map(|l| format!("link {l}")).collect();
            let url = cx.opened_url();
            got.extend(
                url.clone()
                    .filter(|u| opened.as_ref() != Some(u))
                    .map(|u| format!("url {u}")),
            );
            opened = url;
            assert_eq!(got, [want], "clicked at {p:?} in {at:?}");
        }
    }

    /// DK1: a panel hidden behind another tab lets its rows go; shown again, it reads its tail afresh,
    /// pages back as far as where it was read, pages beyond its first screen, and is back there, to the
    /// pixel. Its session is the one its tail landed with after a frame drawn while loading, and hidden
    /// again before it was back, where it was read still stands (remi's DK1 P2s).
    #[gpui_kit::test]
    fn a_hidden_panel_comes_back_where_it_was_read(cx: &mut TestAppContext) {
        let (agent, served) = ("conductor-line", (usize::MAX, 100));
        let (body, cx) = body(cx, agent, served, (720., 600.));
        let (space, other) = body.read_with(cx, |b, _| {
            let s = crate::store::tests::space_of(&b.store, agent);
            let other = s.agents().find(|a| *a != agent).unwrap();
            (s.id.clone(), other.to_string())
        });
        let show = move |b: &mut Body, a: &str| {
            let (space, agent) = (space.clone(), Some(a.to_string()));
            b.ui.zoom = Some(Zoom {
                space: space.clone(),
                agent: agent.clone(),
            });
            let beside = Vec::new();
            b.store.apply(Event::Lens(Move::View {
                space,
                agent,
                beside,
            }))
        };
        let all = served_of(agent, served.0);
        // Away and back, a frame drawn before the tail lands: the panel has no session yet.
        body.update(cx, |b, cx| {
            show(b, &other);
            cx.notify();
        });
        draw(cx);
        let effects = body.update(cx, |b, cx| {
            cx.notify();
            show(b, agent)
        });
        draw(cx);
        body.update(cx, |b, cx| {
            drive(&mut b.store, effects, all, served.1);
            cx.notify();
        });
        draw(cx);
        older(&body, agent, served, false, cx);
        older(&body, agent, served, false, cx);
        body.read_with(cx, |b, _| {
            let list = &b.ui.panel().unwrap().transcript.list;
            list.scroll_to(ListOffset {
                item_ix: 2,
                offset_in_item: px(10.),
            });
        });
        draw(cx);
        let read = rows(&body, cx)[2].first();
        let was = row(&body, 2, cx).top() - screen(&body, cx).top();
        // Another member's tab: the panel is hidden, its transcript closed.
        body.update(cx, |b, cx| {
            show(b, &other);
            cx.notify();
        });
        draw(cx);
        body.read_with(cx, |b, _| {
            assert!(!b.store.transcript.open.contains_key(agent));
            assert_eq!(b.ui.panels[agent].transcript.census().0, 0, "its rows went");
        });
        // Back, its tail read, and away again before the page before it asked for lands.
        body.update(cx, |b, cx| {
            b.reduce = true;
            let effects = show(b, agent);
            drive(&mut b.store, effects, all, served.1);
            cx.notify();
        });
        draw(cx);
        draw(cx);
        body.update(cx, |b, cx| {
            assert_eq!(b.effects.len(), 1, "the page before asked for");
            b.effects.clear();
            show(b, &other);
            cx.notify();
        });
        draw(cx);
        // Back: the tail is read again, and the panel asks for each page before it until that row.
        body.update(cx, |b, cx| {
            let effects = show(b, agent);
            drive(&mut b.store, effects, all, served.1);
            cx.notify();
        });
        let mut back = 0;
        loop {
            draw(cx);
            draw(cx);
            let pages = body.update(cx, |b, cx| {
                let effects = std::mem::take(&mut b.effects);
                cx.notify();
                drive(&mut b.store, effects, all, served.1)
            });
            // Only pages before (the views may not name the api's `Page`).
            let before = |p: &[_]| p.iter().all(|p| format!("{p:?}").starts_with("Before"));
            match pages.len() {
                0 => break,
                1 if before(&pages) => back += 1,
                _ => panic!("{pages:?}"),
            }
        }
        assert!(
            back >= 2,
            "back to the page it was read in, then on as the top nears"
        );
        let ix = rows(&body, cx).iter().position(|r| r.first() == read);
        let now = row(&body, ix.unwrap(), cx).top() - screen(&body, cx).top();
        assert!((now - was).abs() < px(0.5), "{was:?} → {now:?}");
    }

    /// maki's G1 P3: only a tap (pressed and let go with no frame between) on a frame that showed a
    /// selection is replayed. A click with a frame drawn between press and release goes through as it
    /// is, and still routes once.
    #[gpui_kit::test]
    fn a_click_with_a_frame_between_press_and_release_is_not_replayed(cx: &mut TestAppContext) {
        use crate::store::condense::Seg;
        use gpui_kit::base::TextSelection;
        use gpui_kit::{MouseButton, MouseDownEvent, MouseUpEvent, PlatformInput};
        let (body, cx) = body(cx, "mupu", (usize::MAX, 100), (1400., 900.));
        body.update(cx, |b, cx| {
            let text = format!("[{}](src/views/transcript.rs)", "transcript ".repeat(40));
            let tr = b.store.transcript.open.values_mut().next().unwrap();
            tr.items
                .insert((u64::MAX - 1, 0), Item::Assistant(vec![Seg::Text(text)]));
            cx.notify();
        });
        draw(cx);
        draw(cx);
        let n = rows(&body, cx).len();
        let at = row(&body, n - 1, cx);
        let p = point(at.left() + px(700.), at.top() + px(42.));
        let m = Modifiers::default();
        let (left, one) = (MouseButton::Left, 1);
        let down = |position| MouseDownEvent {
            button: left,
            position,
            modifiers: m,
            click_count: one,
            first_mouse: false,
        };
        let up = |position| MouseUpEvent {
            button: left,
            position,
            modifiers: m,
            click_count: one,
        };
        let replays = |cx: &mut VisualTestContext| {
            body.read_with(cx, |b, _| {
                b.ui.panel().unwrap().transcript.taps.replays.get()
            })
        };
        let select = |cx: &mut VisualTestContext| {
            let to = point(p.x + px(250.), p.y);
            cx.simulate_mouse_move(p, None, m);
            cx.simulate_event(down(p));
            cx.simulate_mouse_move(to, Some(left), m);
            cx.simulate_event(up(to));
            draw(cx);
            assert!(!cx.update(TextSelection::selected_text).is_empty());
        };
        let links = |cx: &mut VisualTestContext| body.read_with(cx, |b, _| b.links.len());
        select(cx);
        // Down, a frame, up: as it is.
        cx.update(|window, cx| window.dispatch_event(PlatformInput::MouseDown(down(p)), cx));
        draw(cx);
        cx.update(|window, cx| window.dispatch_event(PlatformInput::MouseUp(up(p)), cx));
        cx.run_until_parked();
        assert_eq!(
            (replays(cx), links(cx)),
            (0, 1),
            "a click with a frame between"
        );
        // A tap: replayed once.
        select(cx);
        cx.update(|window, cx| {
            window.dispatch_event(PlatformInput::MouseDown(down(p)), cx);
            window.dispatch_event(PlatformInput::MouseUp(up(p)), cx);
        });
        cx.run_until_parked();
        assert_eq!((replays(cx), links(cx)), (1, 2), "a tap");
    }

    /// The owner's crash (10-02, every wheel): leaving the watched tail is published from the list's
    /// scroll handler, which the list calls inside its own borrow, and reducing it asked the list again
    /// ("RefCell already mutably borrowed"). A mouse's lines and a trackpad's pixels, over prose, the
    /// scrollbar, sideways, and over an open tool's output.
    #[gpui_kit::test]
    fn the_wheel_leaving_the_watched_tail_publishes_it(cx: &mut TestAppContext) {
        let (body, cx) = body(cx, "mupu", (usize::MAX, 100), (1400., 900.));
        let out = body.update(cx, |b, cx| {
            b.reduce = true;
            let tr = b.store.transcript.focused().unwrap();
            let rows = condense::rows(&tr.items);
            let (ix, first, last) = rows.iter().enumerate().rev().find_map(|(ix, r)| match *r {
                Row::Run(first, last) => Some((ix, first, last)),
                Row::One(_) => None,
            })?;
            let mut tools = tr.items.range(first..=last).rev();
            let (&key, _) = tools.find(|(_, i)| {
                matches!(
                    i,
                    Item::Tool {
                        result: Some(_),
                        ..
                    }
                )
            })?;
            b.ui.panel().unwrap().transcript.toggle((first, last), ix);
            b.ui.panel().unwrap().transcript.fold(Fold(key, 0));
            cx.notify();
            Some(transcript::name("tool", tr.generation, key) + "-out")
        });
        draw(cx);
        let out = out.expect("mupu's last run has a finished tool");
        let pre = cx.update(|window, _| window.try_find(ElementId::Name(out.into())));
        let pre = pre
            .expect("the open tool's output is drawn")
            .bounds()
            .center();
        let lines = |dx, dy| ScrollDelta::Lines(point(dx, dy));
        let pixels = |dx, dy| ScrollDelta::Pixels(point(px(dx), px(dy)));
        let cases = [
            (point(px(700.), px(400.)), lines(0., 3.)),
            (point(px(700.), px(400.)), pixels(0., 40.)),
            (point(px(1395.), px(400.)), lines(0., 3.)),
            (point(px(700.), px(400.)), lines(1., 0.)),
            (pre, lines(0., 3.)),
            (pre, pixels(-40., 10.)),
        ];
        for (position, delta) in cases {
            body.update(cx, |b, cx| {
                b.ui.panel().unwrap().transcript.list.scroll_to(ListOffset {
                    item_ix: usize::MAX,
                    offset_in_item: px(0.),
                });
                let event = b.ui.panel().unwrap().transcript.tail(true);
                drop(b.store.apply(event));
                cx.notify();
            });
            draw(cx);
            cx.simulate_event(ScrollWheelEvent {
                position,
                delta,
                modifiers: Modifiers::default(),
                touch_phase: TouchPhase::Moved,
            });
            draw(cx);
            let (tail, following) = body.read_with(cx, |b, _| {
                let tail = b.store.transcript.focused().unwrap().tail;
                (
                    tail,
                    b.ui.panel().unwrap().transcript.list.is_following_tail(),
                )
            });
            assert_eq!(
                tail, following,
                "{delta:?} at {position:?}: the store hears what the list does"
            );
        }
    }

    /// A3: a tool opened in an open run draws its INPUT and OUTPUT with their text, and closed, neither.
    #[gpui_kit::test]
    fn an_open_tool_shows_its_input_and_output_and_closed_hides_them(cx: &mut TestAppContext) {
        let (body, cx) = body(cx, "mupu", (usize::MAX, 100), (1400., 900.));
        let opened = body.update(cx, |b, cx| {
            let tr = b.store.transcript.focused().unwrap();
            let rows = condense::rows(&tr.items);
            let (ix, first, last) = rows.iter().enumerate().rev().find_map(|(ix, r)| match *r {
                Row::Run(first, last) => Some((ix, first, last)),
                Row::One(_) => None,
            })?;
            let mut tools = tr.items.range(first..=last).rev();
            let (&key, input, output) = tools.find_map(|(key, item)| match item {
                Item::Tool {
                    input,
                    result: Some(r),
                    ..
                } if !r.text.is_empty() => Some((key, input.clone(), r.text.clone())),
                _ => None,
            })?;
            b.ui.panel().unwrap().transcript.toggle((first, last), ix);
            b.ui.panel().unwrap().transcript.fold(Fold(key, 0));
            cx.notify();
            Some((
                transcript::name("tool", tr.generation, key),
                key,
                input,
                output,
            ))
        });
        let (id, key, input, output) = opened.expect("mupu's last run has a finished tool");
        draw(cx);
        let label = |part: &str, cx: &mut VisualTestContext| {
            let id = ElementId::Name(format!("{id}-{part}").into());
            cx.update(|window, _| window.try_find(id))
                .map(|s| s.label().unwrap_or_default().to_string())
        };
        assert_eq!(label("input", cx).as_deref(), Some("INPUT"));
        assert_eq!(label("in", cx), Some(input));
        assert_eq!(label("output", cx).as_deref(), Some("OUTPUT"));
        assert_eq!(label("out", cx), Some(output));
        body.update(cx, |b, cx| {
            b.ui.panel().unwrap().transcript.fold(Fold(key, 0));
            cx.notify();
        });
        draw(cx);
        for part in ["input", "in", "output", "out"] {
            assert_eq!(label(part, cx), None, "{part} drawn closed");
        }
    }
}

mod frame {
    use crate::views::theme::{prose, type_scale};
    use crate::views::transcript::{Kind, gap};
    use gpui_kit::px;

    #[test]
    fn css_lengths_are_web_pixels_at_the_scale() {
        assert_eq!(type_scale(1.).css(13.), px(13.));
        assert_eq!(type_scale(1.2).css(10.), px(12.));
        // The lens's lengths stay the spike's at 0.9.
        assert_eq!(type_scale(1.).px(10.), px(9.));
    }

    #[test]
    fn paragraphs_are_six_web_pixels_apart_at_any_scale() {
        // The kit's root sets the rem to the theme's body size, not 16.
        for scale in [0.8, 1., 1.3] {
            let t = type_scale(scale);
            let gap = prose(t).paragraph_gap().to_pixels(t.body);
            assert!((gap - t.css(6.)).abs() < px(0.01), "{scale}: {gap:?}");
        }
    }

    #[test]
    fn kinds_sit_as_far_apart_as_web_measures_them() {
        use Kind::*;
        let measured = [
            (None, Answer, 10.),
            (Some(Answer), Answer, 10.),
            (Some(Answer), Strip, 10.),
            (Some(Answer), Card, 10.),
            (Some(Strip), Answer, 10.),
            (Some(Strip), Strip, 5.),
            (Some(Strip), Card, 9.),
            (Some(Card), Strip, 9.),
            (Some(Card), Answer, 10.),
            (Some(Strip), Divider, 14.),
            (Some(Divider), Answer, 14.),
            (Some(Strip), System, 6.),
        ];
        for (prev, next, want) in measured {
            assert_eq!(gap(prev, next), want, "{prev:?} → {next:?}");
        }
    }
}

/// A1 review: the transcript's markdown style, laid out headless, and kept to the transcript.
mod prose {
    use crate::views::theme::{self, SANS_T, pal};
    use gpui_kit::base::TextView;
    use gpui_kit::component::ActiveTheme as _;
    use gpui_kit::test::TestWindowExt as _;
    use gpui_kit::{
        Bounds, Context, IntoElement, ParentElement as _, Pixels, Render, Styled as _,
        TestAppContext, Window, canvas, div, px, relative,
    };
    use std::cell::Cell;
    use std::rc::Rc;

    struct Md(&'static str, Rc<Cell<Bounds<Pixels>>>);

    impl Render for Md {
        fn render(&mut self, _: &mut Window, _: &mut Context<Self>) -> impl IntoElement {
            let t = theme::type_scale(1.);
            let text = TextView::markdown("md", self.0).style(theme::prose(t));
            let at = self.1.clone();
            let record = canvas(move |b, _, _| at.set(b), |_, _, _, _| {});
            let el = div().relative().w(px(400.)).font_family(SANS_T);
            let el = el.text_size(t.css(13.)).line_height(relative(1.55));
            let el = el
                .child(text.line_height(relative(1.55)))
                .child(record.absolute().top_0().left_0().size_full());
            // Sized by its text, not stretched to the window.
            div().size_full().flex().flex_col().items_start().child(el)
        }
    }

    fn height(src: &'static str, cx: &mut TestAppContext) -> f32 {
        cx.update(|cx| {
            theme::seed(cx);
            gpui_kit::init(cx);
            theme::dark(cx);
        });
        let at = Rc::new(Cell::new(Bounds::default()));
        let seen = at.clone();
        let (_, cx) = cx.add_window_view(move |_, _| Md(src, seen));
        for _ in 0..3 {
            cx.run_until_parked();
            cx.update(|window, cx| window.render_frame(cx));
        }
        f32::from(at.get().size.height)
    }

    #[gpui_kit::test]
    fn a_heading_sits_as_far_above_its_paragraph_as_web(cx: &mut TestAppContext) {
        // Each heading's line (its size × 1.55) and web's margin under it, then the paragraph. Only
        // the bottom gap: above a heading is the block before's own gap (no collapse by neighbour).
        let body = height("Body.", cx);
        let cases = [
            ("# Title\n\nBody.", 26. * 1.55 + 17.4),
            ("## Title\n\nBody.", 19.5 * 1.55 + 16.2),
            ("### Title\n\nBody.", 15.2 * 1.55 + 15.2),
            ("#### Title\n\nBody.", 13. * 1.55 + 17.3),
        ];
        for (src, want) in cases {
            let got = height(src, cx) - body;
            assert!(
                (got - want).abs() < 0.5,
                "{src:?}: {got} over the paragraph, want {want}"
            );
        }
    }

    #[gpui_kit::test]
    fn web_s_selection_is_the_transcript_s_only(cx: &mut TestAppContext) {
        // The composer's and notes' inputs paint the kit theme's selection.
        cx.update(|cx| {
            theme::seed(cx);
            gpui_kit::init(cx);
            theme::dark(cx);
            let kit = gpui_kit::Hsla {
                a: 1.,
                ..cx.theme().selection
            };
            assert_eq!(kit, gpui_kit::rgb(pal::SELECT).into());
        });
        // Painted over the ground at its opacity, the transcript's reads as web's.
        let sel = theme::prose(theme::type_scale(1.)).selection().to_rgb();
        let ground = gpui_kit::Hsla::from(gpui_kit::rgb(pal::GROUND)).to_rgb();
        let web = gpui_kit::Hsla::from(gpui_kit::rgb(pal::SELECTION)).to_rgb();
        let over = |c: f32, g: f32| c * sel.a + g * (1. - sel.a);
        let seen = [
            over(sel.r, ground.r),
            over(sel.g, ground.g),
            over(sel.b, ground.b),
        ];
        for (got, want) in seen.into_iter().zip([web.r, web.g, web.b]) {
            assert!(
                (got - want).abs() < 1. / 255.,
                "{seen:?} on the ground, want {web:?}"
            );
        }
    }
}

/// A3: a run's members.
mod members {
    use crate::store::Store;
    use crate::store::condense;
    use crate::store::tests::loaded;
    use crate::store::tests::transcript_pages::{drive, history, items, open};
    use crate::store::transcript::{Item, ToolResult};
    use crate::views::entries::{dot, lasted, took};
    use crate::views::theme::pal;
    use crate::views::transcript::{Fold, View};

    fn tool_row(store: &Store, key: crate::store::transcript::Key) -> usize {
        let rows = condense::rows(items(store));
        rows.partition_point(|r| r.last() < key)
    }

    #[test]
    fn an_open_member_stays_open_as_pages_regroup_the_rows() {
        let all = history("mupu");
        let mut store = loaded();
        let effects = open(&mut store, "mupu");
        drive(&mut store, effects, &all, 7);
        let view = View::default();
        view.sync(store.transcript.focused().unwrap(), &store);
        let tools = items(&store).iter().rev();
        let mut tools = tools.filter(|(_, i)| {
            matches!(
                i,
                Item::Tool {
                    result: Some(_),
                    ..
                }
            )
        });
        let key = *tools.next().expect("mupu's tail has a finished tool").0;
        view.fold(Fold(key, 0));
        let was = tool_row(&store, key);
        let mut regrouped = false;
        loop {
            view.sync(store.transcript.focused().unwrap(), &store);
            assert_eq!(view.tools(items(&store)), (1, 1), "open with its output");
            regrouped |= tool_row(&store, key) != was;
            let effects = store.apply(crate::store::tests::older(&store));
            if effects.is_empty() {
                break;
            }
            drive(&mut store, effects, &all, 7);
        }
        assert!(regrouped, "pages before moved the member's row");
        view.fold(Fold(key, 0));
        assert_eq!(view.tools(items(&store)), (0, 0), "a second click closes");
    }

    #[test]
    fn a_tool_s_dot_says_how_it_ended() {
        let result = |error| ToolResult {
            error,
            ..ToolResult::default()
        };
        assert_eq!(dot(None), pal::BLUE, "running");
        assert_eq!(dot(Some(&result(false))), pal::OPERATOR, "done");
        assert_eq!(dot(Some(&result(true))), pal::RED, "failed");
    }

    #[test]
    fn a_tool_runs_until_its_result_comes_whatever_its_times() {
        let result = |at| ToolResult {
            at,
            ..ToolResult::default()
        };
        assert_eq!(lasted(None, Some(1_000)), "running · no result yet");
        assert_eq!(lasted(None, None), "running · no result yet");
        assert_eq!(lasted(Some(&result(Some(1_363))), Some(1_000)), "363ms");
        assert_eq!(
            lasted(Some(&result(None)), Some(1_000)),
            "—",
            "no result time"
        );
        assert_eq!(
            lasted(Some(&result(Some(1_363))), None),
            "—",
            "no call time"
        );
    }

    #[test]
    fn durations_read_as_web_s() {
        let cases = [
            (-1, "—"),
            (0, "0ms"),
            (883, "883ms"),
            (999, "999ms"),
            (1000, "1.0s"),
            (2_749, "2.7s"),
            (2_750, "2.8s"),
            (9_960, "10.0s"),
            (14_499, "14s"),
            (14_500, "15s"),
            (59_499, "59s"),
            (60_000, "1m 0s"),
            (185_400, "3m 5s"),
            (3_600_000, "60m 0s"),
        ];
        for (ms, want) in cases {
            assert_eq!(took(ms), want, "{ms}ms");
        }
    }
}

/// G1: lens cards in a lane are equally tall, and their text wraps and is cut with an ellipsis.
mod lens_cards {
    use crate::store::cards::Got;
    use crate::store::spaces::Row;
    use crate::store::tests::{board, frame, loaded};
    use crate::store::{Event, Store};
    use crate::views::lens::{self, Ui};
    use crate::views::{Host, theme};
    use gpui_kit::{
        Context, IntoElement, Render, Styled as _, TestAppContext, TextOverflow, WhiteSpace,
        Window, px, size,
    };

    struct Lens {
        store: Store,
        ui: Ui,
    }

    impl Host for Lens {
        fn parts(&mut self) -> (&Store, &mut Ui) {
            (&self.store, &mut self.ui)
        }

        fn view(&self) -> (&Store, &Ui) {
            (&self.store, &self.ui)
        }

        fn dispatch(&mut self, _: Event, _: &mut Context<Self>) {}

        fn copy(&mut self, _: String, _: &mut Context<Self>) {}
    }

    impl Render for Lens {
        fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
            let t = theme::type_scale(1.);
            lens::render(&self.store, &self.ui, t, window.viewport_size(), cx)
        }
    }

    #[gpui_kit::test]
    fn the_cards_of_a_lane_are_as_tall_however_much_they_say(cx: &mut TestAppContext) {
        cx.update(|cx| {
            theme::seed(cx);
            gpui_kit::init(cx);
            theme::dark(cx);
        });
        let mut store = loaded();
        store.apply(frame(&store, board()));
        // Every space in focus; conductor-line's card has a long answer, the others their status.
        let ids: Vec<String> = store.spaces.iter().map(|s| s.id.clone()).collect();
        for id in &ids {
            store.prefs.rows.insert(id.clone(), Row::Focus);
        }
        let entries = serde_json::from_str::<crate::api::Entries>(include_str!(
            "../../testdata/agents/conductor-line/tail.json"
        ))
        .unwrap()
        .entries;
        let space = store
            .spaces
            .iter()
            .find(|s| s.agents().any(|a| a == "conductor-line"));
        let space = space.expect("conductor-line's space").id.clone();
        store.prefs.visible.insert(space, "conductor-line".into());
        let turn = store.fleet.agents["conductor-line"].turn_end;
        store.apply(Event::Card(Got {
            agent: "conductor-line".into(),
            turn,
            result: Ok(entries),
        }));
        let long = store.cards.text("conductor-line").expect("an answer").len();
        assert!(long > 400, "long enough to be cut: {long}");
        let (lens, cx) = cx.add_window_view(move |_, cx| Lens {
            store,
            ui: Ui::new(cx),
        });
        cx.simulate_resize(size(px(1400.), px(900.)));
        cx.run_until_parked();
        cx.update(|window, cx| window.draw(cx).clear(cx));
        let heights: Vec<f32> = lens.read_with(cx, |l, _| {
            let cards = l.ui.cards.borrow();
            ids.iter()
                .filter_map(|id| cards.get(id))
                .map(|b| f32::from(b.size.height))
                .collect()
        });
        assert_eq!(heights.len(), ids.len(), "every card laid out");
        assert!(
            heights.windows(2).all(|w| (w[0] - w[1]).abs() < 0.5),
            "one height in the lane: {heights:?}"
        );
    }

    #[test]
    fn a_card_s_text_wraps_and_is_cut_with_an_ellipsis() {
        let t = theme::type_scale(1.);
        let mut text = lens::body("a long answer ".repeat(40), 5, t);
        let style = text.style();
        assert_eq!(style.text.line_clamp, Some(5));
        assert!(
            matches!(style.text.text_overflow, Some(TextOverflow::Truncate(ref s)) if s == "…")
        );
        assert_ne!(
            style.text.white_space,
            Some(WhiteSpace::Nowrap),
            "wraps at words"
        );
        let lines = style.min_size.height.map(|h| format!("{h:?}"));
        assert_eq!(
            lines,
            Some(format!("{:?}", gpui_kit::Length::from(t.line * 5.)))
        );
    }
}

/// F7: type-to-capture with real pointer and key events in a headless window under the kit's `Root` (its
/// selection layer, Tab and cmd-c, as the app's): the zoom, its transcript (mupu's recorded pages, an
/// answer added at the tail), the chip and the composer.
mod capture_events {
    use crate::store::condense::Seg;
    use crate::store::notes::Step as N;
    use crate::store::tests::transcript_pages::{drive, history};
    use crate::store::tests::{board, fleet_frame, loaded};
    use crate::store::transcript::Item;
    use crate::store::{Effect, Event, Store, composer, spaces};
    use crate::views::capture::{self, Capture};
    use crate::views::lens::Ui;
    use crate::views::space::Zoom;
    use crate::views::transcript::{self, Scroll};
    use crate::views::{Host, bind, dock, on, theme};
    use gpui_kit::base::TextSelection;
    use gpui_kit::test::TestWindowExt as _;
    use gpui_kit::{
        Context, Entity, InteractiveElement as _, IntoElement, Modifiers, MouseButton,
        MouseDownEvent, MouseUpEvent, ParentElement as _, Pixels, Point, Render, Styled as _,
        TestAppContext, VisualTestContext, Window, div, point, px, size,
    };

    struct Shell {
        store: Store,
        ui: Ui,
        /// What reached the store, and the effects it answered with.
        events: Vec<Event>,
        effects: Vec<Effect>,
        scrolled: usize,
        leaked: bool,
    }

    impl Host for Shell {
        fn parts(&mut self) -> (&Store, &mut Ui) {
            (&self.store, &mut self.ui)
        }

        fn view(&self) -> (&Store, &Ui) {
            (&self.store, &self.ui)
        }

        fn dispatch(&mut self, event: Event, cx: &mut Context<Self>) {
            // Only the notes and sends: the transcript's own (following the tail) would render again.
            if matches!(event, Event::Note(_) | Event::Compose(_)) {
                self.events.push(event.clone());
                self.effects.extend(self.store.apply(event));
                cx.notify();
            }
        }

        fn copy(&mut self, _: String, _: &mut Context<Self>) {}
    }

    impl Render for Shell {
        fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
            let _ = dock::sync(&mut self.ui, &self.store, window, cx);
            crate::views::composer::sync(&mut self.ui, &self.store, window, cx);
            capture::sync(&mut self.ui, window, cx);
            let (store, ui, t) = (&self.store, &self.ui, theme::type_scale(1.));
            // A zoom key that runs while text is selected breaks the rule.
            let scroll = cx.listener(|s: &mut Shell, _: &Scroll, window, cx| {
                s.scrolled += 1;
                s.leaked |= TextSelection::has_selection(window, cx);
            });
            let space = div()
                .id("space")
                .key_context("Space")
                .track_focus(&ui.zoom_focus)
                .on_action(scroll)
                .on_action(on(cx, |store, ui, c: &Capture| capture::act(store, ui, c)))
                .size_full()
                .child(
                    div()
                        .track_focus(&ui.panel().unwrap().focus)
                        .size_full()
                        .flex()
                        .flex_col()
                        .child(transcript::render(store, ui, "mupu", t, cx))
                        .child(crate::views::composer::render(store, ui, "mupu", t, cx))
                        .children(capture::render(ui, "mupu", t, cx)),
                );
            div().size_full().key_context("Lens").child(space)
        }
    }

    /// Zoomed on a writable mupu, its transcript read, `text` the last answer; focus on the zoom.
    fn open<'a>(
        cx: &'a mut TestAppContext,
        text: &str,
    ) -> (Entity<Shell>, &'a mut VisualTestContext) {
        cx.update(|cx| {
            theme::seed(cx);
            gpui_kit::init(cx);
            theme::dark(cx);
            bind(cx);
        });
        let text = text.to_string();
        let made = std::rc::Rc::new(std::cell::RefCell::new(None));
        let keep = made.clone();
        let (_, cx) = cx.add_window_view(move |window, cx| {
            let shell = gpui_kit::AppContext::new(cx, |cx| shell(text, window, cx));
            *keep.borrow_mut() = Some(shell.clone());
            gpui_kit::base::Root::new(shell, window, cx)
        });
        let shell = made.borrow_mut().take().unwrap();
        cx.simulate_resize(size(px(1400.), px(900.)));
        draw(cx);
        draw(cx);
        (shell, cx)
    }

    fn shell(text: String, window: &mut Window, cx: &mut Context<Shell>) -> Shell {
        {
            let mut store = loaded();
            store.apply(fleet_frame(board()));
            let space = store.spaces[0].id.clone();
            let view = spaces::Move::View {
                space: space.clone(),
                agent: Some("mupu".into()),
                beside: Vec::new(),
            };
            let effects = store.apply(Event::Lens(view));
            drive(&mut store, effects, &history("mupu"), usize::MAX);
            let tr = store.transcript.open.values_mut().next().unwrap();
            tr.items
                .insert((u64::MAX - 1, 0), Item::Assistant(vec![Seg::Text(text)]));
            assert!(store.can_send("mupu").is_ok());
            let mut ui = Ui::new(cx);
            ui.zoom = Some(Zoom {
                space,
                agent: Some("mupu".into()),
            });
            window.focus(&ui.zoom_focus, cx);
            Shell {
                store,
                ui,
                events: Vec::new(),
                effects: Vec::new(),
                scrolled: 0,
                leaked: false,
            }
        }
    }

    fn draw(cx: &mut VisualTestContext) {
        cx.run_until_parked();
        cx.update(|window, cx| window.render_frame(cx));
    }

    /// The last answer's first line: where a drag across it starts.
    fn line(shell: &Entity<Shell>, cx: &mut VisualTestContext) -> Point<Pixels> {
        let at = shell.read_with(cx, |s, _| {
            let painted = s.ui.panel().unwrap().transcript.painted.borrow();
            *painted.rows.iter().max_by_key(|(i, _)| **i).unwrap().1
        });
        point(at.left() + px(36.), at.top() + px(42.))
    }

    /// A real drag across 200px of the last answer's first line.
    fn select(shell: &Entity<Shell>, cx: &mut VisualTestContext) {
        let p = line(shell, cx);
        drag(shell, point(p.x + px(200.), p.y), cx);
    }

    /// A real drag from the last answer's first line, let go at `to`.
    fn drag(shell: &Entity<Shell>, to: Point<Pixels>, cx: &mut VisualTestContext) {
        let (p, m, left) = (line(shell, cx), Modifiers::default(), MouseButton::Left);
        cx.simulate_mouse_move(p, None, m);
        cx.simulate_event(MouseDownEvent {
            button: left,
            position: p,
            modifiers: m,
            click_count: 1,
            first_mouse: false,
        });
        cx.simulate_mouse_move(to, Some(left), m);
        cx.simulate_event(MouseUpEvent {
            button: left,
            position: to,
            modifiers: m,
            click_count: 1,
        });
        draw(cx);
        assert!(
            !cx.update(TextSelection::selected_text).is_empty(),
            "the drag selected"
        );
    }

    /// The capture as the harness asks it: `none`, `chip:<quote>`, `open:<text>`.
    fn shown(shell: &Entity<Shell>, cx: &mut VisualTestContext) -> String {
        shell.read_with(cx, |s, _| match &s.ui.panel().unwrap().capture.draft {
            None => "none".into(),
            Some(d) if d.open => format!("open:{}", s.ui.panel().unwrap().capture.text),
            Some(d) => format!("chip:{}", d.quote),
        })
    }

    fn keys(cx: &mut VisualTestContext, keys: &str) {
        for key in keys.split(' ') {
            cx.simulate_keystrokes(key);
            draw(cx);
        }
    }

    const TEXT: &str = "Everything in this window has been handled, and nothing else waits.";

    #[gpui_kit::test]
    fn typing_after_a_selection_notes_it_with_that_key_and_nothing_scrolls(
        cx: &mut TestAppContext,
    ) {
        let (shell, cx) = open(cx, TEXT);
        select(&shell, cx);
        let chip = shown(&shell, cx);
        assert!(
            chip.starts_with("chip:") && chip.contains("in this window"),
            "{chip}"
        );
        let quote = chip.trim_start_matches("chip:").to_string();
        // The chip sits under the selected line, at its left.
        let p = line(&shell, cx);
        let at = shell.read_with(cx, |s, _| {
            s.ui.panel().unwrap().capture.draft.as_ref().unwrap().at
        });
        assert!(at.y > p.y && at.y < p.y + px(30.), "{at:?} under {p:?}");
        assert_eq!(at.x, p.x);
        // Every key types: `j` would scroll the zoom.
        keys(cx, "j k g");
        assert_eq!(shown(&shell, cx), "open:jkg");
        assert_eq!(
            shell.read_with(cx, |s, _| s.scrolled),
            0,
            "a zoom key fired"
        );
        keys(cx, "shift-enter o enter");
        let (events, effects) = shell.read_with(cx, |s, _| (s.events.clone(), s.effects.clone()));
        let [
            Event::Note(N::Add {
                group,
                text,
                quote: q,
                ..
            }),
        ] = &events[..]
        else {
            panic!("{events:?}")
        };
        assert_eq!(
            (&**group, &**text, q.as_deref()),
            ("mupu", "jkg\no", Some(&*quote))
        );
        // Written as every note is: into the outbox (saved before it is posted, `save_then_send`).
        assert!(effects.contains(&Effect::Persist(crate::store::Persist::Outbox)));
        let posts = effects
            .iter()
            .filter(|e| matches!(e, Effect::Post { .. }))
            .count();
        assert_eq!(posts, 1);
        let note = shell.read_with(cx, |s, _| s.store.notes_of("mupu").next().cloned());
        assert_eq!(note.and_then(|n| n.quote), Some(quote));
        // Closed, the selection is cleared and the zoom's keys are back.
        assert_eq!(shown(&shell, cx), "none");
        assert!(cx.update(TextSelection::selected_text).is_empty());
        keys(cx, "j");
        assert_eq!(shell.read_with(cx, |s, _| s.scrolled), 1);
    }

    #[gpui_kit::test]
    fn enter_opens_it_empty_escape_keeps_nothing_and_gives_the_keys_back(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx, TEXT);
        select(&shell, cx);
        keys(cx, "enter");
        assert_eq!(shown(&shell, cx), "open:");
        keys(cx, "o k escape");
        assert_eq!(shown(&shell, cx), "none");
        // On the chip too.
        select(&shell, cx);
        keys(cx, "escape");
        assert_eq!(shown(&shell, cx), "none");
        assert!(cx.update(TextSelection::selected_text).is_empty());
        let (events, scrolled) = shell.read_with(cx, |s, _| (s.events.len(), s.scrolled));
        assert_eq!((events, scrolled), (0, 0));
        keys(cx, "j");
        assert_eq!(
            shell.read_with(cx, |s, _| s.scrolled),
            1,
            "the keys are back"
        );
    }

    /// `cmd-c` copies the selection (the kit's Root) and leaves the chip as it is: no note opens.
    #[gpui_kit::test]
    fn a_copy_does_not_open_the_note(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx, TEXT);
        select(&shell, cx);
        keys(cx, "cmd-c");
        assert!(
            shown(&shell, cx).starts_with("chip:"),
            "{}",
            shown(&shell, cx)
        );
        assert!(!cx.update(TextSelection::selected_text).is_empty());
    }

    /// `cmd-enter` sends web's note text to the agent on its own (`composer::Step::Quick`): no note,
    /// the draft untouched.
    #[gpui_kit::test]
    fn cmd_enter_sends_the_note_to_the_agent(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx, TEXT);
        select(&shell, cx);
        let quote = shown(&shell, cx).trim_start_matches("chip:").to_string();
        keys(cx, "o k cmd-enter");
        let (events, effects) = shell.read_with(cx, |s, _| (s.events.clone(), s.effects.clone()));
        let text = format!("from mupu's transcript:\n> {quote}\n\nok");
        assert!(
            matches!(&events[..], [Event::Compose(composer::Step::Quick { agent, text: t })] if agent == "mupu" && *t == text),
            "{events:?}"
        );
        let message = Effect::Message {
            agent: "mupu".into(),
            text,
        };
        assert!(effects.contains(&message), "{effects:?}");
        assert_eq!(shown(&shell, cx), "none");
    }

    /// A click anywhere else closes it, typed text and all (web's).
    #[gpui_kit::test]
    fn a_click_elsewhere_closes_it(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx, TEXT);
        select(&shell, cx);
        keys(cx, "o");
        cx.simulate_click(point(px(1300.), px(20.)), Modifiers::default());
        draw(cx);
        assert_eq!(shown(&shell, cx), "none");
        assert_eq!(shell.read_with(cx, |s, _| s.events.len()), 0);
    }

    /// miro's P2: focus leaving the chip or the popover any way (here the kit Root's Tab, to the
    /// composer) cancels it and clears the selection, so no zoom key ever runs with text selected: Esc
    /// then leaves the box and `j` scrolls, the selection gone.
    #[gpui_kit::test]
    fn focus_leaving_it_cancels_it_and_clears_the_selection(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx, TEXT);
        // GPUI says what lost focus only in an active window (the owner's; a scripted run's is not).
        cx.update(|window, _| window.activate_window());
        draw(cx);
        for typed in ["", "o"] {
            select(&shell, cx);
            if !typed.is_empty() {
                keys(cx, typed);
                assert_eq!(shown(&shell, cx), format!("open:{typed}"));
            }
            keys(cx, "tab");
            assert_eq!(shown(&shell, cx), "none", "after `{typed}` and tab");
            assert!(cx.update(TextSelection::selected_text).is_empty());
            keys(cx, "escape j");
        }
        let (events, scrolled, leaked) =
            shell.read_with(cx, |s, _| (s.events.len(), s.scrolled, s.leaked));
        assert_eq!((events, leaked), (0, false));
        assert_eq!(scrolled, 2, "the keys are back once it is gone");
    }

    /// miro's P2: a drag begun in the transcript and let go outside it (over the composer) still gets the
    /// chip, so `j` types instead of scrolling.
    #[gpui_kit::test]
    fn a_drag_let_go_outside_the_transcript_still_gets_the_chip(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx, TEXT);
        let p = line(&shell, cx);
        drag(&shell, point(p.x + px(200.), px(885.)), cx);
        assert!(
            shown(&shell, cx).starts_with("chip:"),
            "{}",
            shown(&shell, cx)
        );
        keys(cx, "j");
        assert_eq!(shown(&shell, cx), "open:j");
        let (scrolled, leaked) = shell.read_with(cx, |s, _| (s.scrolled, s.leaked));
        assert_eq!((scrolled, leaked), (0, false));
    }
}

/// The dock (DK2) in a window: the zoom shell drawn as the app draws it (`space::render`, the dock
/// with its panels), real keys and clicks, and every event the views send applied to the store.
/// Layout reconcile on load: a saved dock opens with each member once where it was, one preview per
/// group (the first), members missing from it added to its first group, emptied groups and splits gone,
/// and the shown tab kept or the one before it.
#[test]
fn a_saved_dock_is_reconciled_with_the_members() {
    use crate::views::dock::{Tree, restore};
    use gpui_kit::component::dock::{PanelInfo, PanelState};
    use gpui_kit::{Axis, px};
    let tab = |agent: &str| {
        let mut s = PanelState::new("agent");
        s.info = PanelInfo::panel(serde_json::json!({ "agent": agent }));
        s
    };
    let group = |agents: &[&str], active| PanelState {
        panel_name: "TabPanel".into(),
        children: agents.iter().map(|a| tab(a)).collect(),
        info: PanelInfo::tabs(active),
    };
    let split = |children: Vec<PanelState>, sizes: Vec<f32>| PanelState {
        panel_name: "StackPanel".into(),
        children,
        info: PanelInfo::stack(sizes.into_iter().map(px).collect(), Axis::Horizontal),
    };
    let saved = split(
        vec![
            group(&["a", "gone", "p1", "p2", "b"], 4),
            group(&["a", "p3", "c"], 2),
            group(&["gone"], 0),
        ],
        vec![600., 500., 300.],
    );
    let members: Vec<String> = ["a", "b", "c", "d"].map(String::from).into();
    let tabs = |agents: &[&str], active| Tree::Tabs {
        agents: agents.iter().map(|a| a.to_string()).collect(),
        active,
    };
    assert_eq!(
        restore(&saved, &members),
        Some(Tree::Split(
            Axis::Horizontal,
            vec![
                (tabs(&["a", "gone", "b", "d"], 2), Some(px(600.))),
                (tabs(&["p3", "c"], 1), Some(px(500.))),
            ]
        )),
        "gone is the first group's preview, so p1 and p2 go; a once; d appended"
    );
    let shown_gone = split(vec![group(&["a", "x", "y"], 2)], vec![0.]);
    let members = ["a".to_string()];
    assert_eq!(
        restore(&shown_gone, &members),
        Some(Tree::Split(
            Axis::Horizontal,
            vec![(tabs(&["a", "x"], 1), None)]
        )),
        "y dropped: the tab before it shows"
    );
    let mut marked = group(&["a", "gone", "p"], 2);
    marked.children[2].info =
        PanelInfo::panel(serde_json::json!({ "agent": "p", "preview": true }));
    assert_eq!(
        restore(&split(vec![marked], vec![0.]), &members),
        Some(Tree::Split(
            Axis::Horizontal,
            vec![(tabs(&["a", "p"], 1), None)]
        )),
        "p was saved as the group's preview: gone (a member removed since) goes"
    );
    assert_eq!(restore(&split(vec![], vec![]), &members), None);
    assert_eq!(restore(&tab("a"), &members), None, "not a tree of groups");
}

mod dock_events {
    use crate::api::Member;
    use crate::store::spaces::{Layouts, Move};
    use crate::store::tests::{board, fleet_frame, loaded, space_of};
    use crate::store::{Event, Store};
    use crate::views::lens::Ui;
    use crate::views::space::{self, Zoom};
    use crate::views::transcript::OpenLink;
    use crate::views::{Host, bind, dock, theme};
    use gpui_kit::component::dock::{DockPlacement, InsertTarget};
    use gpui_kit::test::TestWindowExt as _;
    use gpui_kit::{
        Context, Entity, InteractiveElement as _, IntoElement, Modifiers, ParentElement as _,
        Render, Styled as _, TestAppContext, VisualTestContext, Window, div, point, px, size,
    };

    struct Shell {
        store: Store,
        ui: Ui,
        /// What the store was told is on screen, in order: the focused agent and those beside it.
        views: Vec<(Option<String>, Vec<String>)>,
        moves: Vec<Move>,
        /// The transcript reads the store asked for, unanswered (`feed`).
        reads: Vec<crate::store::transcript::Read>,
    }

    impl Host for Shell {
        fn parts(&mut self) -> (&Store, &mut Ui) {
            (&self.store, &mut self.ui)
        }

        fn view(&self) -> (&Store, &Ui) {
            (&self.store, &self.ui)
        }

        fn dispatch(&mut self, event: Event, cx: &mut Context<Self>) {
            if let Event::Lens(m) = &event {
                if let Move::View { agent, beside, .. } = m {
                    self.views.push((agent.clone(), beside.clone()));
                }
                self.moves.push(m.clone());
            }
            for effect in self.store.apply(event) {
                if let crate::store::Effect::Fetch(crate::store::Fetch::Transcript(r)) = effect {
                    self.reads.push(r);
                }
            }
            cx.notify();
        }

        fn copy(&mut self, _: String, _: &mut Context<Self>) {}
    }

    impl Render for Shell {
        fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
            let events = dock::sync(&mut self.ui, &self.store, window, cx);
            if !events.is_empty() {
                cx.defer_in(window, |s, _, cx| {
                    events.into_iter().for_each(|e| s.dispatch(e, cx))
                });
            }
            let t = theme::type_scale(1.);
            let zoom = self.ui.zoom.clone();
            let space = zoom.map(|z| space::render(&self.store, &self.ui, &z, t, cx));
            div().size_full().key_context("Lens").children(space)
        }
    }

    const SPACE: [&str; 3] = ["perps-provisioning-mupe", "design-296-lego", "walk-nuna"];

    /// Zoomed into perps (three members) on its second, focused.
    fn open(cx: &mut TestAppContext) -> (Entity<Shell>, &mut VisualTestContext) {
        launch(cx, Layouts::default())
    }

    /// `open`, with `layouts` read from disk at boot.
    fn launch(
        cx: &mut TestAppContext,
        layouts: Layouts,
    ) -> (Entity<Shell>, &mut VisualTestContext) {
        boot(cx, layouts, SPACE[1], 1400.)
    }

    /// Zoomed into perps on `agent`, focused, in a window `width` wide: the zoom (and its dock) comes
    /// once the window is that wide.
    fn boot<'a>(
        cx: &'a mut TestAppContext,
        layouts: Layouts,
        agent: &str,
        width: f32,
    ) -> (Entity<Shell>, &'a mut VisualTestContext) {
        let agent = agent.to_string();
        cx.update(|cx| {
            theme::seed(cx);
            gpui_kit::init(cx);
            theme::dark(cx);
            bind(cx);
        });
        let made = std::rc::Rc::new(std::cell::RefCell::new(None));
        let keep = made.clone();
        let (_, cx) = cx.add_window_view(move |window, cx| {
            let shell = gpui_kit::AppContext::new(cx, |cx| {
                let mut store = loaded();
                store.apply(Event::LayoutsLoaded(layouts));
                store.apply(fleet_frame(board()));
                let ui = Ui::new(cx);
                let (views, moves) = (Vec::new(), Vec::new());
                Shell {
                    store,
                    ui,
                    views,
                    moves,
                    reads: Vec::new(),
                }
            });
            *keep.borrow_mut() = Some(shell.clone());
            let dots = shell.read(cx).ui.dots.clone();
            let frame = gpui_kit::AppContext::new(cx, |cx| {
                crate::views::Frame::new(shell.into(), dots, cx)
            });
            gpui_kit::base::Root::new(frame, window, cx)
        });
        let shell = made.borrow_mut().take().unwrap();
        cx.simulate_resize(size(px(width), px(900.)));
        draw(cx);
        shell.update(cx, |s, cx| {
            let space = space_of(&s.store, SPACE[0]).id.clone();
            let agent = Some(agent);
            s.ui.zoom = Some(Zoom { space, agent });
            cx.notify();
        });
        draw(cx);
        cx.update(|window, cx| {
            let focus = shell.read(cx).ui.focus_target().clone();
            window.focus(&focus, cx);
        });
        draw(cx);
        (shell, cx)
    }

    fn draw(cx: &mut VisualTestContext) {
        cx.run_until_parked();
        cx.update(|window, cx| window.render_frame(cx));
        cx.run_until_parked();
    }

    fn dock(shell: &Entity<Shell>, cx: &mut VisualTestContext) -> String {
        cx.update(|_, cx| {
            let s = shell.read(cx);
            dock::describe(&s.store, &s.ui, cx)
        })
    }

    fn told(shell: &Entity<Shell>, cx: &mut VisualTestContext) -> (Option<String>, Vec<String>) {
        shell.read_with(cx, |s, _| s.views.last().cloned().unwrap_or_default())
    }

    /// A real left click at a window point.
    fn click(cx: &mut VisualTestContext, x: f32, y: f32) {
        cx.simulate_mouse_move(point(px(x), px(y)), None, Modifiers::none());
        cx.simulate_click(point(px(x), px(y)), Modifiers::none());
        draw(cx);
    }

    fn act(cx: &mut VisualTestContext, action: impl gpui_kit::Action) {
        cx.dispatch_action(action);
        draw(cx);
    }

    /// The visible set drives the store: an `alt`-click on a mention opens it beside, in a group to the
    /// right, focused; the store is told it is looked at with the other group's shown tab beside it.
    #[gpui_kit::test]
    fn an_alt_click_opens_beside_and_the_store_reads_both(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        assert_eq!(
            dock(&shell, cx),
            format!("{} [{}*] {}", SPACE[0], SPACE[1], SPACE[2])
        );
        assert_eq!(told(&shell, cx), (Some(SPACE[1].into()), Vec::new()));
        act(cx, OpenLink("herder-agent:mupu".into(), true));
        let split = format!("{} {}* {} | [mupu*~]", SPACE[0], SPACE[1], SPACE[2]);
        assert_eq!(dock(&shell, cx), split);
        assert_eq!(
            told(&shell, cx),
            (Some("mupu".into()), vec![SPACE[1].into()])
        );
        shell.read_with(cx, |s, _| {
            let open = &s.store.transcript.open;
            assert!(
                open.contains_key("mupu") && open.contains_key(SPACE[1]),
                "both read"
            );
        });
    }

    /// Seen follows focus across groups: a click in the other group's panel makes its agent the zoom's,
    /// and only it is seen.
    #[gpui_kit::test]
    fn a_click_in_the_other_group_moves_focus_and_seen(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        act(cx, OpenLink("herder-agent:mupu".into(), true));
        click(cx, 300., 500.);
        assert_eq!(
            told(&shell, cx),
            (Some(SPACE[1].into()), vec!["mupu".into()])
        );
        shell.read_with(cx, |s, _| {
            assert_eq!(s.store.transcript.focused, Some(SPACE[1].to_string()));
        });
        let split = format!("{} [{}*] {} | mupu*~", SPACE[0], SPACE[1], SPACE[2]);
        assert_eq!(dock(&shell, cx), split);
    }

    /// The tab keys act on the focused group: `tab` and `alt-left` step through it, wrapping, `cmd-1`
    /// picks; `alt-enter` maximizes it (the other group's tab is no longer beside) and puts it back;
    /// `cmd-w` closes the focused tab, and a pinned one leaves the space.
    #[gpui_kit::test]
    fn the_tab_keys_act_on_the_focused_group(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        act(cx, OpenLink("herder-agent:mupu".into(), true));
        click(cx, 300., 500.);
        let at = |i: usize| {
            let tab = |j: usize| match j == i {
                true => format!("[{}*]", SPACE[j]),
                false => SPACE[j].to_string(),
            };
            format!("{} {} {} | mupu*~", tab(0), tab(1), tab(2))
        };
        cx.simulate_keystrokes("tab");
        draw(cx);
        assert_eq!(dock(&shell, cx), at(2));
        cx.simulate_keystrokes("tab");
        draw(cx);
        assert_eq!(dock(&shell, cx), at(0), "wraps");
        cx.simulate_keystrokes("alt-left");
        draw(cx);
        assert_eq!(dock(&shell, cx), at(2));
        cx.simulate_keystrokes("cmd-2");
        draw(cx);
        assert_eq!(dock(&shell, cx), at(1));
        assert_eq!(
            told(&shell, cx),
            (Some(SPACE[1].into()), vec!["mupu".into()])
        );

        cx.simulate_keystrokes("alt-enter");
        draw(cx);
        assert_eq!(dock(&shell, cx), format!("max {}", at(1)));
        assert_eq!(told(&shell, cx), (Some(SPACE[1].into()), Vec::new()));
        cx.simulate_keystrokes("alt-enter");
        draw(cx);
        assert_eq!(dock(&shell, cx), at(1));

        cx.simulate_keystrokes("cmd-w");
        draw(cx);
        let left = format!("{} [{}*] | mupu*~", SPACE[0], SPACE[2]);
        assert_eq!(dock(&shell, cx), left, "the group shows the tab after it");
        shell.read_with(cx, |s, _| {
            let unpinned = s
                .moves
                .iter()
                .any(|m| matches!(m, Move::Unpin { agent, .. } if agent == SPACE[1]));
            assert!(unpinned, "the member is removed");
            let space = space_of(&s.store, SPACE[0]);
            assert!(space.agents().all(|a| a != SPACE[1]));
        });
    }

    /// None of the dock's chords fires in the composer: there they are the box's or nothing.
    #[gpui_kit::test]
    fn the_dock_chords_do_not_fire_in_the_composer(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        act(cx, OpenLink("herder-agent:mupu".into(), true));
        let before = dock(&shell, cx);
        cx.update(|window, cx| {
            let ui = &shell.read(cx).ui;
            let state = ui.panel().unwrap().composer.focus_handle(cx);
            window.focus(&state, cx);
        });
        draw(cx);
        for keys in ["cmd-w", "cmd-1", "alt-left", "alt-right"] {
            cx.simulate_keystrokes(keys);
            draw(cx);
            assert_eq!(dock(&shell, cx), before, "{keys}");
        }
        shell.read_with(cx, |s, _| {
            assert!(s.moves.iter().all(|m| !matches!(m, Move::Unpin { .. })))
        });
    }

    fn pinned(shell: &Entity<Shell>, cx: &mut VisualTestContext, who: &str) -> bool {
        shell.read_with(cx, |s, _| {
            let pin = |m: &Move| matches!(m, Move::Pin { agent, .. } if agent == who);
            s.moves.iter().any(pin) && space_of(&s.store, SPACE[0]).agents().any(|a| a == who)
        })
    }

    /// A mention opens a preview in the focused group; the next one opened there replaces it, in its
    /// place; a double-click on it pins it (the agent joins the space).
    #[gpui_kit::test]
    fn a_preview_replaces_the_groups_preview_and_a_double_click_pins_it(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        act(cx, OpenLink("herder-agent:mupu".into(), false));
        let tabs = format!("{} {} {}", SPACE[0], SPACE[1], SPACE[2]);
        assert_eq!(dock(&shell, cx), format!("{tabs} [mupu*~]"));
        act(cx, OpenLink("herder-agent:support-mifa".into(), false));
        assert_eq!(dock(&shell, cx), format!("{tabs} [support-mifa*~]"));
        shell.read_with(cx, |s, _| {
            assert!(!s.ui.panels.contains_key("mupu"), "its panel went")
        });
        act(cx, dock::Pin("support-mifa".into()));
        assert_eq!(dock(&shell, cx), format!("{tabs} [support-mifa*]"));
        assert!(pinned(&shell, cx, "support-mifa"));
    }

    /// A preview dragged into another group is pinned (found by the layout's change), and it is the
    /// zoom's: it took focus.
    #[gpui_kit::test]
    fn a_preview_dragged_to_another_group_is_pinned(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        act(cx, OpenLink("herder-agent:mupu".into(), true));
        cx.update(|window, cx| {
            let area = shell.read(cx).ui.dock.as_ref().unwrap().area.clone();
            let id = shell.read(cx).ui.panels["mupu"].id;
            let tree = area.read(cx).layout(DockPlacement::Center).unwrap();
            let left = tree
                .find_panel_node(shell.read(cx).ui.panels[SPACE[0]].id)
                .unwrap();
            let to = InsertTarget::Tabs {
                node: left,
                ix: None,
                activate: true,
            };
            area.update(cx, |a, cx| a.move_panel(id, to, window, cx));
        });
        draw(cx);
        assert!(pinned(&shell, cx, "mupu"));
        let tabs = format!("{} {} {}", SPACE[0], SPACE[1], SPACE[2]);
        assert_eq!(dock(&shell, cx), format!("{tabs} [mupu*]"));
    }

    /// Restore after relaunch: the dock as left (its groups in order, the preview, each group's shown
    /// tab) is saved as it changes, and the next launch opens the space on it; maximize is not kept.
    #[gpui_kit::test]
    fn a_dock_opens_after_a_relaunch_as_it_was_left(cx: &mut TestAppContext) {
        let (shell, vcx) = open(cx);
        act(vcx, OpenLink("herder-agent:mupu".into(), true));
        vcx.update(|window, cx| {
            let ui = &shell.read(cx).ui;
            let area = ui.dock.as_ref().unwrap().area.clone();
            let (id, mupu) = (ui.panels[SPACE[2]].id, ui.panels["mupu"].id);
            let tree = area.read(cx).layout(DockPlacement::Center).unwrap();
            let right = tree.find_panel_node(mupu).unwrap();
            let to = InsertTarget::Tabs {
                node: right,
                ix: None,
                activate: true,
            };
            area.update(cx, |a, cx| a.move_panel(id, to, window, cx));
        });
        draw(vcx);
        vcx.simulate_keystrokes("alt-enter");
        draw(vcx);
        let left = format!("max {} {}* | mupu~ [{}*]", SPACE[0], SPACE[1], SPACE[2]);
        assert_eq!(dock(&shell, vcx), left);
        let layouts = shell.read_with(vcx, |s, _| s.store.layouts.clone());
        let (shell, vcx) = launch(cx, layouts);
        let again = format!("{} [{}*] | mupu~ {}*", SPACE[0], SPACE[1], SPACE[2]);
        assert_eq!(
            dock(&shell, vcx),
            again,
            "opened on its second, not maximized"
        );
    }

    /// How many times `who` was pinned.
    fn pins(shell: &Entity<Shell>, cx: &mut VisualTestContext, who: &str) -> usize {
        shell.read_with(cx, |s, _| {
            let pin = |m: &&Move| matches!(m, Move::Pin { agent, .. } if agent == who);
            s.moves.iter().filter(pin).count()
        })
    }

    /// `agent` loaded, so the store takes a send to it.
    fn writable(shell: &Entity<Shell>, cx: &mut VisualTestContext, agent: &str) {
        shell.update(cx, |s, _| {
            let detail = include_str!("../../testdata/agents/mupu/detail.json");
            let t = s.store.transcript.open.get_mut(agent).unwrap();
            t.detail = Some(serde_json::from_str(detail).unwrap());
            assert!(s.store.can_send(agent).is_ok());
        });
    }

    /// A send the store takes from a preview pins it, once; one it refuses (nothing to send, the agent
    /// not loaded) writes nothing.
    #[gpui_kit::test]
    fn a_send_from_a_preview_pins_it_and_a_refused_one_does_not(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        act(cx, OpenLink("herder-agent:mupu".into(), false));
        cx.update(|window, cx| {
            let ui = &shell.read(cx).ui;
            let state = ui.panel().unwrap().composer.focus_handle(cx);
            window.focus(&state, cx);
        });
        draw(cx);
        shell.read_with(cx, |s, _| assert!(!s.store.ready("mupu")));
        act(cx, crate::views::composer::Compose::Send);
        assert_eq!(
            pins(&shell, cx, "mupu"),
            0,
            "a refused send wrote the members"
        );
        writable(&shell, cx, "mupu");
        shell.update(cx, |s, _| {
            s.store.prefs.drafts.insert("mupu".into(), "hello".into());
        });
        draw(cx);
        shell.read_with(cx, |s, _| assert!(s.store.ready("mupu")));
        act(cx, crate::views::composer::Compose::Send);
        assert_eq!(pins(&shell, cx, "mupu"), 1);
        assert!(pinned(&shell, cx, "mupu"));
    }

    /// A quoted note's quick send (capture) from a preview pins it too, as web's does.
    #[gpui_kit::test]
    fn a_quick_send_from_a_preview_pins_it(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        act(cx, OpenLink("herder-agent:mupu".into(), false));
        writable(&shell, cx, "mupu");
        shell.update(cx, |s, cx| {
            let panel = s.ui.panels.get_mut("mupu").unwrap();
            panel.capture.draft = Some(crate::views::capture::Draft {
                agent: "mupu".into(),
                quote: "a quote".into(),
                at: point(px(50.), px(100.)),
                open: true,
            });
            let send = crate::views::capture::Capture::Send;
            let events = crate::views::capture::act(&s.store, &mut s.ui, &send);
            let quick = |e: &Event| {
                matches!(
                    e,
                    Event::Compose(crate::store::composer::Step::Quick { .. })
                )
            };
            assert!(events.iter().any(quick));
            for e in events {
                s.dispatch(e, cx);
            }
        });
        draw(cx);
        assert_eq!(pins(&shell, cx, "mupu"), 1);
        assert!(pinned(&shell, cx, "mupu"));
    }

    /// A click in the other group's strip (its □) maximizes that group, and the zoom moves to its tab.
    #[gpui_kit::test]
    fn the_other_groups_maximize_holds(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        act(cx, OpenLink("herder-agent:mupu".into(), true));
        click(cx, 681., 42.);
        let now = dock(&shell, cx);
        assert!(now.starts_with("max "), "maximize undone: {now}");
        assert_eq!(told(&shell, cx), (Some(SPACE[1].into()), vec![]));
    }

    /// The space's members as another device left them.
    fn remote(
        shell: &Entity<Shell>,
        cx: &mut VisualTestContext,
        edit: impl FnOnce(&mut Vec<Member>),
    ) {
        shell.update(cx, |s, cx| {
            let id = s.ui.zoom.as_ref().unwrap().space.clone();
            let space = s.store.spaces.iter_mut().find(|sp| sp.id == id).unwrap();
            edit(&mut space.members);
            cx.notify();
        });
        draw(cx);
    }

    fn without(name: &'static str) -> impl FnOnce(&mut Vec<Member>) {
        move |m| m.retain(|m| !matches!(m, Member::Agent { name: n } if n == name))
    }

    /// A member another device removes stays open as a preview, unless its group has one: then its tab
    /// closes, and the group keeps the preview it had (saved marked as such, for a relaunch). Nothing is
    /// written back.
    #[gpui_kit::test]
    fn a_member_removed_elsewhere_leaves_one_preview_in_its_group(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        remote(&shell, cx, without(SPACE[2]));
        let now = format!("{} [{}*] {}~", SPACE[0], SPACE[1], SPACE[2]);
        assert_eq!(
            dock(&shell, cx),
            now,
            "no preview there: it becomes the group's"
        );
        remote(&shell, cx, without(SPACE[0]));
        let now = format!("[{}*] {}~", SPACE[1], SPACE[2]);
        assert_eq!(dock(&shell, cx), now, "the group has a preview: it closes");
        act(cx, OpenLink("herder-agent:mupu".into(), false));
        let now = format!("{} [mupu*~]", SPACE[1]);
        assert_eq!(dock(&shell, cx), now, "the next preview replaces that one");
        shell.read_with(cx, |s, _| {
            let saved = serde_json::to_string(&s.store.layouts).unwrap();
            assert!(
                saved.contains(r#"{"agent":"mupu","preview":true}"#),
                "{saved}"
            );
        });
        remote(&shell, cx, without(SPACE[1]));
        assert_eq!(
            dock(&shell, cx),
            "[mupu*~]",
            "the zoom's own tab closes too"
        );
        assert_eq!(told(&shell, cx).0.as_deref(), Some("mupu"));
        shell.read_with(cx, |s, _| {
            assert!(s.moves.iter().all(|m| matches!(m, Move::View { .. })))
        });
    }

    /// A member another device adds to a dock with no tab left opens there, the zoom's and focused.
    #[gpui_kit::test]
    fn a_member_added_to_an_empty_dock_is_focused(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        for _ in 0..3 {
            act(cx, dock::Close(None));
        }
        assert!(told(&shell, cx).0.is_none());
        remote(&shell, cx, |m| {
            m.push(Member::Agent {
                name: "mupu".into(),
            })
        });
        assert_eq!(told(&shell, cx), (Some("mupu".into()), vec![]));
        shell.read_with(cx, |s, _| assert_eq!(s.ui.zoomed_agent(), Some("mupu")));
        cx.update(|window, cx| {
            let focus = shell.read(cx).ui.panels["mupu"].focus.clone();
            assert!(
                focus.contains_focused(window, cx),
                "the tab keys need it focused"
            );
        });
    }

    /// A tab dropped in its own group (moved there) is the zoom's and takes focus, as one dropped in
    /// another group does.
    #[gpui_kit::test]
    fn a_tab_moved_in_its_group_takes_focus(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        act(cx, OpenLink("herder-agent:mupu".into(), true));
        cx.update(|window, cx| {
            let area = shell.read(cx).ui.dock.as_ref().unwrap().area.clone();
            let id = shell.read(cx).ui.panels[SPACE[0]].id;
            let tree = area.read(cx).layout(DockPlacement::Center).unwrap();
            let node = tree.find_panel_node(id).unwrap();
            let ix = Some(2);
            let to = InsertTarget::Tabs {
                node,
                ix,
                activate: true,
            };
            area.update(cx, |a, cx| a.move_panel(id, to, window, cx));
        });
        draw(cx);
        assert_eq!(told(&shell, cx).0.as_deref(), Some(SPACE[0]));
        let now = format!("{} {} [{}*] | mupu*~", SPACE[1], SPACE[2], SPACE[0]);
        assert_eq!(dock(&shell, cx), now);
    }

    /// The same by a real drag: the left group's first tab dragged onto its strip's empty end.
    #[gpui_kit::test]
    fn a_tab_dragged_in_its_group_takes_focus(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        act(cx, OpenLink("herder-agent:mupu".into(), true));
        let left = gpui_kit::MouseButton::Left;
        cx.simulate_mouse_down(point(px(80.), px(42.)), left, Modifiers::none());
        cx.simulate_mouse_move(point(px(100.), px(42.)), left, Modifiers::none());
        draw(cx);
        cx.simulate_mouse_move(point(px(470.), px(42.)), left, Modifiers::none());
        draw(cx);
        cx.simulate_mouse_up(point(px(470.), px(42.)), left, Modifiers::none());
        draw(cx);
        assert_eq!(
            told(&shell, cx).0.as_deref(),
            Some(SPACE[0]),
            "{}",
            dock(&shell, cx)
        );
        let now = format!("{} {} [{}*] | mupu*~", SPACE[1], SPACE[2], SPACE[0]);
        assert_eq!(dock(&shell, cx), now);
    }

    /// A key right after focus moved into another group, before a frame, acts on that group.
    #[gpui_kit::test]
    fn a_key_after_a_focus_move_acts_on_the_new_group(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        act(cx, OpenLink("herder-agent:mupu".into(), true));
        cx.update(|window, cx| {
            let focus = shell.read(cx).ui.panels[SPACE[1]].focus.clone();
            window.focus(&focus, cx);
        });
        cx.dispatch_action(space::Zoomed::Agent(1));
        draw(cx);
        assert_eq!(told(&shell, cx).0.as_deref(), Some(SPACE[2]));
    }

    /// mupu's transcript read, its tail from the fixture.
    fn feed(shell: &Entity<Shell>, cx: &mut VisualTestContext) {
        shell.update(cx, |s, cx| {
            let at = s
                .reads
                .iter()
                .position(|r| r.agent == "mupu")
                .expect("a read of mupu");
            let read = s.reads.remove(at);
            let tail = include_str!("../../testdata/agents/mupu/tail.json");
            let page: crate::api::Entries = serde_json::from_str(tail).unwrap();
            let got = Ok(crate::store::transcript::Got::Page(Box::new(page)));
            s.dispatch(
                Event::Transcript(crate::store::transcript::Step::Read(read, got)),
                cx,
            );
        });
        draw(cx);
    }

    /// Focus on the transcript's text (its selection's own element), as a click there leaves it.
    fn on_text(shell: &Entity<Shell>, cx: &mut VisualTestContext) {
        click(cx, 900., 500.);
        cx.update(|window, cx| {
            let ui = &shell.read(cx).ui;
            let p = ui.panels["mupu"].focus.clone();
            let f = window.focused(cx).expect("focused");
            assert!(
                f != p && p.contains(&f, window),
                "inside mupu's panel, not on it"
            );
        });
    }

    /// The zoom's panel holds focus (keys reach the zoom's bindings).
    fn keys_live(shell: &Entity<Shell>, cx: &mut VisualTestContext, why: &str) {
        cx.update(|window, cx| {
            let ui = &shell.read(cx).ui;
            let live = window
                .focused(cx)
                .is_some_and(|f| ui.focus_target().contains(&f, window));
            assert!(live, "{why}: focus left the zoom");
        });
    }

    /// A maximize lays the dock out anew, and the element focus was on (the transcript's text) goes:
    /// focus goes back to the zoom's panel, so the keys act without a click, from `alt-enter` and the
    /// group's □, in and out.
    #[gpui_kit::test]
    fn the_keys_act_after_a_maximize_without_a_click(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        act(cx, OpenLink("herder-agent:mupu".into(), true));
        feed(&shell, cx);
        let max = |cx: &mut VisualTestContext| dock(&shell, cx).starts_with("max ");
        // In: the next key (alt-enter again) acts, and puts it back.
        on_text(&shell, cx);
        cx.simulate_keystrokes("alt-enter");
        draw(cx);
        keys_live(&shell, cx, "maximized");
        assert!(max(cx));
        cx.simulate_keystrokes("alt-enter");
        draw(cx);
        assert!(!max(cx), "the key after a maximize acts");
        // Out: from the text of the maximized panel, the same.
        cx.simulate_keystrokes("alt-enter");
        draw(cx);
        on_text(&shell, cx);
        cx.simulate_keystrokes("alt-enter");
        draw(cx);
        keys_live(&shell, cx, "put back");
        assert!(!max(cx));
        cx.simulate_keystrokes("alt-left");
        draw(cx);
        cx.simulate_keystrokes("tab");
        draw(cx);
        assert_eq!(
            told(&shell, cx).0.as_deref(),
            Some("mupu"),
            "a one-tab group steps to itself"
        );
        cx.simulate_keystrokes("alt-enter");
        draw(cx);
        assert!(max(cx), "the key after putting back acts");
        cx.simulate_keystrokes("alt-enter");
        draw(cx);
        // The □ (mupu's group, the right one) is a click, not a key: the same.
        on_text(&shell, cx);
        click(cx, 1381., 42.);
        keys_live(&shell, cx, "the □");
        assert!(dock(&shell, cx).starts_with("max "), "{}", dock(&shell, cx));
        cx.simulate_keystrokes("alt-enter");
        draw(cx);
        assert!(
            !dock(&shell, cx).starts_with("max "),
            "{}",
            dock(&shell, cx)
        );
    }

    /// The same after the other ways the dock lays out anew: a drop, a close, `cmd-w` on the last tab,
    /// another device's add and removal.
    #[gpui_kit::test]
    fn focus_stays_in_the_zoom_after_any_dock_change(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        act(cx, OpenLink("herder-agent:mupu".into(), true));
        feed(&shell, cx);
        on_text(&shell, cx);
        // mupu's tab dropped on the left group: it is pinned there, and keeps focus.
        let left = gpui_kit::MouseButton::Left;
        cx.simulate_mouse_down(point(px(760.), px(42.)), left, Modifiers::none());
        cx.simulate_mouse_move(point(px(740.), px(42.)), left, Modifiers::none());
        draw(cx);
        cx.simulate_mouse_move(point(px(470.), px(42.)), left, Modifiers::none());
        draw(cx);
        cx.simulate_mouse_up(point(px(470.), px(42.)), left, Modifiers::none());
        draw(cx);
        keys_live(&shell, cx, "a drop");
        assert_eq!(
            told(&shell, cx).0.as_deref(),
            Some("mupu"),
            "{}",
            dock(&shell, cx)
        );
        on_text(&shell, cx);
        remote(&shell, cx, without(SPACE[2]));
        keys_live(&shell, cx, "a removal elsewhere");
        remote(&shell, cx, |m| {
            m.push(Member::Agent {
                name: "riko".into(),
            })
        });
        keys_live(&shell, cx, "an add elsewhere");
        on_text(&shell, cx);
        cx.simulate_keystrokes("cmd-w");
        draw(cx);
        keys_live(&shell, cx, "a close");
        for _ in 0..5 {
            cx.simulate_keystrokes("cmd-w");
            draw(cx);
        }
        assert_eq!(dock(&shell, cx), "", "every tab closed");
        keys_live(&shell, cx, "the last tab closed");
    }

    fn at(cx: &mut VisualTestContext, id: &str) -> Option<gpui_kit::Bounds<gpui_kit::Pixels>> {
        let id = gpui_kit::ElementId::Name(id.to_string().into());
        cx.update(|window, _| window.try_find(id).map(|e| e.bounds()))
    }

    /// Wholly inside the strip's scrolled view.
    fn in_view(cx: &mut VisualTestContext, agent: &str) -> bool {
        let view = at(cx, "tab-scroll").expect("the strip");
        let tab = at(cx, &format!("tab-{agent}")).expect("the tab");
        tab.left() >= view.left() && tab.right() <= view.right()
    }

    /// +N lists the tabs not wholly in view, by place, wherever the strip is scrolled to.
    #[test]
    fn the_overflow_lists_the_tabs_out_of_view() {
        use crate::views::tabs::out_of_view;
        // Four 100-wide tabs from x 10, a view from 10 to 260: two and a half fit.
        let tabs = || (0..4).map(|i| Some((px(10. + 100. * i as f32), px(110. + 100. * i as f32))));
        let view = (px(10.), px(260.));
        assert_eq!(
            out_of_view(view, px(0.), tabs()),
            vec![2, 3],
            "the cut one is out"
        );
        assert_eq!(
            out_of_view(view, px(-150.), tabs()),
            vec![0, 1],
            "scrolled to the end"
        );
        assert_eq!(
            out_of_view(view, px(-100.), tabs()),
            vec![0, 3],
            "both ends"
        );
        assert_eq!(
            out_of_view(view, px(-99.7), tabs()),
            vec![0, 3],
            "half a pixel is in"
        );
        let unknown = tabs().take(2).chain([None, Some((px(310.), px(410.)))]);
        assert_eq!(
            out_of_view(view, px(0.), unknown),
            vec![3],
            "unlaid ones are not"
        );
        assert!(
            out_of_view((px(0.), px(0.)), px(0.), tabs()).is_empty(),
            "nor in an unlaid view"
        );
    }

    /// Perps's three tabs in a window too narrow for them keep their width and scroll (S2); the shown
    /// tab changing scrolls it into view.
    #[gpui_kit::test]
    fn the_shown_tab_scrolls_into_view(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        let widths = |cx: &mut VisualTestContext| {
            SPACE.map(|a| at(cx, &format!("tab-{a}")).unwrap().size.width)
        };
        let wide = widths(cx);
        cx.simulate_resize(size(px(420.), px(900.)));
        draw(cx);
        draw(cx);
        assert_eq!(widths(cx), wide, "the tabs keep their width");
        assert!(in_view(cx, SPACE[0]), "the first in view");
        assert!(!in_view(cx, SPACE[2]), "the third out of view");
        act(cx, space::Tab(SPACE[2].into()));
        draw(cx);
        assert_eq!(
            dock(&shell, cx),
            format!("{} {} [{}*]", SPACE[0], SPACE[1], SPACE[2])
        );
        assert!(in_view(cx, SPACE[2]), "scrolled to the shown one");
        assert!(!in_view(cx, SPACE[0]), "the first scrolled away");
        act(cx, space::Tab(SPACE[0].into()));
        draw(cx);
        assert!(in_view(cx, SPACE[0]), "and back");
        act(cx, space::Tab(SPACE[1].into()));
        draw(cx);
        assert!(in_view(cx, SPACE[1]), "the middle one too");
        let (max, view) = (at(cx, "tab-max").unwrap(), at(cx, "tab-scroll").unwrap());
        assert!(
            max.left() >= view.right() && max.right() <= px(420.),
            "the □ stays in the window"
        );
    }

    /// A group opened narrow on a tab out of view at first scrolls to it once laid out.
    #[gpui_kit::test]
    fn a_group_opened_narrow_reveals_its_shown_tab(cx: &mut TestAppContext) {
        let (shell, cx) = boot(cx, Layouts::default(), SPACE[2], 420.);
        for _ in 0..5 {
            draw(cx);
        }
        assert_eq!(
            dock(&shell, cx),
            format!("{} {} [{}*]", SPACE[0], SPACE[1], SPACE[2])
        );
        assert!(in_view(cx, SPACE[2]), "the shown tab in view");
        assert!(!in_view(cx, SPACE[0]), "the first scrolled away");
    }

    /// Picking under +N the shown tab, narrowed out of view, scrolls to it: the real button and menu.
    #[gpui_kit::test]
    fn picking_the_shown_tab_under_more_reveals_it(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        cx.simulate_resize(size(px(420.), px(900.)));
        draw(cx);
        draw(cx);
        assert!(!in_view(cx, SPACE[1]), "the shown tab narrowed out of view");
        let more = at(cx, "tab-more").expect("+N");
        click(cx, more.center().x.into(), more.center().y.into());
        cx.simulate_keystrokes("down");
        draw(cx);
        cx.simulate_keystrokes("enter");
        for _ in 0..5 {
            draw(cx);
        }
        assert_eq!(
            dock(&shell, cx),
            format!("{} [{}*] {}", SPACE[0], SPACE[1], SPACE[2])
        );
        assert!(in_view(cx, SPACE[1]), "picked, it is scrolled to");
    }

    /// A short preview replaced by a wider one: the new one is scrolled to once it is laid out in its
    /// own width, not judged by the old one's place (fido's review).
    #[gpui_kit::test]
    fn a_wider_preview_replacing_a_short_one_is_revealed(cx: &mut TestAppContext) {
        let (shell, cx) = boot(cx, Layouts::default(), SPACE[1], 710.);
        act(cx, OpenLink("herder-agent:mupu".into(), false));
        for _ in 0..6 {
            draw(cx);
        }
        assert!(in_view(cx, "mupu"), "the short preview in view");
        act(
            cx,
            OpenLink("herder-agent:fees-program-design-lifo".into(), false),
        );
        for _ in 0..8 {
            draw(cx);
        }
        assert!(dock(&shell, cx).contains("[fees-program-design-lifo*~]"));
        assert!(in_view(cx, "fees-program-design-lifo"), "the wider one too");
    }

    /// A tab wider than the strip is never wholly in view: its reveal gives up, and the strip settles,
    /// asking for no more frames.
    #[gpui_kit::test]
    fn an_oversized_tab_settles(cx: &mut TestAppContext) {
        use gpui_kit::component::dock::TabGroup;
        let asked = std::rc::Rc::new(std::cell::Cell::new(0));
        let count = asked.clone();
        let _watch = cx.update(|cx| {
            cx.observe_new(move |_: &mut TabGroup, _, cx| {
                let count = count.clone();
                cx.observe_self(move |_, _| count.set(count.get() + 1))
                    .detach();
            })
        });
        let (_shell, cx) = boot(cx, Layouts::default(), SPACE[0], 150.);
        for _ in 0..15 {
            draw(cx);
        }
        assert!(asked.get() > 0, "the strip drew again while it settled");
        asked.set(0);
        for _ in 0..20 {
            draw(cx);
        }
        assert_eq!(asked.get(), 0, "settled");
        assert!(at(cx, "tab-max").is_some(), "the □ stays");
    }

    /// The tabs keep their width whatever the pointer or the agents' status do (S3 A2): the dot and ×
    /// slots are always there, and only the shown tab is medium. (Blocked needs the operator: its pill
    /// is new content.)
    #[gpui_kit::test]
    fn a_tab_keeps_its_width_through_hover_and_news(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        let widths = |cx: &mut VisualTestContext| {
            SPACE.map(|a| at(cx, &format!("tab-{a}")).unwrap().size.width)
        };
        let wide = widths(cx);
        let tab = at(cx, &format!("tab-{}", SPACE[0])).unwrap();
        cx.simulate_mouse_move(tab.center(), None, Modifiers::none());
        draw(cx);
        assert!(at(cx, &format!("tab-close-{}", SPACE[0])).is_some());
        assert_eq!(widths(cx), wide, "hovered");
        let news = |cx: &mut VisualTestContext| {
            SPACE.map(|a| {
                let id = gpui_kit::ElementId::Name(format!("tab-dot-{a}").into());
                cx.update(|window, _| window.find(id).label().map(String::from))
            })
        };
        for bus in ["active", "listening", "idle", "blocked", "active"] {
            shell.update(cx, |s, cx| {
                for agent in SPACE {
                    s.store.fleet.agents.get_mut(agent).unwrap().bus_status = bus.into();
                }
                cx.notify();
            });
            draw(cx);
            let dot = matches!(bus, "active" | "blocked").then(|| bus.to_string());
            assert_eq!(
                news(cx),
                [dot.clone(), dot.clone(), dot],
                "a dot only for news"
            );
            if bus != "blocked" {
                assert_eq!(widths(cx), wide, "{bus}");
            }
        }
    }

    /// The scroller fades on a side only where it cuts tabs (S3 A6).
    #[gpui_kit::test]
    fn the_strip_fades_where_it_cuts_tabs(cx: &mut TestAppContext) {
        let (_shell, cx) = open(cx);
        let fades = |cx: &mut VisualTestContext| {
            let at = |cx: &mut VisualTestContext, id: &str| at(cx, id).is_some();
            (at(cx, "tab-fade-left"), at(cx, "tab-fade-right"))
        };
        assert_eq!(fades(cx), (false, false), "all in view");
        cx.simulate_resize(size(px(420.), px(900.)));
        draw(cx);
        draw(cx);
        assert_eq!(fades(cx), (false, true), "the end cut");
        act(cx, space::Tab(SPACE[0].into()));
        draw(cx);
        act(cx, space::Tab(SPACE[1].into()));
        for _ in 0..3 {
            draw(cx);
        }
        assert!(in_view(cx, SPACE[1]));
        assert_eq!(fades(cx), (true, true), "the middle one: both cut");
        act(cx, space::Tab(SPACE[2].into()));
        for _ in 0..3 {
            draw(cx);
        }
        assert_eq!(fades(cx), (true, false), "scrolled to the end");
    }

    /// Maximized, the group's ⤢ is a selected ⤡ and the keys say ⌥⏎ restores (S3 A7).
    #[gpui_kit::test]
    fn the_maximized_group_shows_restore(cx: &mut TestAppContext) {
        let (_shell, cx) = open(cx);
        let state = |cx: &mut VisualTestContext| {
            cx.update(|window, _| {
                let max = window.find(gpui_kit::ElementId::Name("tab-max".into()));
                let keys = window.find(gpui_kit::ElementId::Name("crumb-keys".into()));
                let restore = keys.label().unwrap().ends_with("⌥⏎ restore");
                (max.label().map(String::from), max.selected(), restore)
            })
        };
        let maximize = (Some("Maximize".into()), Some(false), false);
        assert_eq!(state(cx), maximize);
        let max = at(cx, "tab-max").unwrap();
        click(cx, max.center().x.into(), max.center().y.into());
        draw(cx);
        assert_eq!(state(cx), (Some("Restore".into()), Some(true), true));
        cx.simulate_keystrokes("alt-enter");
        draw(cx);
        assert_eq!(state(cx), maximize, "and back by the key");
    }

    /// A tab's × is there only under the pointer, and closes it.
    #[gpui_kit::test]
    fn a_tab_has_its_close_only_on_hover(cx: &mut TestAppContext) {
        let (shell, cx) = open(cx);
        let close = |agent: &str| format!("tab-close-{agent}");
        assert!(
            at(cx, &close(SPACE[0])).is_none(),
            "no × away from the pointer"
        );
        assert!(at(cx, &close(SPACE[1])).is_none(), "nor on the shown one");
        let tab = at(cx, &format!("tab-{}", SPACE[0])).unwrap();
        cx.simulate_mouse_move(tab.center(), None, Modifiers::none());
        draw(cx);
        assert!(at(cx, &close(SPACE[0])).is_some(), "× on the hovered tab");
        assert!(at(cx, &close(SPACE[1])).is_none(), "and only there");
        let x = at(cx, &close(SPACE[0])).unwrap();
        assert!(tab.contains(&x.center()), "inside its tab, no wider");
        cx.simulate_mouse_move(point(px(700.), px(500.)), None, Modifiers::none());
        draw(cx);
        assert!(
            at(cx, &close(SPACE[0])).is_none(),
            "gone when the pointer leaves"
        );
        cx.simulate_mouse_move(tab.center(), None, Modifiers::none());
        draw(cx);
        click(cx, x.center().x.into(), x.center().y.into());
        assert_eq!(
            dock(&shell, cx),
            format!("[{}*] {}", SPACE[1], SPACE[2]),
            "its × closes it"
        );
    }
}

/// G3: a clicked path's choices with real keys and a press in a headless window: they take focus when
/// they land, `↑` `↓` move (clamped), `⏎` opens the one under the cursor, `esc` and a press elsewhere
/// close them; each time focus is back on the panel.
mod paths_events {
    use crate::api::{Candidate, ResolveRoot, Resolved};
    use crate::store::tests::transcript_pages::{drive, history};
    use crate::store::tests::{board, fleet_frame, loaded};
    use crate::store::transcript::{Got, Step, What};
    use crate::store::{Effect, Event, Fetch, Store, spaces};
    use crate::views::lens::Ui;
    use crate::views::space::Zoom;
    use crate::views::{Host, bind, dock, paths, theme, transcript};
    use gpui_kit::test::TestWindowExt as _;
    use gpui_kit::{
        Context, Entity, InteractiveElement as _, IntoElement, Modifiers, MouseButton,
        MouseDownEvent, MouseUpEvent, ParentElement as _, Render, Styled as _, TestAppContext,
        VisualTestContext, Window, div, point, px, size,
    };

    struct Shell {
        store: Store,
        ui: Ui,
        effects: Vec<Effect>,
    }

    impl Host for Shell {
        fn parts(&mut self) -> (&Store, &mut Ui) {
            (&self.store, &mut self.ui)
        }

        fn view(&self) -> (&Store, &Ui) {
            (&self.store, &self.ui)
        }

        fn dispatch(&mut self, event: Event, cx: &mut Context<Self>) {
            if matches!(event, Event::Transcript(Step::Choose { .. })) {
                self.effects
                    .extend(transcript::reduce(&mut self.store, &self.ui, event));
                cx.notify();
            }
        }

        fn copy(&mut self, _: String, _: &mut Context<Self>) {}
    }

    impl Render for Shell {
        fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
            let _ = dock::sync(&mut self.ui, &self.store, window, cx);
            paths::sync(&mut self.ui, &self.store, window, cx);
            let (store, ui, t) = (&self.store, &self.ui, theme::type_scale(1.));
            let panel = ui.panel().unwrap();
            let at = panel.transcript.pressed();
            let space = div().key_context("Space").size_full().child(
                div()
                    .track_focus(&panel.focus)
                    .size_full()
                    .flex()
                    .flex_col()
                    .child(transcript::render(store, ui, "mupu", t, cx))
                    .children(paths::render(store, ui, "mupu", at, t, cx)),
            );
            div().size_full().key_context("Lens").child(space)
        }
    }

    fn candidate(root: &str) -> Candidate {
        Candidate {
            root: root.into(),
            path: "src/x.rs".into(),
            kind: "file".into(),
            tier: "suffix".into(),
            score: 0,
        }
    }

    /// Click `src/x.rs:4` and answer with two strong matches in two worktrees.
    fn offer(shell: &Entity<Shell>, cx: &mut VisualTestContext) {
        shell.update(cx, |s, cx| {
            let click = Step::OpenPath {
                agent: "mupu".into(),
                mention: "src/x.rs:4".into(),
            };
            let effects = s.store.apply(Event::Transcript(click));
            let Some(Effect::Fetch(Fetch::Transcript(read))) = effects.into_iter().next() else {
                panic!("no resolve")
            };
            assert!(matches!(read.what, What::Resolve { .. }));
            let roots = ["/w/a", "/w/b"].map(|root| ResolveRoot {
                root: root.into(),
                status: "complete".into(),
            });
            let got = Got::Resolved(Resolved {
                candidates: vec![candidate("/w/a"), candidate("/w/b")],
                roots: roots.into(),
            });
            assert!(
                s.store
                    .apply(Event::Transcript(Step::Read(read, Ok(got))))
                    .is_empty()
            );
            cx.notify();
        });
        draw(cx);
    }

    fn draw(cx: &mut VisualTestContext) {
        cx.run_until_parked();
        cx.update(|window, cx| window.render_frame(cx));
    }

    /// (choices up, the picker focused, the panel focused, its cursor)
    fn state(shell: &Entity<Shell>, cx: &mut VisualTestContext) -> (bool, bool, bool, usize) {
        cx.update(|window, cx| {
            let s = shell.read(cx);
            let up = s.store.transcript.focused().unwrap().choices.is_some();
            let p = s.ui.panel().unwrap();
            let (picker, panel) = (p.paths.focus.is_focused(window), p.focus.is_focused(window));
            (up, picker, panel, p.paths.cursor)
        })
    }

    fn opened(root: &str) -> Effect {
        Effect::OpenFile {
            root: root.into(),
            file: Some("src/x.rs".into()),
            line: Some(4),
        }
    }

    #[gpui_kit::test]
    fn the_choices_take_keys_and_give_focus_back(cx: &mut TestAppContext) {
        cx.update(|cx| {
            theme::seed(cx);
            gpui_kit::init(cx);
            theme::dark(cx);
            bind(cx);
        });
        let made = std::rc::Rc::new(std::cell::RefCell::new(None));
        let keep = made.clone();
        let (_, cx) = cx.add_window_view(move |window, cx| {
            let shell = gpui_kit::AppContext::new(cx, |cx| {
                let mut store = loaded();
                store.apply(fleet_frame(board()));
                let space = store.spaces[0].id.clone();
                let view = spaces::Move::View {
                    space: space.clone(),
                    agent: Some("mupu".into()),
                    beside: Vec::new(),
                };
                let effects = store.apply(Event::Lens(view));
                drive(&mut store, effects, &history("mupu"), usize::MAX);
                let mut ui = Ui::new(cx);
                let agent = Some("mupu".into());
                ui.zoom = Some(Zoom { space, agent });
                let effects = Vec::new();
                Shell { store, ui, effects }
            });
            *keep.borrow_mut() = Some(shell.clone());
            gpui_kit::base::Root::new(shell, window, cx)
        });
        let shell = made.borrow_mut().take().unwrap();
        cx.simulate_resize(size(px(1400.), px(900.)));
        // Active, so focus leaving the choices reports their blur.
        cx.update(|window, _| window.activate_window());
        draw(cx);
        let focus = shell.read_with(cx, |s, _| s.ui.panel().unwrap().focus.clone());
        cx.update(|window, cx| window.focus(&focus, cx));
        draw(cx);
        offer(&shell, cx);
        assert_eq!(state(&shell, cx), (true, true, false, 0), "landed: focused");
        for (key, cursor) in [("up", 0), ("down", 1), ("down", 1), ("up", 0), ("down", 1)] {
            cx.simulate_keystrokes(key);
            draw(cx);
            assert_eq!(state(&shell, cx), (true, true, false, cursor), "{key}");
        }
        cx.simulate_keystrokes("enter");
        draw(cx);
        assert_eq!(state(&shell, cx), (false, false, true, 1));
        let effects = shell.update(cx, |s, _| std::mem::take(&mut s.effects));
        assert_eq!(effects, vec![opened("/w/b")]);
        // A fresh offer starts on the first; esc closes it, opening nothing.
        offer(&shell, cx);
        assert_eq!(state(&shell, cx), (true, true, false, 0));
        cx.simulate_keystrokes("escape");
        draw(cx);
        assert_eq!(state(&shell, cx), (false, false, true, 0));
        cx.simulate_keystrokes("enter");
        draw(cx);
        let effects = shell.update(cx, |s, _| std::mem::take(&mut s.effects));
        assert_eq!(effects, vec![]);
        // A press anywhere else closes it too.
        offer(&shell, cx);
        let (m, at) = (Modifiers::default(), point(px(1300.), px(850.)));
        cx.simulate_event(MouseDownEvent {
            button: MouseButton::Left,
            position: at,
            modifiers: m,
            click_count: 1,
            first_mouse: false,
        });
        cx.simulate_event(MouseUpEvent {
            button: MouseButton::Left,
            position: at,
            modifiers: m,
            click_count: 1,
        });
        draw(cx);
        assert_eq!(state(&shell, cx), (false, false, true, 0));
        let effects = shell.update(cx, |s, _| std::mem::take(&mut s.effects));
        assert_eq!(effects, vec![]);
        // A real click on a row (hit-tested, not the harness's action) opens it, focus back on the panel.
        offer(&shell, cx);
        let row = cx.update(|window, _| {
            window
                .find(gpui_kit::ElementId::NamedInteger("path".into(), 0))
                .bounds()
        });
        cx.simulate_click(row.center(), Modifiers::default());
        draw(cx);
        assert_eq!(
            state(&shell, cx),
            (false, false, true, 0),
            "a row clicked gives focus back"
        );
        let effects = shell.update(cx, |s, _| std::mem::take(&mut s.effects));
        assert_eq!(effects, vec![opened("/w/a")]);
        // Focus moving away closes them, with no press outside.
        offer(&shell, cx);
        cx.update(|window, cx| window.focus(&focus, cx));
        draw(cx);
        draw(cx);
        assert_eq!(
            state(&shell, cx),
            (false, false, true, 0),
            "blur closes the picker"
        );
        // Choices gone while they hold focus (a reset) give focus back to the panel.
        offer(&shell, cx);
        shell.update(cx, |s, cx| {
            s.store.transcript.open.get_mut("mupu").unwrap().choices = None;
            cx.notify();
        });
        draw(cx);
        assert_eq!(
            state(&shell, cx),
            (false, false, true, 0),
            "choices gone give focus back"
        );
    }
}
