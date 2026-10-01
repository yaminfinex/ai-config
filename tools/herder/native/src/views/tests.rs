//! The lens and zoom key logic against the fixture store: which moves reach the store, where the
//! selection and zoom go, and when the selected card is revealed. Drawing is the harness's job.

use crate::store::spaces::{Move, Row, Space};
use crate::store::tests::{board, bump, fleet_frame, loaded, space_of};
use crate::store::{Event, Store};
use crate::views::lens::{self, Nav, State};
use crate::views::space::{self, Zoomed};

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
    let first = store.next_needing(None).unwrap();
    assert_eq!(ui.selected(&store).map(|s| &s.id), Some(&first.id));
    assert!(ui.reveal.get(), "the selection is revealed");

    lens::act(&store, &mut ui, Nav::ZoomIn);
    let next = store.next_needing(Some(&first.id)).unwrap();
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
