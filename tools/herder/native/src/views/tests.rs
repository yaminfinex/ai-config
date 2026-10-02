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

/// F2: where regrouped rows splice into the list, and which runs stay open as pages land.
mod runs {
    use crate::store::condense::{self, Row};
    use crate::store::tests::loaded;
    use crate::store::tests::transcript_pages::{drive, history, items, open, reference, wake};
    use crate::store::transcript::{Key, Step};
    use crate::store::{Event, Store};
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
            let effects = store.apply(Event::Transcript(Step::Older));
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
        view.sync(store.transcript.open.as_ref().unwrap(), store);
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
            let effects = store.apply(Event::Transcript(Step::Older));
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
    use crate::store::condense::{self, Seg};
    use crate::store::tests::loaded;
    use crate::store::tests::transcript_pages::{drive, history, items, open};
    use crate::store::transcript::{Item, Key, Step};
    use crate::store::{Event, Store};
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
            view.sync(store.transcript.open.as_ref().unwrap(), &store);
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
            let effects = store.apply(Event::Transcript(Step::Older));
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
    use crate::store::tests::loaded;
    use crate::store::tests::transcript_pages::{self, drive, history, items, open, reference};
    use crate::store::tests::{board, bump, frame};
    use crate::store::transcript::{Item, Key, Step};
    use crate::store::{Event, Store};
    use crate::views::lens::Ui;
    use crate::views::space::Zoom;
    use crate::views::transcript::{self, Fold, Mark};
    use crate::views::{Host, theme};
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
        /// Reduce what the view dispatches, as the shell does (else the test reads the pages itself).
        reduce: bool,
    }

    impl Host for Body {
        fn parts(&mut self) -> (&Store, &mut Ui) {
            (&self.store, &mut self.ui)
        }

        fn view(&self) -> (&Store, &Ui) {
            (&self.store, &self.ui)
        }

        fn dispatch(&mut self, event: Event, cx: &mut Context<Self>) {
            if self.reduce {
                drop(transcript::reduce(&mut self.store, &self.ui, event));
                cx.notify();
            }
        }

        fn copy(&mut self, _: String, _: &mut Context<Self>) {}
    }

    impl Render for Body {
        fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
            let zoom = self.ui.zoom.clone().unwrap();
            let t = theme::type_scale(1.);
            let body = transcript::render(&self.store, &self.ui, &zoom, t, cx);
            div().size_full().flex().flex_col().child(body)
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
        let (body, cx) = cx.add_window_view(move |window, cx| {
            let mut ui = Ui::new(window, cx);
            let (space, agent) = ("none".into(), Some(agent.into()));
            ui.zoom = Some(Zoom { space, agent });
            Body {
                store,
                ui,
                reduce: false,
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
        body.read_with(cx, |b, _| b.ui.transcript.painted.borrow().rows[&ix])
    }

    /// Where `was` (a member, or a pill holding its key) last laid out.
    fn mark(body: &Entity<Body>, was: Mark, cx: &mut VisualTestContext) -> Option<Bounds<Pixels>> {
        body.read_with(cx, |b, _| {
            let painted = b.ui.transcript.painted.borrow();
            painted
                .marks
                .iter()
                .find(|(m, _)| m.is(was))
                .map(|(_, b)| *b)
        })
    }

    fn screen(body: &Entity<Body>, cx: &mut VisualTestContext) -> Bounds<Pixels> {
        body.read_with(cx, |b, _| b.ui.transcript.list.viewport_bounds())
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
            let effects = b.store.apply(Event::Transcript(Step::Older));
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
            let list = &b.ui.transcript.list;
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
        body.read_with(cx, |b, _| b.ui.transcript.toggle((first, last), 0));
        scroll(body, px(0.), cx);
        let keys: Vec<Key> = body.read_with(cx, |b, _| {
            items(&b.store)
                .range(first..=last)
                .map(|(k, _)| *k)
                .collect()
        });
        let most = body.read_with(cx, |b, _| b.ui.transcript.list.max_offset_for_scrollbar().y);
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
        let pills = body.read_with(cx, |b, _| b.ui.transcript.painted.borrow().marks.clone());
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
            let list = &b.ui.transcript.list;
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
                b.ui.transcript.list.scroll_to(at);
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
            b.store.apply(b.ui.transcript.tail(true));
            bump(&mut board, "mupu", 1);
            let landed = frame(&b.store, board.clone());
            transcript::reduce(&mut b.store, &b.ui, landed);
            assert!(!b.store.agent_needs_you("mupu"), "watched as it lands");
            // The scrollbar's handle moves the list without its scroll handler.
            let list = &b.ui.transcript.list;
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
        body.read_with(cx, |b, _| transcript::toggle_lowest(&b.ui));
        let (rows, _, open) = body.read_with(cx, |b, _| b.ui.transcript.census());
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
        let screen = body.read_with(cx, |b, _| b.ui.transcript.list.viewport_bounds());
        let run = body.read_with(cx, |b, _| {
            b.ui.transcript.painted.borrow().rows.get(&(n - 2)).copied()
        });
        assert!(
            run.is_none_or(|b| b.bottom() <= screen.top()),
            "the run is off screen"
        );
        body.read_with(cx, |b, _| transcript::toggle_lowest(&b.ui));
        assert_eq!(body.read_with(cx, |b, _| b.ui.transcript.census().2), 0);
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
            let tr = b.store.transcript.open.as_ref().unwrap();
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
            b.ui.transcript.toggle((first, last), ix);
            b.ui.transcript.fold(Fold(key, 0));
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
                b.ui.transcript.list.scroll_to(ListOffset {
                    item_ix: usize::MAX,
                    offset_in_item: px(0.),
                });
                let event = b.ui.transcript.tail(true);
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
                let tail = b.store.transcript.open.as_ref().unwrap().tail;
                (tail, b.ui.transcript.list.is_following_tail())
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
            let tr = b.store.transcript.open.as_ref().unwrap();
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
            b.ui.transcript.toggle((first, last), ix);
            b.ui.transcript.fold(Fold(key, 0));
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
            b.ui.transcript.fold(Fold(key, 0));
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
    use crate::store::condense;
    use crate::store::tests::loaded;
    use crate::store::tests::transcript_pages::{drive, history, items, open};
    use crate::store::transcript::{Item, Step, ToolResult};
    use crate::store::{Event, Store};
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
        view.sync(store.transcript.open.as_ref().unwrap(), &store);
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
            view.sync(store.transcript.open.as_ref().unwrap(), &store);
            assert_eq!(view.tools(items(&store)), (1, 1), "open with its output");
            regrouped |= tool_row(&store, key) != was;
            let effects = store.apply(Event::Transcript(Step::Older));
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
