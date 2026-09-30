//! Store reductions from the recorded fixtures: state and effects, no network, no clock.

use super::*;
use crate::api::{Hello, Member, StateChanged, StateRows};
use crate::store::fleet::Status;
use crate::store::spaces::Row;
use crate::store::sync::Hold;
use serde_json::json;

pub(crate) fn board() -> Board {
    serde_json::from_str(include_str!("../../testdata/fleet.json")).expect("fleet.json decodes")
}

fn fixture_rows(ns: Ns) -> StateRows {
    let text = match ns {
        Ns::Spaces => include_str!("../../testdata/state-spaces.json"),
        Ns::Members => include_str!("../../testdata/state-spaces.members.json"),
        Ns::Notes => include_str!("../../testdata/state-notes.json"),
    };
    serde_json::from_str(text).expect("state fixture decodes")
}

pub(crate) fn fleet_frame(board: Board) -> Event {
    Event::Stream {
        generation: 1,
        event: StreamEvent::Frame(Wire::Fleet(board)),
    }
}

fn hello(build: &str) -> Event {
    Event::Stream {
        generation: 1,
        event: StreamEvent::Frame(Wire::Hello(Hello {
            build_identity: build.into(),
        })),
    }
}

/// A store that has booted (stream generation 1) and pulled every namespace from the fixtures.
pub(crate) fn loaded() -> Store {
    let mut store = Store::default();
    store.apply(Event::Boot);
    for ns in Ns::ALL {
        store.apply(Event::Sync {
            ns,
            step: Step::Pulled(fixture_rows(ns)),
        });
    }
    store
}

fn row(key: &str, updated: i64, write_id: &str) -> StateRow {
    StateRow {
        key: key.into(),
        value: json!({"id": key, "name": key, "order": 1}),
        updated,
        write_id: write_id.into(),
        deleted: false,
    }
}

fn sends(effects: &[Effect]) -> Vec<Vec<StateRow>> {
    effects
        .iter()
        .filter_map(|e| match e {
            Effect::Send(Write::State { rows, .. }) => Some(rows.clone()),
            _ => None,
        })
        .collect()
}

fn pulls(effects: &[Effect]) -> Vec<(Ns, u64)> {
    effects
        .iter()
        .filter_map(|e| match e {
            Effect::Fetch(Fetch::State { ns, since }) => Some((*ns, *since)),
            _ => None,
        })
        .collect()
}

fn queued(store: &Store, ns: Ns) -> Vec<(String, i64)> {
    store.sync[&ns]
        .outbox
        .values()
        .map(|r| (r.key.clone(), r.updated))
        .collect()
}

#[test]
fn board_event_fills_the_fleet_and_a_drop_keeps_it() {
    let mut store = loaded();
    store.apply(hello("source:abc"));
    assert_eq!(
        store.conn,
        Conn::Live {
            build: "source:abc".into()
        }
    );
    let effects = store.apply(fleet_frame(board()));
    assert!(effects.contains(&Effect::Persist(Persist::Snapshot)));
    let mupu = &store.fleet.agents["mupu"];
    assert_eq!(mupu.tool, "claude");
    assert_eq!(mupu.status(), Status::Done);
    assert_eq!(mupu.workspace, "~");
    assert!(store.fleet.agents.len() > 10);

    store.apply(Event::Stream {
        generation: 1,
        event: StreamEvent::Dropped,
    });
    assert_eq!(store.conn, Conn::Offline);
    assert!(
        store.fleet.agents.contains_key("mupu"),
        "the last board survives a drop"
    );
}

#[test]
fn status_is_derived_blocked_first() {
    let mut a = fleet::Agent {
        herdr_status: "working".into(),
        bus_status: "blocked".into(),
        ..Default::default()
    };
    assert_eq!(a.status(), Status::Blocked);
    a.bus_status = "active".into();
    assert_eq!(a.status(), Status::Working);
    a.herdr_status = "idle".into();
    assert_eq!(a.status(), Status::Idle);
}

