//! The lens and zoom key logic against the fixture store: which moves reach the store, where the
//! selection and zoom go, and when the selected card is revealed. Drawing is the harness's job.

use crate::api::Entries;
use crate::store::spaces::{Move, Row, Space, Stop};
use crate::store::tests::{board, bump, fleet_frame, frame, loaded, space_of};
use crate::store::transcript::{Got, Step, What};
use crate::store::{Effect, Event, Fetch, Store};
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

/// The `View` moves among `events`.
fn viewed(events: Vec<Event>) -> Vec<(String, Option<String>)> {
    let views = events.into_iter().filter_map(|e| match e {
        Event::Lens(Move::View { space, agent }) => Some((space, agent)),
        _ => None,
    });
    views.collect()
}

#[test]
fn zooming_in_views_the_agent_needing_you_and_tabs_view_the_next() {
    let store = store();
    let herder = space_of(&store, "orch-lega").clone();
    let mut ui = State::default();
    let events = space::zoom_into(&store, &mut ui, &herder, None);
    assert_eq!(viewed(events), [view(&herder, "orch-lega")]);
    assert_eq!(zoomed(&ui), Some((herder.id.as_str(), Some("orch-lega"))));
    assert!(
        ui.anim.as_ref().is_some_and(|a| a.morphs()),
        "enter morphs from the card"
    );

    assert_eq!(
        viewed(space::act(&store, &mut ui, Zoomed::Agent(1))),
        [view(&herder, "conductor-line")]
    );
    assert_eq!(
        viewed(space::act(&store, &mut ui, Zoomed::Agent(1))),
        [view(&herder, "orch-lega")],
        "wraps"
    );

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
    assert!(matches!(
        events.as_slice(),
        [Event::Lens(Move::View { .. })]
    ));
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
    assert_eq!(viewed(events), [(String::new(), Some(alone.into()))]);
    assert_eq!(zoomed(&ui), Some(("", Some(alone))));
    // Zoomed, `n` goes on round: the first space.
    let events = lens::next_needing(&store, &mut ui, false);
    assert_eq!(viewed(events).len(), 1);
    assert_eq!(zoomed(&ui).map(|z| z.0), Some(order[0].as_str()));
}

#[test]
fn membership_changing_while_zoomed() {
    let mut store = store();
    let herder = space_of(&store, "orch-lega").clone();
    let mut ui = State::default();
    space::zoom_into(&store, &mut ui, &herder, None);

    // The zoomed agent leaves the space: tab goes to the first agent still in it.
    let at = store.spaces.iter().position(|s| s.id == herder.id).unwrap();
    store.spaces[at]
        .members
        .retain(|m| !matches!(m, crate::api::Member::Agent { name } if name == "orch-lega"));
    assert_eq!(
        viewed(space::act(&store, &mut ui, Zoomed::Agent(1))),
        [view(&herder, "conductor-line")]
    );

    // The space itself goes: any zoom key morphs back to the lens.
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
    let effects = store.apply(Event::Lens(Move::View { space, agent }));
    let read = effects.into_iter().find_map(|e| match e {
        Effect::Fetch(Fetch::Transcript(r)) if matches!(r.what, What::Page(_)) => Some(r),
        _ => None,
    });
    let tail = include_str!("../../testdata/agents/mupu/tail.json");
    let page: Entries = serde_json::from_str(tail).expect("entries fixture decodes");
    let got = Ok(Got::Page(Box::new(page)));
    store.apply(Event::Transcript(Step::Read(read.unwrap(), got)));
    // A render: the list mirrors the rows and follows the bottom, and says so.
    let mut ui = State::default();
    ui.transcript
        .sync(store.transcript.open.as_ref().unwrap(), &store);
    store.apply(ui.transcript.tail(true));
    bump(&mut b, "mupu", 1);
    store.apply(frame(&store, b.clone()));
    assert!(!store.agent_needs_you("mupu"), "watched as it lands");
    // `g`, then a fleet frame before any render.
    for event in transcript::scroll(&store, &mut ui, Scroll::Top) {
        store.apply(event);
    }
    bump(&mut b, "mupu", 1);
    store.apply(frame(&store, b));
    assert!(store.agent_needs_you("mupu"), "scrolled off the bottom");
}