#[test]
fn spaces_are_ordered_with_members_and_tombstones_dropped() {
    let store = loaded();
    let raw = fixture_rows(Ns::Spaces);
    let dead: Vec<&str> = raw
        .rows
        .iter()
        .filter(|r| r.deleted)
        .map(|r| r.key.as_str())
        .collect();
    assert!(!dead.is_empty(), "the fixture has tombstones");
    assert_eq!(store.spaces.len(), raw.rows.len() - dead.len());
    assert!(store.spaces.iter().all(|s| !dead.contains(&s.id.as_str())));
    assert!(
        store
            .spaces
            .windows(2)
            .all(|w| (w[0].order, &w[0].id) <= (w[1].order, &w[1].id)),
        "lens order is (order, id)"
    );
    let with_mupu = store
        .spaces
        .iter()
        .find(|s| s.agents().any(|a| a == "mupu"))
        .expect("mupu is in a space");
    assert_eq!(
        with_mupu.members,
        [
            Member::Agent {
                name: "mupu".into()
            },
            Member::Agent {
                name: "support-mifa".into()
            }
        ],
        "dock order kept"
    );
}

#[test]
fn members_are_unique_and_follow_their_row() {
    let mut store = loaded();
    let space = store.spaces[0].id.clone();
    let members = StateRow {
        key: space.clone(),
        value: json!({"members": [
            {"kind": "agent", "name": "b"},
            {"kind": "file", "root": "r", "path": "p"},
            {"kind": "agent", "name": "b"},
            {"kind": "agent", "name": "a"},
        ]}),
        updated: i64::MAX,
        write_id: "w".into(),
        deleted: false,
    };
    store.apply(Event::Sync {
        ns: Ns::Members,
        step: Step::Pulled(StateRows {
            rows: vec![members],
            rev: u64::MAX,
        }),
    });
    let names: Vec<&str> = store
        .spaces
        .iter()
        .find(|s| s.id == space)
        .unwrap()
        .agents()
        .collect();
    assert_eq!(names, ["b", "a"]);
}

#[test]
fn notes_are_the_live_records() {
    let store = loaded();
    let raw = fixture_rows(Ns::Notes);
    let live = raw.rows.iter().filter(|r| !r.deleted).count();
    assert_eq!(store.notes.len(), live);
    assert!(store.notes.windows(2).all(|w| w[0].created <= w[1].created));
    assert!(store.notes.iter().all(|n| !n.group.is_empty()));
}

#[test]
fn needs_you_follows_seen_marks() {
    let mut store = loaded();
    let mut b = board();
    store.apply(fleet_frame(b.clone()));
    let space = store
        .spaces
        .iter()
        .find(|s| s.agents().any(|a| a == "mupu"))
        .unwrap()
        .clone();
    let turn = store.fleet.agents["mupu"]
        .turn_end
        .expect("mupu has finished a turn");
    assert_eq!(
        store.prefs.seen["mupu"].turn_end, turn,
        "first sight seeds the baseline"
    );
    assert_eq!(store.needs_you(&space), 0);

    // mupu finishes another turn.
    let pane = b.workspaces[0].tabs[0]
        .panes
        .iter_mut()
        .find(|p| p.agent == "mupu")
        .unwrap();
    pane.turn_end_id = Some(turn + 5);
    store.apply(fleet_frame(b.clone()));
    assert_eq!(store.needs_you(&space), 1);

    let view = spaces::Move::View {
        space: space.id.clone(),
        agent: Some("mupu".into()),
    };
    let effects = store.apply(Event::Lens(view));
    assert_eq!(effects, [Effect::Persist(Persist::Prefs)]);
    assert_eq!(store.needs_you(&space), 0);

    // A working agent does not need you, whatever its marks say.
    let pane = b.workspaces[0].tabs[0]
        .panes
        .iter_mut()
        .find(|p| p.agent == "mupu")
        .unwrap();
    pane.turn_end_id = Some(turn + 9);
    pane.herdr_status = "working".into();
    store.apply(fleet_frame(b));
    assert_eq!(store.needs_you(&space), 0);
}

#[test]
fn a_snapshot_paints_first_and_never_lands_on_live_data() {
    let mut old = board();
    old.workspaces.clear();
    old.unplaced.clear();
    let snapshot = Snapshot {
        board: board(),
        rows: [(Ns::Spaces, fixture_rows(Ns::Spaces).rows)].into(),
    };

    let mut store = Store::default();
    store.apply(Event::Snapshot(snapshot.clone()));
    assert!(store.fleet.agents.contains_key("mupu"));
    assert!(!store.spaces.is_empty(), "spaces paint from the snapshot");

    store.apply(Event::Boot);
    store.apply(fleet_frame(old.clone()));
    assert!(store.fleet.agents.is_empty());
    store.apply(Event::Snapshot(snapshot));
    assert!(
        store.fleet.agents.is_empty(),
        "a snapshot after live data is refused"
    );
}

#[test]
fn a_post_retires_what_it_sent_but_keeps_newer_edits() {
    let mut store = loaded();
    let effects = store.apply(Event::Sync {
        ns: Ns::Spaces,
        step: Step::Edit(vec![row("s1", 10, "a")]),
    });
    // The save itself precedes every send in the shell (`save_then_send`, tests/fake_server.rs).
    assert!(effects.contains(&Effect::Persist(Persist::Outbox)));
    assert_eq!(sends(&effects), [vec![row("s1", 10, "a")]]);

    // Edited again while the POST is in flight: no second POST yet.
    let effects = store.apply(Event::Sync {
        ns: Ns::Spaces,
        step: Step::Edit(vec![row("s1", 11, "a")]),
    });
    assert!(sends(&effects).is_empty());

    let effects = store.apply(Event::Sync {
        ns: Ns::Spaces,
        step: Step::Posted,
    });
    assert_eq!(queued(&store, Ns::Spaces), [("s1".into(), 11)]);
    assert_eq!(pulls(&effects).len(), 1, "a post is followed by a pull");

    let effects = store.apply(Event::Sync {
        ns: Ns::Spaces,
        step: Step::Pulled(StateRows {
            rows: vec![row("s1", 10, "a")],
            rev: 1000,
        }),
    });
    assert_eq!(
        sends(&effects),
        [vec![row("s1", 11, "a")]],
        "the newer edit goes next"
    );
    store.apply(Event::Sync {
        ns: Ns::Spaces,
        step: Step::Posted,
    });
    assert!(queued(&store, Ns::Spaces).is_empty());
}

#[test]
fn a_pull_discards_dominated_queued_rows() {
    let mut store = loaded();
    store.apply(Event::OutboxLoaded(
        [(Ns::Notes, vec![row("n1", 10, "b"), row("n2", 10, "b")])].into(),
    ));
    assert!(
        store.sync[&Ns::Notes].rows.contains_key("n1"),
        "queued rows apply locally"
    );
    let effects = store.apply(Event::Sync {
        ns: Ns::Notes,
        step: Step::Pulled(StateRows {
            // n1 was beaten by web; n2's own write came back.
            rows: vec![row("n1", 12, "a"), row("n2", 10, "b")],
            rev: 9999,
        }),
    });
    assert!(queued(&store, Ns::Notes).is_empty());
    assert!(effects.contains(&Effect::Persist(Persist::Outbox)));
    assert!(sends(&effects).is_empty());
    assert_eq!(
        store.sync[&Ns::Notes].rows["n1"].updated,
        12,
        "the store keeps the winner"
    );
}

#[test]
fn a_409_keeps_edits_local_until_a_pull_succeeds() {
    let mut store = loaded();
    store.apply(Event::Sync {
        ns: Ns::Spaces,
        step: Step::Edit(vec![row("s1", 10, "a")]),
    });
    let effects = store.apply(Event::Sync {
        ns: Ns::Spaces,
        step: Step::PostFailed(Some(409)),
    });
    assert!(effects.is_empty(), "no retry: {effects:?}");
    assert_eq!(store.sync[&Ns::Spaces].hold, Some(Hold::LocalOnly));
    let effects = store.apply(Event::Sync {
        ns: Ns::Spaces,
        step: Step::Edit(vec![row("s2", 10, "a")]),
    });
    assert!(sends(&effects).is_empty(), "edits stay local");
    assert_eq!(queued(&store, Ns::Spaces).len(), 2);

    let effects = store.apply(Event::Sync {
        ns: Ns::Spaces,
        step: Step::Pulled(StateRows::default()),
    });
    assert_eq!(
        sends(&effects)[0].len(),
        2,
        "an attributed pull releases the outbox"
    );
}