mod links {
    use crate::views::markdown::{Mentions, link, path_like, route, vscode_url};

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
        assert_eq!(viewed(events), [view(&slack, "mupu")]);
        assert_eq!(zoomed(&ui), Some((slack.id.as_str(), Some("mupu"))));

        // Open elsewhere (a preview in another space): it moves to its own space.
        ui.zoom = Some(Zoom {
            space: chief.id.clone(),
            agent: Some("mupu".into()),
        });
        let events = space::summon(&store, &mut ui, "agent:mupu");
        assert_eq!(viewed(events), [view(&slack, "mupu")]);
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
            viewed(events),
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
        assert_eq!(viewed(events), [(String::new(), Some(alone.into()))]);
        assert_eq!(zoomed(&ui), Some(("", Some(alone))));
        assert!(ui.zoom.as_ref().is_some_and(Zoom::alone));
        assert_eq!(
            crate::views::probe::shown(&store, &ui),
            format!("{alone} preview")
        );
        assert_eq!(ui.selected(&store).map(|s| s.id.clone()), before);
        // Summoned again while open: still seen.
        let events = space::summon(&store, &mut ui, &format!("agent:{alone}"));
        assert_eq!(viewed(events), [(String::new(), Some(alone.into()))]);
        for key in [Zoomed::Agent(1), Zoomed::Space(1), Zoomed::Space(-1)] {
            assert!(space::act(&store, &mut ui, key).is_empty());
            assert_eq!(zoomed(&ui), Some(("", Some(alone))));
        }
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
        assert!(viewed(events).is_empty());
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
    use gpui_kit::component::text::TextView;
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
    use crate::views::{Host, bind, composer, on, theme};
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
            composer::sync(&mut self.ui, &self.store, window, cx);
            notes::sync(&mut self.ui, &self.store, window, cx);
            let (store, ui, t) = (&self.store, &self.ui, theme::type_scale(1.));
            let zoom = div()
                .id("space")
                .key_context("Space")
                .track_focus(&ui.zoom_focus)
                .on_action(on(cx, |store, ui, n: &Notes| notes::act(store, ui, n)))
                .on_action(on(cx, |store, ui, c: &Card| notes_list::act(store, ui, c)))
                .size_full()
                .flex()
                .flex_col()
                .children(notes::render(store, ui, "mupu", t, cx))
                .child(composer::render(store, ui, "mupu", t, cx));
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
        let (shell, cx) = cx.add_window_view(move |window, cx| {
            let store = zoomed("mupu", Some("listening"));
            let mut ui = Ui::new(window, cx);
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
            let picked = &s.ui.notes.list.picked;
            (
                picked.chosen(&ids),
                picked.cursor.clone(),
                s.ui.notes.said(),
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
                let focused = s.ui.notes.focus_handle(cx).is_focused(window);
                (
                    s.ui.notes.editing.is_some(),
                    focused,
                    s.ui.notes.text.clone(),
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
            let box_ = shell.read(cx).ui.composer.focus_handle(cx);
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
            let editing = s.ui.notes.editing.as_ref();
            editing.and_then(|e| e.note.as_ref()).map(|n| n.id.clone())
        });
        assert_eq!(edited.as_deref(), Some("b"));
    }

    #[gpui_kit::test]
    fn a_partial_hand_off_selects_the_next_note_once_it_lands_and_not_when_it_fails(
        cx: &mut TestAppContext,
    ) {
        let (shell, cx) = open(cx, &["a", "b", "c", "d"]);
        shell.update(cx, |s, _| s.ui.notes.open = true);
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
            let box_ = shell.read(cx).ui.composer.focus_handle(cx);
            window.focus(&box_, cx);
        });
        cx.update(|window, cx| window.press("up", cx));
        cx.run_until_parked();
        assert_eq!(state(&shell, cx).1.as_deref(), Some("c"));
    }
}