#[test]
fn a_413_holds_the_outbox_until_the_next_edit() {
    let mut store = loaded();
    store.apply(Event::Sync {
        ns: Ns::Notes,
        step: Step::Edit(vec![row("n1", 10, "a")]),
    });
    store.apply(Event::Sync {
        ns: Ns::Notes,
        step: Step::PostFailed(Some(413)),
    });
    assert_eq!(store.sync[&Ns::Notes].hold, Some(Hold::TooLarge));
    let effects = store.apply(Event::Sync {
        ns: Ns::Notes,
        step: Step::Pulled(StateRows::default()),
    });
    assert!(sends(&effects).is_empty(), "a pull does not lift a 413");
    let effects = store.apply(Event::Sync {
        ns: Ns::Notes,
        step: Step::Edit(vec![row("n2", 10, "a")]),
    });
    assert_eq!(sends(&effects)[0].len(), 2);
}

#[test]
fn failures_back_off_500ms_doubling_to_10s() {
    let mut store = loaded();
    store.apply(Event::Sync {
        ns: Ns::Spaces,
        step: Step::Edit(vec![row("s1", 10, "a")]),
    });
    let mut delays = Vec::new();
    for _ in 0..7 {
        let effects = store.apply(Event::Sync {
            ns: Ns::Spaces,
            step: Step::PostFailed(Some(503)),
        });
        let [
            Effect::Retry {
                ns: Ns::Spaces,
                after_ms,
            },
        ] = effects[..]
        else {
            panic!("{effects:?}")
        };
        delays.push(after_ms);
        // The retry pulls first; the pull's answer sends again.
        let effects = store.apply(Event::Sync {
            ns: Ns::Spaces,
            step: Step::Retry,
        });
        assert_eq!(pulls(&effects).len(), 1);
        let effects = store.apply(Event::Sync {
            ns: Ns::Spaces,
            step: Step::Pulled(StateRows::default()),
        });
        assert_eq!(sends(&effects).len(), 1);
    }
    assert_eq!(delays, [500, 1000, 2000, 4000, 8000, 10000, 10000]);
    // Only a successful POST resets it (as web does).
    store.apply(Event::Sync {
        ns: Ns::Spaces,
        step: Step::Posted,
    });
    store.apply(Event::Sync {
        ns: Ns::Spaces,
        step: Step::Edit(vec![row("s1", 11, "a")]),
    });
    let effects = store.apply(Event::Sync {
        ns: Ns::Spaces,
        step: Step::PostFailed(None),
    });
    assert!(matches!(effects[..], [Effect::Retry { after_ms: 500, .. }]));
}

#[test]
fn a_transport_failure_while_a_retry_waits_schedules_nothing_more() {
    let mut store = loaded();
    let effects = store.apply(Event::Sync {
        ns: Ns::Notes,
        step: Step::PullFailed(None),
    });
    assert!(matches!(effects[..], [Effect::Retry { after_ms: 500, .. }]));
    let effects = store.apply(Event::Sync {
        ns: Ns::Notes,
        step: Step::PullFailed(None),
    });
    assert!(effects.is_empty());
}

#[test]
fn state_changed_pulls_only_above_the_cursor() {
    let mut store = loaded();
    let rev = fixture_rows(Ns::Notes).rev;
    let changed = |rev| Event::Stream {
        generation: 1,
        event: StreamEvent::Frame(Wire::StateChanged(StateChanged {
            namespace: "notes".into(),
            rev,
        })),
    };
    assert!(store.apply(changed(rev)).is_empty());
    assert_eq!(pulls(&store.apply(changed(rev + 1))), [(Ns::Notes, rev)]);
    // A second nudge while that pull is in flight coalesces into one more pull after it.
    assert!(store.apply(changed(rev + 2)).is_empty());
    let effects = store.apply(Event::Sync {
        ns: Ns::Notes,
        step: Step::Pulled(StateRows {
            rows: vec![],
            rev: rev + 1,
        }),
    });
    assert_eq!(pulls(&effects), [(Ns::Notes, rev + 1)]);
}

#[test]
fn every_hello_catches_up_and_a_new_build_is_flagged() {
    // Boot's pulls answered before the stream subscribed: a web edit in that gap sends no nudge this
    // client sees, so the first hello pulls again from the cursor.
    let mut store = loaded();
    let effects = store.apply(hello("b1"));
    assert_eq!(pulls(&effects).len(), 3, "the first hello pulls too");
    assert!(pulls(&effects).iter().all(|(_, since)| *since > 0));
    assert!(!store.server_updated);

    // A hello while boot's pulls are still in flight coalesces into one more pull after each.
    let mut store = Store::default();
    store.apply(Event::Boot);
    assert!(pulls(&store.apply(hello("b1"))).is_empty());
    let notes = fixture_rows(Ns::Notes);
    let rev = notes.rev;
    let effects = store.apply(Event::Sync {
        ns: Ns::Notes,
        step: Step::Pulled(notes),
    });
    assert_eq!(pulls(&effects), [(Ns::Notes, rev)]);

    store.apply(hello("b1"));
    assert!(!store.server_updated, "a reopen on the same build");
    store.apply(hello("b2"));
    assert!(store.server_updated);
}

#[test]
fn an_unknown_viewer_is_retried_on_a_timer_and_only_409_is_a_refusal() {
    let asks = |effects: &[Effect]| {
        effects
            .iter()
            .filter(|e| **e == Effect::Fetch(Fetch::Viewer))
            .count()
    };
    let retry = |effects: &[Effect]| {
        effects.iter().find_map(|e| match e {
            Effect::RetryViewer { after_ms } => Some(*after_ms),
            _ => None,
        })
    };
    let mut store = Store::default();
    assert_eq!(asks(&store.apply(Event::Boot)), 1);
    // The stream's hello arrives while boot's ask is still in flight: no second ask.
    assert_eq!(asks(&store.apply(hello("b1"))), 0, "one ask at a time");
    // Then that ask fails in transport (offline launch): unknown, not unattributed, and retried on a
    // timer, because a healthy stream may never send another hello.
    let effects = store.apply(Event::Viewer(Err(None)));
    assert_eq!(store.viewer, Attribution::Unknown);
    assert_eq!(retry(&effects), Some(500));
    assert_eq!(asks(&store.apply(Event::ViewerRetry)), 1);
    // A server fault is not a refusal either; the backoff grows.
    let effects = store.apply(Event::Viewer(Err(Some(502))));
    assert_eq!(store.viewer, Attribution::Unknown);
    assert_eq!(retry(&effects), Some(1000));
    // A hello while that timer waits asks at once; the failure it meets schedules no second timer.
    assert_eq!(asks(&store.apply(hello("b1"))), 1);
    assert_eq!(retry(&store.apply(Event::Viewer(Err(Some(503))))), None);
    // The timer fires and the server answers, with no further hello.
    assert_eq!(asks(&store.apply(Event::ViewerRetry)), 1);
    let effects = store.apply(Event::Viewer(Ok("web-me".into())));
    assert!(effects.is_empty(), "{effects:?}");
    assert_eq!(store.viewer, Attribution::Attributed("web-me".into()));
    assert_eq!(asks(&store.apply(hello("b1"))), 0);
    assert_eq!(asks(&store.apply(Event::ViewerRetry)), 0);

    // 409 is the one refusal: never retried, by timer or by hello.
    let mut store = Store::default();
    store.apply(Event::Boot);
    let effects = store.apply(Event::Viewer(Err(Some(409))));
    assert_eq!(store.viewer, Attribution::Refused);
    assert!(effects.is_empty(), "{effects:?}");
    assert_eq!(asks(&store.apply(hello("b1"))), 0, "a refusal is an answer");
}

#[test]
fn frames_from_an_old_stream_are_ignored() {
    let mut store = loaded();
    store.apply(Event::Boot);
    store.apply(fleet_frame(board()));
    assert!(
        store.fleet.agents.is_empty(),
        "generation 1 is stale after a second boot"
    );
}

#[test]
fn text_scale_steps_clamps_and_persists() {
    let mut store = Store::default();
    assert_eq!(
        store.apply(Event::TextScale(TextScale::Bigger)),
        vec![Effect::Persist(Persist::Prefs)]
    );
    assert_eq!(store.prefs.text_scale, 1.1);
    for _ in 0..20 {
        store.apply(Event::TextScale(TextScale::Bigger));
    }
    assert_eq!(store.prefs.text_scale, 1.8);
    for _ in 0..30 {
        store.apply(Event::TextScale(TextScale::Smaller));
    }
    assert_eq!(store.prefs.text_scale, 0.7);
    store.apply(Event::TextScale(TextScale::Reset));
    assert_eq!(store.prefs, Prefs::default());
}

/// The board with `name`'s latest turn moved on by `by`.
pub(crate) fn bump(b: &mut Board, name: &str, by: u64) {
    let panes = b
        .workspaces
        .iter_mut()
        .flat_map(|w| &mut w.tabs)
        .flat_map(|t| &mut t.panes);
    for pane in panes.filter(|p| p.agent == name) {
        pane.turn_end_id = pane.turn_end_id.map(|t| t + by);
    }
}

pub(crate) fn space_of<'a>(store: &'a Store, agent: &str) -> &'a spaces::Space {
    store
        .spaces
        .iter()
        .find(|s| s.agents().any(|a| a == agent))
        .unwrap()
}

fn lens(m: spaces::Move) -> Event {
    Event::Lens(m)
}

#[test]
fn spaces_sit_in_watch_until_placed() {
    let mut store = loaded();
    store.apply(fleet_frame(board()));
    let [focus, watch, background] = store.rows();
    assert!(focus.is_empty() && background.is_empty());
    assert_eq!(watch.len(), store.spaces.len());

    let slack = space_of(&store, "mupu").id.clone();
    let chief = space_of(&store, "chief-mihe").id.clone();
    let place = |row| {
        lens(spaces::Move::SetRow {
            space: slack.clone(),
            row,
        })
    };
    assert_eq!(
        store.apply(place(Row::Focus)),
        [Effect::Persist(Persist::Prefs)]
    );
    assert!(
        store.apply(place(Row::Focus)).is_empty(),
        "no change, no write"
    );
    assert_eq!(store.prefs.rows[&slack], Row::Focus);
    let ids = |row: &[&spaces::Space]| row.iter().map(|s| s.id.clone()).collect::<Vec<_>>();
    assert_eq!(ids(&store.rows()[0]), std::slice::from_ref(&slack));

    // Within a row the store's order holds: chief (order 0) before slack (order 1).
    store.apply(lens(spaces::Move::SetRow {
        space: chief.clone(),
        row: Row::Background,
    }));
    store.apply(place(Row::Background));
    assert_eq!(ids(&store.rows()[2]), [chief.clone(), slack.clone()]);
    let order = ids(&store.lens());
    assert_eq!(
        order[order.len() - 2..],
        [chief, slack],
        "background comes last"
    );
}

#[test]
fn the_visible_agent_cycles_over_members_on_the_board() {
    let mut store = loaded();
    let mut b = board();
    store.apply(fleet_frame(b.clone()));
    let slack = space_of(&store, "mupu").clone();
    assert_eq!(
        store.visible(&slack),
        Some("mupu"),
        "the first member by default"
    );

    let cycle = || lens(spaces::Move::CycleVisible(slack.id.clone()));
    assert_eq!(store.apply(cycle()), [Effect::Persist(Persist::Prefs)]);
    assert_eq!(store.visible(&slack), Some("support-mifa"));
    store.apply(cycle());
    assert_eq!(store.visible(&slack), Some("mupu"), "wraps");
    store.apply(cycle());

    // support-mifa leaves the board: the card falls back to the first member still there, and
    // cycling skips the gone one.
    for w in &mut b.workspaces {
        for t in &mut w.tabs {
            t.panes.retain(|p| p.agent != "support-mifa");
        }
    }
    store.apply(fleet_frame(b));
    assert_eq!(store.prefs.visible[&slack.id], "support-mifa");
    assert_eq!(store.visible(&slack), Some("mupu"));
    store.apply(cycle());
    assert_eq!(store.visible(&slack), Some("mupu"));
}

#[test]
fn next_needing_walks_the_rows_in_order_and_wraps() {
    let mut store = loaded();
    let mut b = board();
    store.apply(fleet_frame(b.clone()));
    assert_eq!(
        store.next_needing(None),
        None,
        "nothing unread on first sight"
    );

    bump(&mut b, "mupu", 1);
    bump(&mut b, "chief-mihe", 1);
    bump(&mut b, "orch-lega", 1);
    store.apply(fleet_frame(b));
    let id = |store: &Store, agent| space_of(store, agent).id.clone();
    let (slack, chief, herder) = (
        id(&store, "mupu"),
        id(&store, "chief-mihe"),
        id(&store, "orch-lega"),
    );
    // chief to focus and slack to background: focus, then watch (herder), then background.
    store.apply(lens(spaces::Move::SetRow {
        space: chief.clone(),
        row: Row::Focus,
    }));
    store.apply(lens(spaces::Move::SetRow {
        space: slack.clone(),
        row: Row::Background,
    }));
    let next = |store: &Store, from: Option<&str>| store.next_needing(from).map(|s| s.id.clone());
    assert_eq!(next(&store, None), Some(chief.clone()));
    assert_eq!(next(&store, Some(&chief)), Some(herder.clone()));
    assert_eq!(next(&store, Some(&herder)), Some(slack.clone()));
    assert_eq!(
        next(&store, Some(&slack)),
        Some(chief.clone()),
        "wraps to the top"
    );

    // From a space that does not need you, the next one after it.
    let quiet = id(&store, "swap-bench-mora");
    assert_eq!(next(&store, Some(&quiet)), Some(herder.clone()));

    // Only one left: it is its own next.
    store.apply(lens(spaces::Move::Read(chief.clone())));
    store.apply(lens(spaces::Move::Read(herder.clone())));
    assert_eq!(next(&store, Some(&slack)), Some(slack));
}

#[test]
fn read_and_unread_mark_the_whole_space() {
    let mut store = loaded();
    let mut b = board();
    store.apply(fleet_frame(b.clone()));
    let slack = space_of(&store, "mupu").clone();
    assert_eq!(store.needs_you(&slack), 0);

    // `u`: the space needs you, sticky across new boards, until a zoom-in.
    let unread = || lens(spaces::Move::Unread(slack.id.clone()));
    assert_eq!(store.apply(unread()), [Effect::Persist(Persist::Prefs)]);
    assert!(store.apply(unread()).is_empty(), "already unread");
    store.apply(fleet_frame(b.clone()));
    assert_eq!(store.needs_you(&slack), 1);
    assert_eq!(store.next_needing(None).map(|s| &s.id), Some(&slack.id));
    let view = |agent: &str| {
        let agent = Some(agent.to_string());
        lens(spaces::Move::View {
            space: slack.id.clone(),
            agent,
        })
    };
    assert_eq!(
        store.apply(view("support-mifa")),
        [Effect::Persist(Persist::Prefs)]
    );
    assert_eq!(store.needs_you(&slack), 0, "a zoom-in clears it");
    assert!(
        store
            .apply(lens(spaces::Move::Unread("no-such-space".into())))
            .is_empty()
    );

    // `m`: every agent in the space is read, and the unread mark goes too.
    bump(&mut b, "mupu", 2);
    bump(&mut b, "support-mifa", 2);
    store.apply(fleet_frame(b));
    store.apply(unread());
    assert_eq!(store.needs_you(&slack), 2);
    assert_eq!(
        store
            .apply(lens(spaces::Move::Read(slack.id.clone())))
            .len(),
        1
    );
    assert_eq!(store.needs_you(&slack), 0);
    assert!(store.prefs.unread.is_empty());
}

#[test]
fn viewing_an_absent_agent_leaves_prefs_alone() {
    let mut store = loaded();
    store.apply(fleet_frame(board()));
    let before = store.prefs.clone();
    let slack = space_of(&store, "mupu").id.clone();
    let view = spaces::Move::View {
        space: slack.clone(),
        agent: Some("nobody".into()),
    };
    assert!(store.apply(lens(view)).is_empty());
    assert!(
        store
            .apply(lens(spaces::Move::Read("no-such-space".into())))
            .is_empty()
    );
    assert_eq!(store.prefs, before);
}

/// The board with `name` Blocked (or not).
fn block(b: &mut Board, name: &str, blocked: bool) {
    let panes = b
        .workspaces
        .iter_mut()
        .flat_map(|w| &mut w.tabs)
        .flat_map(|t| &mut t.panes);
    for pane in panes.filter(|p| p.agent == name) {
        pane.bus_status = if blocked { "blocked" } else { "listening" }.into();
    }
}

#[test]
fn a_block_needs_you_until_viewed_and_again_when_it_recurs() {
    const { assert!(fleet::BLOCKED_ALWAYS_NEEDS_YOU) };
    let mut store = loaded();
    let mut b = board();
    store.apply(fleet_frame(b.clone()));
    let slack = space_of(&store, "mupu").id.clone();
    let view = || {
        let agent = Some("mupu".to_string());
        lens(spaces::Move::View {
            space: slack.clone(),
            agent,
        })
    };

    block(&mut b, "mupu", true);
    store.apply(fleet_frame(b.clone()));
    assert!(store.agent_needs_you("mupu"), "blocked, no new turn");
    assert_eq!(store.apply(view()), [Effect::Persist(Persist::Prefs)]);
    assert!(
        !store.agent_needs_you("mupu"),
        "viewing acknowledges this block"
    );
    store.apply(fleet_frame(b.clone()));
    assert!(!store.agent_needs_you("mupu"), "still the same block");

    // It leaves Blocked and blocks again within the same turn: that alerts again.
    block(&mut b, "mupu", false);
    let effects = store.apply(fleet_frame(b.clone()));
    assert!(
        effects.contains(&Effect::Persist(Persist::Prefs)),
        "the block mark clears"
    );
    block(&mut b, "mupu", true);
    store.apply(fleet_frame(b));
    assert!(store.agent_needs_you("mupu"));
}

#[test]
fn a_block_before_any_turn_alerts_again_when_it_recurs() {
    let mut store = loaded();
    let mut b = board();
    let panes = b.workspaces.iter_mut().flat_map(|w| &mut w.tabs);
    for pane in panes
        .flat_map(|t| &mut t.panes)
        .filter(|p| p.agent == "mupu")
    {
        pane.turn_end_id = None;
    }
    let slack = space_of(&store, "mupu").id.clone();
    let view = lens(spaces::Move::View {
        space: slack,
        agent: Some("mupu".into()),
    });
    block(&mut b, "mupu", true);
    store.apply(fleet_frame(b.clone()));
    assert!(store.agent_needs_you("mupu"), "blocked with no turn yet");
    store.apply(view);
    assert!(!store.agent_needs_you("mupu"), "viewed");
    block(&mut b, "mupu", false);
    store.apply(fleet_frame(b.clone()));
    block(&mut b, "mupu", true);
    store.apply(fleet_frame(b));
    assert!(store.agent_needs_you("mupu"), "blocked again");
}

#[test]
fn seen_marks_read_the_old_bare_turn_form() {
    let old: Prefs = serde_json::from_str(r#"{"seen": {"mupu": 42}}"#).unwrap();
    let mark = spaces::Seen {
        turn_end: 42,
        blocked: false,
    };
    assert_eq!(old.seen["mupu"], mark);
    let new = serde_json::to_string(&old).unwrap();
    let back: Prefs = serde_json::from_str(&new).unwrap();
    assert_eq!(back.seen["mupu"], mark);
}
