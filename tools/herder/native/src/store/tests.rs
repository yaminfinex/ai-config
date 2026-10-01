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
    assert_eq!(store.viewer, Attribution::Refused(None));
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

// Transcript (U3): paging, catch-up, resets and condensing, against the recorded pages.

mod transcript_pages {
    use super::*;
    use crate::api::client::Page;
    use crate::api::{Candidate, Entries, EntriesWindow, Entry, Reset, ResolveRoot, Resolved};
    use crate::store::condense::{self, condense};
    use crate::store::transcript::{Got, Item, Op, PAGE, Read, Step as T, Timer, What};
    use std::collections::VecDeque;

    fn fixture(agent: &str, page: &str) -> Entries {
        let text = match (agent, page) {
            ("mupu", "tail") => include_str!("../../testdata/agents/mupu/tail.json"),
            ("mupu", _) => include_str!("../../testdata/agents/mupu/before.json"),
            ("conductor-line", "tail") => {
                include_str!("../../testdata/agents/conductor-line/tail.json")
            }
            ("conductor-line", _) => {
                include_str!("../../testdata/agents/conductor-line/before.json")
            }
            ("grill-confirm-lubo", "tail") => {
                include_str!("../../testdata/agents/grill-confirm-lubo/tail.json")
            }
            ("grill-confirm-lubo", _) => {
                include_str!("../../testdata/agents/grill-confirm-lubo/before.json")
            }
            ("riko", "tail") => include_str!("../../testdata/agents/riko/tail.json"),
            _ => include_str!("../../testdata/agents/riko/before.json"),
        };
        serde_json::from_str(text).expect("entries fixture decodes")
    }

    const AGENTS: [&str; 4] = ["mupu", "conductor-line", "grill-confirm-lubo", "riko"];

    /// Both recorded pages of `agent`, oldest first: one stretch of its transcript.
    fn history(agent: &str) -> Vec<Entry> {
        let mut all = fixture(agent, "before").entries;
        all.extend(fixture(agent, "tail").entries);
        all
    }

    /// A server over `all` (the whole file), paging by `limit` as the contract says.
    fn serve(all: &[Entry], page: &Page, limit: usize, session: &str) -> Entries {
        let at = |offset: u64| all.partition_point(|e| e.byte_offset < offset);
        let (a, b, mode) = match page {
            Page::Tail { .. } => (all.len().saturating_sub(limit), all.len(), "tail"),
            Page::From { offset, .. } => {
                (at(*offset), (at(*offset) + limit).min(all.len()), "from")
            }
            Page::Before { offset, .. } => {
                (at(*offset).saturating_sub(limit), at(*offset), "before")
            }
        };
        let offset = |i: usize| {
            all.get(i)
                .map_or(all.last().map_or(0, |e| e.byte_offset + 1), |e| {
                    e.byte_offset
                })
        };
        let first = if a == 0 { 0 } else { offset(a) };
        Entries {
            session_id: session.into(),
            window: EntriesWindow {
                mode: mode.into(),
                from: first,
                limit: limit as u64,
            },
            entries: all[a..b].to_vec(),
            next_offset: (mode != "before").then(|| offset(b)),
            prev_offset: (mode == "before").then_some(first),
            reset: None,
        }
    }

    /// Answer every transcript read among `effects` (and the reads they lead to) from `all`; returns
    /// the pages read.
    fn drive(store: &mut Store, effects: Vec<Effect>, all: &[Entry], limit: usize) -> Vec<Page> {
        let mut queue: VecDeque<Effect> = effects.into();
        let mut pages = Vec::new();
        while let Some(effect) = queue.pop_front() {
            let Effect::Fetch(Fetch::Transcript(read)) = effect else {
                continue;
            };
            let got = match &read.what {
                What::Page(page) => {
                    pages.push(page.clone());
                    Got::Page(Box::new(serve(all, page, limit, "s1")))
                }
                What::Detail => Got::Detail(Box::default()),
                What::Resolve(..) => continue,
            };
            queue.extend(store.apply(Event::Transcript(T::Read(read, Ok(got)))));
        }
        pages
    }

    fn open(store: &mut Store, agent: &str) -> Vec<Effect> {
        let (space, agent) = ("none".into(), agent.to_string());
        store.apply(Event::Transcript(T::Show { space, agent }))
    }

    fn items(store: &Store) -> &BTreeMap<(u64, u16), Item> {
        &store.transcript.open.as_ref().unwrap().items
    }

    /// The rows a single read of the whole history gives.
    fn reference(agent: &str, all: &[Entry]) -> BTreeMap<(u64, u16), Item> {
        let mut store = loaded();
        let effects = open(&mut store, agent);
        drive(&mut store, effects, all, usize::MAX);
        items(&store).clone()
    }

    #[test]
    fn the_tail_then_before_pages_reach_the_start_with_every_entry_once() {
        for agent in AGENTS {
            // Stretch each history past two pages by repeating it at higher offsets.
            let base = history(agent);
            let span = base.last().unwrap().byte_offset + 1;
            let copy = |i: u64, e: &Entry| {
                let mut e = e.clone();
                e.byte_offset += i * span;
                if let Some(id) = e.payload["tool_use_id"].as_str() {
                    e.payload["tool_use_id"] = json!(format!("{id}-{i}"));
                }
                e
            };
            let all: Vec<Entry> = (0..3u64)
                .flat_map(|i| base.iter().map(move |e| copy(i, e)))
                .collect();
            let mut store = loaded();
            let effects = open(&mut store, agent);
            let mut pages = drive(&mut store, effects, &all, PAGE as usize);
            assert!(
                matches!(pages[..], [Page::Tail { .. }]),
                "{agent}: the tail first"
            );
            for _ in 0..100 {
                let effects = store.apply(Event::Transcript(T::Older));
                if effects.is_empty() {
                    break;
                }
                pages.extend(drive(&mut store, effects, &all, PAGE as usize));
            }
            let t = store.transcript.open.as_ref().unwrap();
            assert!(t.at_start(), "{agent}: paged to the start");
            let backs = pages
                .iter()
                .filter(|p| matches!(p, Page::Before { .. }))
                .count();
            assert_eq!(
                backs,
                all.len().div_ceil(PAGE as usize) - 1,
                "{agent}: one read per page"
            );
            assert_eq!(
                items(&store),
                &reference(agent, &all),
                "{agent}: every entry once"
            );
            assert!(
                store.apply(Event::Transcript(T::Older)).is_empty(),
                "nothing before the start"
            );
        }
    }

    #[test]
    fn wakes_read_forward_one_at_a_time_and_catch_up() {
        let all = history("conductor-line");
        let (early, _) = all.split_at(all.len() - 30);
        let mut store = loaded();
        let effects = open(&mut store, "conductor-line");
        drive(&mut store, effects, early, PAGE as usize);
        let entry = |agent: &str| Event::Stream {
            generation: store.stream,
            event: StreamEvent::Frame(Wire::Entry {
                agent: agent.into(),
                entry: Entry::default(),
            }),
        };
        let (other, first, second) = (
            entry("mupu"),
            entry("conductor-line"),
            entry("conductor-line"),
        );
        assert!(
            store.apply(other).is_empty(),
            "another agent's wake reads nothing"
        );
        let effects = store.apply(first);
        let reads: Vec<&Read> = effects
            .iter()
            .filter_map(|e| match e {
                Effect::Fetch(Fetch::Transcript(r)) => Some(r),
                _ => None,
            })
            .collect();
        assert!(
            matches!(reads[0].what, What::Page(Page::From { .. })),
            "{reads:?}"
        );
        let again = store.apply(second);
        assert!(
            !again.iter().any(|e| matches!(
                e,
                Effect::Fetch(Fetch::Transcript(Read {
                    what: What::Page(_),
                    ..
                }))
            )),
            "a wake while reading waits for the read"
        );
        let pages = drive(&mut store, effects, &all, PAGE as usize);
        assert_eq!(
            pages.len(),
            2,
            "the waiting wake reads once more: {pages:?}"
        );
        let tail = &all[early.len() - PAGE as usize..];
        assert_eq!(items(&store), &reference("conductor-line", tail));
    }

    /// The transcript reads among `effects`.
    fn reads(effects: &[Effect]) -> Vec<Read> {
        let reads = effects.iter().filter_map(|e| match e {
            Effect::Fetch(Fetch::Transcript(r)) => Some(r.clone()),
            _ => None,
        });
        reads.collect()
    }

    fn frame(store: &Store, wire: Wire) -> Event {
        let generation = store.stream;
        let event = StreamEvent::Frame(wire);
        Event::Stream { generation, event }
    }

    fn wake(store: &Store, agent: &str) -> Event {
        let (agent, entry) = (agent.into(), Entry::default());
        frame(store, Wire::Entry { agent, entry })
    }

    fn fail(store: &mut Store, read: &Read) -> Vec<Effect> {
        store.apply(Event::Transcript(T::Read(read.clone(), Err("down".into()))))
    }

    fn notice(store: &Store) -> Option<&str> {
        store.transcript.open.as_ref().unwrap().notice()
    }

    /// The one retry timer among `effects`, and its delay.
    fn timer(effects: &[Effect]) -> (Timer, u64) {
        match effects {
            [Effect::RetryTranscript { timer, after_ms }] => (*timer, *after_ms),
            other => panic!("one retry: {other:?}"),
        }
    }

    fn retry(store: &mut Store, timer: Timer) -> Vec<Read> {
        reads(&store.apply(Event::Transcript(T::Retry(timer))))
    }

    #[test]
    fn failed_reads_retry_with_a_bounded_backoff_and_drain_queued_wakes() {
        let mut store = loaded();
        let opened = reads(&open(&mut store, "mupu"));
        let (tail, generation) = (&opened[0], opened[0].generation);
        assert!(matches!(tail.what, What::Page(Page::Tail { .. })));
        assert!(
            reads(&store.apply(wake(&store, "mupu"))).is_empty(),
            "queued"
        );
        let (mut pending, after_ms) = timer(&fail(&mut store, tail));
        assert_eq!(
            (pending.generation, pending.op, after_ms),
            (generation, Op::Forward, 1000)
        );
        assert_eq!(notice(&store), Some("could not read: down"));

        // Each retry reads once for the failed read and the wake queued behind it; three, then none.
        let mut tail = tail.clone();
        for after_ms in [2000, 4000] {
            let again = retry(&mut store, pending);
            assert!(
                matches!(again.as_slice(), [r] if r.what == tail.what),
                "{again:?}"
            );
            assert!(retry(&mut store, pending).is_empty(), "a timer fires once");
            tail = again[0].clone();
            let next = timer(&fail(&mut store, &tail));
            assert_eq!(next.1, after_ms);
            pending = next.0;
        }
        let again = retry(&mut store, pending);
        assert!(fail(&mut store, &again[0]).is_empty(), "bounded");
        let old = Timer {
            generation: generation + 9,
            ..pending
        };
        assert!(retry(&mut store, old).is_empty());

        // A hello clears the notice and reads again; the page lands.
        let hello = frame(
            &store,
            Wire::Hello(Hello {
                build_identity: "b".into(),
            }),
        );
        let effects = store.apply(hello);
        assert_eq!(notice(&store), None);
        drive(&mut store, effects, &history("mupu"), PAGE as usize);
        assert!(store.transcript.open.as_ref().unwrap().loaded());
    }

    #[test]
    fn a_failed_detail_retries_and_showing_a_stuck_transcript_reopens_it() {
        let mut store = loaded();
        let opened = reads(&open(&mut store, "mupu"));
        let generation = opened[0].generation;
        let (pending, _) = timer(&fail(&mut store, &opened[1]));
        let again = retry(&mut store, pending);
        assert!(matches!(
            again.as_slice(),
            [Read {
                what: What::Detail,
                ..
            }]
        ));

        // The tail failed and nothing is in flight: showing the agent again opens it afresh.
        fail(&mut store, &opened[0]);
        let reopened = reads(&open(&mut store, "mupu"));
        assert!(
            reopened.iter().all(|r| r.generation > generation),
            "{reopened:?}"
        );
        assert!(matches!(reopened[0].what, What::Page(Page::Tail { .. })));
    }

    #[test]
    fn a_failed_page_back_waits_for_the_notice_instead_of_looping() {
        let all = history("mupu");
        let mut store = loaded();
        let effects = open(&mut store, "mupu");
        drive(&mut store, effects, &all, PAGE as usize);
        let back = reads(&store.apply(Event::Transcript(T::Older)));
        assert!(matches!(back[0].what, What::Page(Page::Before { .. })));
        assert!(fail(&mut store, &back[0]).is_empty(), "no retry");
        assert!(
            store.apply(Event::Transcript(T::Older)).is_empty(),
            "no hot loop"
        );
        store.apply(Event::Transcript(T::Dismiss));
        let back = reads(&store.apply(Event::Transcript(T::Older)));
        assert_eq!(back.len(), 1, "dismissing lets it page back again");

        // A forward page landing leaves the page back's notice; a hello clears it.
        fail(&mut store, &back[0]);
        let effects = store.apply(wake(&store, "mupu"));
        drive(&mut store, effects, &all, PAGE as usize);
        assert_eq!(notice(&store), Some("could not read: down"));
        assert!(store.apply(Event::Transcript(T::Older)).is_empty());
        let hello = Wire::Hello(Hello {
            build_identity: "b".into(),
        });
        let effects = store.apply(frame(&store, hello));
        drive(&mut store, effects, &all, PAGE as usize);
        assert_eq!(notice(&store), None);
        assert_eq!(reads(&store.apply(Event::Transcript(T::Older))).len(), 1);
    }

    #[test]
    fn each_read_keeps_its_own_retries_when_a_sibling_recovers() {
        let all = history("mupu");
        let mut store = loaded();
        let opened = reads(&open(&mut store, "mupu"));
        let (tail_timer, _) = timer(&fail(&mut store, &opened[0]));
        let (mut detail_timer, after_ms) = timer(&fail(&mut store, &opened[1]));
        assert_eq!((detail_timer.op, after_ms), (Op::Detail, 1000));

        // The tail recovers on its retry; the detail's own budget is untouched.
        let tail = retry(&mut store, tail_timer);
        let effects = tail
            .into_iter()
            .map(|r| Effect::Fetch(Fetch::Transcript(r)));
        drive(&mut store, effects.collect(), &all, PAGE as usize);
        assert!(store.transcript.open.as_ref().unwrap().loaded());
        assert!(retry(&mut store, tail_timer).is_empty(), "fired already");

        // The detail keeps failing: original + three retries at 1, 2 and 4 s, and nothing else.
        let mut attempts = 1;
        for after_ms in [2000, 4000] {
            let again = retry(&mut store, detail_timer);
            assert!(matches!(again.as_slice(), [r] if r.what == What::Detail));
            attempts += 1;
            let next = timer(&fail(&mut store, &again[0]));
            assert_eq!((next.0.op, next.1), (Op::Detail, after_ms));
            assert!(retry(&mut store, detail_timer).is_empty(), "superseded");
            detail_timer = next.0;
        }
        let again = retry(&mut store, detail_timer);
        attempts += 1;
        assert!(fail(&mut store, &again[0]).is_empty(), "budget spent");
        assert_eq!(attempts, 4);
        assert!(retry(&mut store, tail_timer).is_empty());
        assert!(retry(&mut store, detail_timer).is_empty());
        assert!(store.transcript.open.as_ref().unwrap().blocked());

        // A detail that recovers clears its own notice and lets paging back resume.
        let effects = store.apply(wake(&store, "mupu"));
        drive(&mut store, effects, &all, PAGE as usize);
        assert_eq!(notice(&store), None);
        assert_eq!(reads(&store.apply(Event::Transcript(T::Older))).len(), 1);
    }

    #[test]
    fn a_detail_recovering_keeps_a_page_back_notice() {
        let all = history("mupu");
        let mut store = loaded();
        let effects = open(&mut store, "mupu");
        drive(&mut store, effects, &all, PAGE as usize);
        let back = reads(&store.apply(Event::Transcript(T::Older)));
        let detail = reads(&store.apply(wake(&store, "mupu")));
        let detail = detail.iter().find(|r| r.what == What::Detail).unwrap();
        fail(&mut store, &back[0]);
        let got = Ok(Got::Detail(Box::default()));
        store.apply(Event::Transcript(T::Read(detail.clone(), got)));
        assert_eq!(
            notice(&store),
            Some("could not read: down"),
            "the page back's"
        );
        assert!(store.transcript.open.as_ref().unwrap().blocked());
    }

    #[test]
    fn a_wake_racing_a_page_back_reads_forward_independently() {
        let all = history("mupu");
        let mut store = loaded();
        let effects = open(&mut store, "mupu");
        drive(&mut store, effects, &all, PAGE as usize);
        let next = store.transcript.open.as_ref().unwrap().next_offset;
        let back = reads(&store.apply(Event::Transcript(T::Older)));
        let forward = store.apply(wake(&store, "mupu"));
        assert!(matches!(
            reads(&forward)[0].what,
            What::Page(Page::From { .. })
        ));
        assert!(
            reads(&store.apply(wake(&store, "mupu"))).is_empty(),
            "queued"
        );

        // The page back lands first: it neither moves the forward cursor nor takes the queued wake.
        let Read {
            what: What::Page(page),
            ..
        } = &back[0]
        else {
            panic!()
        };
        let got = Got::Page(Box::new(serve(&all, page, PAGE as usize, "s1")));
        let effects = store.apply(Event::Transcript(T::Read(back[0].clone(), Ok(got))));
        assert!(reads(&effects).is_empty(), "{effects:?}");
        assert_eq!(store.transcript.open.as_ref().unwrap().next_offset, next);
        let pages = drive(&mut store, forward, &all, PAGE as usize);
        assert_eq!(pages.len(), 2, "the queued wake reads once more: {pages:?}");
    }

    #[test]
    fn a_tail_of_only_hidden_entries_keeps_reading_back() {
        let mut all = history("mupu");
        let end = all.last().unwrap().byte_offset;
        let hidden = (1..=PAGE as u64 * 2).map(|i| Entry {
            byte_offset: end + i,
            kind: Kind::TurnDuration,
            ..Entry::default()
        });
        all.extend(hidden);
        let mut store = loaded();
        let effects = open(&mut store, "mupu");
        let pages = drive(&mut store, effects, &all, PAGE as usize);
        let backs = pages.iter().filter(|p| matches!(p, Page::Before { .. }));
        assert!(backs.count() >= 2, "{pages:?}");
        assert!(!items(&store).is_empty());
    }

    #[test]
    fn a_message_to_the_open_agent_refreshes_its_detail() {
        let mut store = loaded();
        let effects = open(&mut store, "mupu");
        drive(&mut store, effects, &history("mupu"), PAGE as usize);
        let to = |names: &[&str]| {
            let to = names.iter().map(|n| n.to_string()).collect();
            Wire::Message(crate::api::Message { to })
        };
        assert!(store.apply(frame(&store, to(&["riko"]))).is_empty());
        let effects = store.apply(frame(&store, to(&["riko", "mupu"])));
        assert!(matches!(
            reads(&effects).as_slice(),
            [Read {
                what: What::Detail,
                ..
            }]
        ));
    }

    #[test]
    fn zooming_out_drops_the_transcript_and_its_stream() {
        let mut store = loaded();
        let effects = open(&mut store, "mupu");
        drive(&mut store, effects, &history("mupu"), PAGE as usize);
        let effects = store.apply(Event::Transcript(T::Hide));
        assert!(store.transcript.open.is_none());
        let unsubscribed =
            |e: &Effect| matches!(e, Effect::Stream { agents, .. } if agents.is_empty());
        assert!(
            matches!(effects.as_slice(), [e] if unsubscribed(e)),
            "{effects:?}"
        );
        assert!(store.apply(wake(&store, "mupu")).is_empty());
        assert!(store.apply(Event::Transcript(T::Hide)).is_empty(), "once");
        let reopened = reads(&open(&mut store, "mupu"));
        assert!(matches!(reopened[0].what, What::Page(Page::Tail { .. })));
    }

    #[test]
    fn a_reset_rereads_the_tail_and_stale_answers_are_dropped() {
        let all = history("mupu");
        let mut store = loaded();
        let effects = open(&mut store, "mupu");
        drive(&mut store, effects, &all, PAGE as usize);
        let old = store.transcript.open.as_ref().unwrap().generation;
        let wake = store.apply(Event::Stream {
            generation: store.stream,
            event: StreamEvent::Frame(Wire::Entry {
                agent: "mupu".into(),
                entry: Entry::default(),
            }),
        });
        let Some(Effect::Fetch(Fetch::Transcript(read))) = wake.into_iter().next() else {
            panic!()
        };
        let reset = Entries {
            session_id: "s2".into(),
            reset: Some(Reset {
                reason: "session_changed".into(),
                session_id: Some("s2".into()),
            }),
            ..Entries::default()
        };
        let effects = store.apply(Event::Transcript(T::Read(
            read.clone(),
            Ok(Got::Page(Box::new(reset))),
        )));
        let t = store.transcript.open.as_ref().unwrap();
        assert!(t.items.is_empty() && !t.loaded() && t.generation > old);
        assert!(effects.iter().any(|e| matches!(
            e,
            Effect::Fetch(Fetch::Transcript(Read {
                what: What::Page(Page::Tail { .. }),
                ..
            }))
        )));
        // The old generation's answer lands late: dropped.
        let late = serve(&all, &Page::Tail { limit: PAGE }, PAGE as usize, "s1");
        assert!(
            store
                .apply(Event::Transcript(T::Read(
                    read,
                    Ok(Got::Page(Box::new(late)))
                )))
                .is_empty()
        );
        assert!(items(&store).is_empty());
        drive(&mut store, effects, &all, PAGE as usize);
        let tail = &all[all.len() - PAGE as usize..];
        assert_eq!(items(&store), &reference("mupu", tail));
        // A rewindow frame does the same.
        let effects = store.apply(Event::Stream {
            generation: store.stream,
            event: StreamEvent::Frame(Wire::Rewindow(crate::api::Rewindow {
                agent: "mupu".into(),
            })),
        });
        assert!(items(&store).is_empty() && !effects.is_empty());
    }

    #[test]
    fn showing_subscribes_the_stream_to_the_space_and_a_preview() {
        let mut store = loaded();
        store.apply(fleet_frame(board()));
        let space = space_of(&store, "mupu").clone();
        let view = |agent: &str| {
            Event::Transcript(T::Show {
                space: space.id.clone(),
                agent: agent.into(),
            })
        };
        let streams = |effects: &[Effect]| -> Vec<Vec<String>> {
            let agents = effects.iter().filter_map(|e| match e {
                Effect::Stream { agents, .. } => Some(agents.clone()),
                _ => None,
            });
            agents.collect()
        };
        let mut members: Vec<String> = space.agents().map(String::from).collect();
        members.sort();
        assert_eq!(streams(&store.apply(view("mupu"))), vec![members.clone()]);
        let other = members.iter().find(|m| *m != "mupu").cloned();
        if let Some(other) = other {
            assert!(
                streams(&store.apply(view(&other))).is_empty(),
                "same space, same stream"
            );
        }
        let outsider = store
            .fleet
            .agents
            .keys()
            .find(|a| !members.contains(a))
            .unwrap()
            .clone();
        let effects = store.apply(view(&outsider));
        assert!(
            streams(&effects)[0].contains(&outsider),
            "a preview joins the stream"
        );
        assert_eq!(store.transcript.open.as_ref().unwrap().agent, outsider);
        assert_eq!(
            store.spaces,
            loaded_spaces(),
            "a preview never becomes a member"
        );
    }

    fn loaded_spaces() -> Vec<spaces::Space> {
        loaded().spaces
    }

    #[test]
    fn a_path_opens_only_on_one_strong_candidate_from_complete_roots() {
        let mut store = loaded();
        let effects = open(&mut store, "mupu");
        drive(&mut store, effects, &history("mupu"), PAGE as usize);
        let lookup = |store: &mut Store| {
            let effects = store.apply(Event::Transcript(T::OpenPath("src/x.rs:12".into())));
            effects.into_iter().next()
        };
        // Off the board (the serve would reject `agent=`), then on it.
        let Some(Effect::Fetch(Fetch::Transcript(read))) = lookup(&mut store) else {
            panic!()
        };
        assert_eq!(read.what, What::Resolve("src/x.rs".into(), Some(12), false));
        let (generation, frame) = (store.stream, StreamEvent::Frame(Wire::Fleet(board())));
        store.apply(Event::Stream {
            generation,
            event: frame,
        });
        let effects = lookup(&mut store);
        let Some(Effect::Fetch(Fetch::Transcript(read))) = effects else {
            panic!()
        };
        assert_eq!(read.what, What::Resolve("src/x.rs".into(), Some(12), true));
        let candidate = |tier: &str, score: f64| Candidate {
            root: "/home/u/repo/".into(),
            path: "src/x.rs".into(),
            kind: if tier == "prefix" { "dir" } else { "file" }.into(),
            tier: tier.into(),
            score,
        };
        let root = |status: &str| ResolveRoot {
            root: "/home/u/repo".into(),
            status: status.into(),
        };
        let answer = |store: &mut Store, candidates, roots| {
            let got = Got::Resolved(Resolved { candidates, roots });
            store.apply(Event::Transcript(T::Read(read.clone(), Ok(got))))
        };
        let opened = Effect::OpenFile {
            path: "/home/u/repo/src/x.rs".into(),
            line: Some(12),
        };
        assert_eq!(
            answer(
                &mut store,
                vec![candidate("exact", 900.)],
                vec![root("complete")]
            ),
            vec![opened.clone()]
        );
        // Ambiguous, incomplete or merely fuzzy: no guess, a notice.
        let two = vec![candidate("suffix", 400.), candidate("suffix", 300.)];
        assert!(answer(&mut store, two, vec![root("complete")]).is_empty());
        let one = || vec![candidate("exact", 900.)];
        assert!(answer(&mut store, one(), vec![root("degraded")]).is_empty());
        let fuzzy = vec![candidate("fuzzy", 9000.)];
        assert!(answer(&mut store, fuzzy, vec![root("complete")]).is_empty());
        let t = store.transcript.open.as_ref().unwrap();
        assert_eq!(t.notice(), Some("no single file matches src/x.rs"));
        assert!(!t.blocked(), "an unmatched path does not hold paging back");

        // VS Code opens a remote path without `:<line>` as a folder: a file gets line 1, a folder none.
        let read = Read {
            what: What::Resolve("src/x.rs".into(), None, true),
            ..read
        };
        let answer = |store: &mut Store, candidates| {
            let got = Got::Resolved(Resolved {
                candidates,
                roots: vec![root("complete")],
            });
            store.apply(Event::Transcript(T::Read(read.clone(), Ok(got))))
        };
        let file = |line| Effect::OpenFile {
            path: "/home/u/repo/src/x.rs".into(),
            line,
        };
        assert_eq!(answer(&mut store, one()), vec![file(Some(1))]);
        assert_eq!(
            answer(&mut store, vec![candidate("suffix", 1.)]),
            vec![file(Some(1))]
        );
        let dir = vec![Candidate {
            tier: "exact".into(),
            ..candidate("prefix", 1.)
        }];
        assert_eq!(answer(&mut store, dir), vec![file(None)]);
    }

    fn all_items() -> Vec<(Kind, Item)> {
        let entries = AGENTS.iter().flat_map(|a| history(a));
        entries
            .flat_map(|e| condense(&e).into_iter().map(move |i| (e.kind, i)))
            .collect()
    }

    use crate::api::Kind;

    #[test]
    fn every_fixture_kind_condenses_as_web_compact_does() {
        let items = all_items();
        let has = |f: &dyn Fn(&Item) -> bool| items.iter().any(|(_, i)| f(i));
        let text = |f: &dyn Fn(&Item) -> Option<String>| {
            items.iter().filter_map(|(_, i)| f(i)).collect::<Vec<_>>()
        };
        let prompts = text(&|i| {
            if let Item::Prompt(t) = i {
                Some(t.clone())
            } else {
                None
            }
        });
        assert!(prompts.contains(
            &"Read ~/slack-eyes/PLAYBOOK.md REHYDRATE section and resume Slack watch.".into()
        ));
        let dividers = text(&|i| {
            if let Item::CompactDivider(t) = i {
                Some(t.clone())
            } else {
                None
            }
        });
        assert!(
            dividers.contains(&"context compacted (manual, 219k → 5k tokens)".into()),
            "{dividers:?}"
        );
        assert!(dividers.contains(&"compaction summary".into()));
        let chips = text(&|i| {
            if let Item::SystemChip(t) = i {
                Some(t.clone())
            } else {
                None
            }
        });
        assert!(
            chips.iter().any(|c| c.starts_with("/compact Keep:")),
            "slash command: {chips:?}"
        );
        assert!(
            chips
                .iter()
                .any(|c| c.starts_with("Running scheduled task ("))
        );
        assert!(
            !chips
                .iter()
                .any(|c| c.contains("Compacted") || c.contains('\u{1b}')),
            "command output joins its command"
        );
        let notes = text(&|i| {
            if let Item::TaskNotification(t) = i {
                Some(t.clone())
            } else {
                None
            }
        });
        assert!(
            notes.len() == 3 && notes.iter().all(|n| n.starts_with("Monitor event:")),
            "{notes:?}"
        );
        let operator = items.iter().find_map(|(_, i)| match i {
            Item::Delivery {
                operator: true,
                text,
                ..
            } => Some(text.clone()),
            _ => None,
        });
        let operator = operator.expect("an operator delivery");
        assert!(!operator.contains("HERDER_WEB_OPERATOR") && !operator.starts_with('\n'));
        assert!(has(&|i| matches!(
            i,
            Item::Delivery {
                operator: false,
                quiet: false,
                ..
            }
        )));
        assert!(has(&|i| matches!(i, Item::Thinking(_))));
        assert!(has(&|i| matches!(i, Item::Assistant { .. })));
        for (kind, item) in &items {
            assert!(
                !matches!(
                    kind,
                    Kind::TurnDuration | Kind::InjectedSystem | Kind::HcomDeliveryStub
                ),
                "{kind:?} is hidden"
            );
            if let Item::Assistant { markdown } = item {
                assert!(
                    !markdown.contains("<internal>") && !markdown.contains("<status>"),
                    "{markdown}"
                );
            }
        }
        // The one delivery entry with three deliveries yields three rows at one offset.
        let lubo = history("grill-confirm-lubo");
        let many = lubo.iter().find(|e| condense(e).len() == 3);
        assert!(many.is_some(), "several deliveries share an entry");
        assert_eq!(
            condense::clean("a <internal>x</internal>b <status>ok</status>"),
            "a b ok"
        );
        assert_eq!(condense::clean("shown<internal>hidden to the end"), "shown");
    }

    #[test]
    fn tool_results_pair_with_their_calls_across_pages() {
        let all = history("mupu");
        let mut store = loaded();
        let effects = open(&mut store, "mupu");
        drive(&mut store, effects, &all, PAGE as usize);
        let before = items(&store).clone();
        while !store.transcript.open.as_ref().unwrap().at_start() {
            let effects = store.apply(Event::Transcript(T::Older));
            drive(&mut store, effects, &all, PAGE as usize);
        }
        let tools = |m: &BTreeMap<(u64, u16), Item>| {
            let paired = m.values().filter(|i| {
                matches!(
                    i,
                    Item::Tool {
                        result: Some(_),
                        ..
                    }
                )
            });
            paired.count()
        };
        let ids = |kind| all.iter().filter(|e| e.kind == kind).count();
        assert!(tools(items(&store)) >= tools(&before));
        assert_eq!(
            tools(items(&store)),
            ids(Kind::ToolUse).min(ids(Kind::ToolResult))
        );
        let error = Entry {
            byte_offset: u64::MAX - 1,
            kind: Kind::ToolResult,
            payload: json!({"tool_use_id": "x", "is_error": true, "content": "boom\nmore"}),
            ..Entry::default()
        };
        let call = Entry {
            byte_offset: u64::MAX - 2,
            kind: Kind::ToolUse,
            payload: json!({"tool_use_id": "x", "name": "Bash", "input": {"command": "false  &&\n true"}}),
            ..Entry::default()
        };
        let page = |entries| Entries {
            session_id: "s1".into(),
            entries,
            ..Entries::default()
        };
        let t = store.transcript.open.as_ref().unwrap();
        let (agent, generation) = (t.agent.clone(), t.generation);
        let session = "s1".to_string();
        for entry in [error, call] {
            let what = What::Page(Page::From {
                offset: 0,
                session: session.clone(),
                limit: PAGE,
            });
            let read = Read {
                agent: agent.clone(),
                generation,
                what,
            };
            store.apply(Event::Transcript(T::Read(
                read,
                Ok(Got::Page(Box::new(page(vec![entry])))),
            )));
        }
        let tool = items(&store).get(&(u64::MAX - 2, 0)).cloned();
        let result = Some(transcript::ToolResult {
            error: true,
            text: "boom".into(),
        });
        let summary = "false && true".to_string();
        assert_eq!(
            tool,
            Some(Item::Tool {
                name: "Bash".into(),
                summary,
                result
            }),
            "a result before its call"
        );
    }
}

/// U4: drafts, who can be written to, and each send's lifecycle.
mod composer {
    use super::*;
    use crate::api::{AgentDetail, Refusal};
    use crate::store::composer::{Failure, ReadOnly, Sending, Step as C};
    use crate::store::transcript::{Got, Step as T, What};

    fn edit(store: &mut Store, agent: &str, text: &str) -> Vec<Effect> {
        let (agent, text) = (agent.into(), text.into());
        store.apply(Event::Compose(C::Edit { agent, text }))
    }

    fn send(store: &mut Store, agent: &str, file_back: bool) -> Vec<Effect> {
        let agent = agent.into();
        store.apply(Event::Compose(C::Send { agent, file_back }))
    }

    fn sent(store: &mut Store, agent: &str, result: Result<(), Failure>) -> Vec<Effect> {
        store.apply(Event::Compose(C::Sent {
            agent: agent.into(),
            result,
        }))
    }

    fn messages(effects: &[Effect]) -> Vec<(String, String)> {
        let message = |e: &Effect| match e {
            Effect::Send(Write::Message { agent, text }) => Some((agent.clone(), text.clone())),
            _ => None,
        };
        effects.iter().filter_map(message).collect()
    }

    /// A live store zoomed on `agent` (in its first space), with its detail answered as `bus_status`.
    pub(super) fn zoomed(agent: &str, bus_status: Option<&str>) -> Store {
        let mut store = loaded();
        store.apply(fleet_frame(board()));
        let space = store.spaces[0].id.clone();
        let agent_name = agent.to_string();
        let effects = store.apply(Event::Transcript(T::Show {
            space,
            agent: agent_name,
        }));
        let detail = effects.into_iter().find_map(|e| match e {
            Effect::Fetch(Fetch::Transcript(r)) if r.what == What::Detail => Some(r),
            _ => None,
        });
        if let (Some(read), Some(status)) = (detail, bus_status) {
            let detail = AgentDetail {
                bus_status: status.into(),
                ..AgentDetail::default()
            };
            let got = Ok(Got::Detail(Box::new(detail)));
            store.apply(Event::Transcript(T::Read(read, got)));
        }
        store
    }

    #[test]
    fn drafts_are_kept_per_agent_and_persisted() {
        let mut store = zoomed("mupu", Some("listening"));
        assert_eq!(
            edit(&mut store, "mupu", "hello"),
            vec![Effect::Persist(Persist::Prefs)]
        );
        edit(&mut store, "riko", "other");
        assert!(
            edit(&mut store, "mupu", "hello").is_empty(),
            "no change, no write"
        );
        assert_eq!(store.prefs.drafts["mupu"], "hello");
        assert_eq!(store.prefs.drafts["riko"], "other");
        // Prefs round-trip through `prefs.json`: the drafts are restored.
        let back: Prefs = serde_json::from_slice(&crate::local::encode(&store.prefs)).unwrap();
        assert_eq!(back.drafts, store.prefs.drafts);
        // Emptying the box drops the draft.
        edit(&mut store, "mupu", "");
        assert!(!store.prefs.drafts.contains_key("mupu"));
        assert_eq!(store.prefs.drafts["riko"], "other");
    }

    #[test]
    fn can_send_names_every_read_only_state() {
        let name = "mupu";
        assert_eq!(zoomed(name, Some("listening")).can_send(name), Ok(()));
        assert_eq!(zoomed(name, None).can_send(name), Err(ReadOnly::Pending));
        assert_eq!(
            zoomed(name, Some("retired")).can_send(name),
            Err(ReadOnly::Retired)
        );
        let store = zoomed(name, Some("listening"));
        assert_eq!(store.can_send("gone-agent"), Err(ReadOnly::OffBoard));
        // Another agent than the open transcript's is pending, not retired.
        let other = store.fleet.agents.keys().find(|a| *a != name).unwrap();
        assert_eq!(store.can_send(other), Err(ReadOnly::Pending));
        let mut refused = zoomed(name, Some("listening"));
        refused.apply(Event::Viewer(Err(Some(409))));
        assert_eq!(refused.can_send(name), Err(ReadOnly::Refused));
        // Unknown attribution (a transport failure) still sends: the server decides.
        let mut unknown = zoomed(name, Some("listening"));
        unknown.apply(Event::Viewer(Err(None)));
        assert_eq!(unknown.viewer, Attribution::Unknown);
        assert_eq!(unknown.can_send(name), Ok(()));
    }

    #[test]
    fn a_send_goes_once_and_success_clears_the_draft() {
        let mut store = zoomed("mupu", Some("listening"));
        assert!(
            send(&mut store, "mupu", false).is_empty(),
            "nothing to send"
        );
        edit(&mut store, "mupu", "  ");
        assert!(
            send(&mut store, "mupu", false).is_empty(),
            "a blank draft stays"
        );
        edit(&mut store, "mupu", "hi there");
        let effects = send(&mut store, "mupu", false);
        assert_eq!(messages(&effects), vec![("mupu".into(), "hi there".into())]);
        assert!(store.in_flight("mupu"));
        // In flight: a second press and an edit do nothing.
        assert!(send(&mut store, "mupu", false).is_empty());
        assert!(edit(&mut store, "mupu", "changed").is_empty());
        assert_eq!(store.prefs.drafts["mupu"], "hi there");
        let effects = sent(&mut store, "mupu", Ok(()));
        assert_eq!(effects, vec![Effect::Persist(Persist::Prefs)]);
        assert!(!store.prefs.drafts.contains_key("mupu"));
        assert!(store.sends.is_empty());
        // A stray answer with nothing in flight changes nothing.
        assert!(sent(&mut store, "mupu", Ok(())).is_empty());
    }

    #[test]
    fn a_refused_send_keeps_the_text_and_says_why() {
        let failures = [
            Failure::Refused("sender refused".into()),
            Failure::Unreachable("hcom timed out".into()),
            Failure::UnknownAgent,
            Failure::NoAnswer("timed out".into()),
            Failure::NotSaved("disk full".into()),
        ];
        for failure in failures {
            let mut store = zoomed("mupu", Some("listening"));
            edit(&mut store, "mupu", "hi");
            send(&mut store, "mupu", false);
            assert!(sent(&mut store, "mupu", Err(failure.clone())).is_empty());
            assert_eq!(store.prefs.drafts["mupu"], "hi");
            assert_eq!(store.sends["mupu"], Sending::Failed(failure));
            // The owner can try again; editing clears the notice.
            assert!(store.ready("mupu"));
            edit(&mut store, "mupu", "hi!");
            assert!(store.sends.is_empty());
        }
    }

    #[test]
    fn nothing_resends_on_reconnect_or_retry_timers() {
        let mut store = zoomed("mupu", Some("listening"));
        edit(&mut store, "mupu", "once");
        assert_eq!(messages(&send(&mut store, "mupu", false)).len(), 1);
        let space = store.spaces[0].id.clone();
        let mut later = Vec::new();
        later.extend(store.apply(hello("b1")));
        later.extend(store.apply(Event::ViewerRetry));
        later.extend(store.apply(Event::Sync {
            ns: Ns::Spaces,
            step: Step::Retry,
        }));
        later.extend(store.apply(fleet_frame(board())));
        sent(&mut store, "mupu", Err(Failure::Unreachable("down".into())));
        later.extend(store.apply(hello("b1")));
        later.extend(store.apply(Event::Transcript(T::Show {
            space,
            agent: "mupu".into(),
        })));
        assert!(
            messages(&later).is_empty(),
            "only the owner sends: {later:?}"
        );
    }

    #[test]
    fn a_filed_back_send_acknowledges_only_what_was_there_when_sent() {
        let mut store = zoomed("mupu", Some("listening"));
        let mut b = board();
        let space = space_of(&store, "mupu").clone();
        let turn = store.fleet.agents["mupu"].turn_end.unwrap();
        store.prefs.seen.get_mut("mupu").unwrap().turn_end = turn - 1;
        edit(&mut store, "mupu", "done here");
        assert_eq!(messages(&send(&mut store, "mupu", true)).len(), 1);
        assert!(store.agent_needs_you("mupu"), "not seen until it lands");
        let filed = Effect::FiledBack {
            agent: "mupu".into(),
        };
        assert!(sent(&mut store, "mupu", Ok(())).contains(&filed));
        assert_eq!(store.prefs.seen["mupu"].turn_end, turn);
        // The owner moves to another agent; mupu finishes a newer turn and they mark its space unread
        // before the send lands: both survive the landing.
        edit(&mut store, "mupu", "again");
        send(&mut store, "mupu", true);
        let other = space.agents().find(|a| *a != "mupu").unwrap().to_string();
        let (id, agent) = (space.id.clone(), other);
        store.apply(Event::Transcript(T::Show { space: id, agent }));
        bump(&mut b, "mupu", 1);
        // On the stream the open transcript reopened (`fleet_frame` is generation 1's).
        let event = StreamEvent::Frame(Wire::Fleet(b.clone()));
        let generation = store.stream;
        store.apply(Event::Stream { generation, event });
        store.apply(Event::Lens(spaces::Move::Unread(space.id.clone())));
        assert!(sent(&mut store, "mupu", Ok(())).contains(&filed));
        assert!(store.agent_needs_you("mupu"), "the newer turn is unseen");
        assert!(store.prefs.unread.contains(&space.id));
        // A failure stays in the zoom, saying why, and acknowledges nothing.
        let mut store = zoomed("mupu", Some("listening"));
        store.prefs.seen.get_mut("mupu").unwrap().turn_end = turn - 1;
        edit(&mut store, "mupu", "once more");
        send(&mut store, "mupu", true);
        let effects = sent(&mut store, "mupu", Err(Failure::Unreachable("down".into())));
        assert!(effects.is_empty(), "{effects:?}");
        assert!(store.agent_needs_you("mupu"));
        let failed = Sending::Failed(Failure::Unreachable("down".into()));
        assert_eq!(store.sends["mupu"], failed);
    }

    #[test]
    fn a_block_that_ends_and_returns_during_a_file_back_still_needs_you() {
        let mut store = zoomed("mupu", Some("listening"));
        let space = space_of(&store, "mupu").id.clone();
        let mut b = board();
        let mut frame = |store: &mut Store, status: &str| {
            let panes = b.workspaces.iter_mut().flat_map(|w| &mut w.tabs);
            for pane in panes
                .flat_map(|t| &mut t.panes)
                .filter(|p| p.agent == "mupu")
            {
                pane.herdr_status = status.into();
            }
            let event = StreamEvent::Frame(Wire::Fleet(b.clone()));
            let generation = store.stream;
            store.apply(Event::Stream { generation, event });
        };
        frame(&mut store, "blocked");
        let agent = Some("mupu".to_string());
        store.apply(Event::Lens(spaces::Move::View { space, agent }));
        assert!(!store.agent_needs_you("mupu"), "this block is seen");
        edit(&mut store, "mupu", "unblock yourself");
        send(&mut store, "mupu", true);
        // Same turn: the block ends and a new one starts before the send lands.
        frame(&mut store, "idle");
        frame(&mut store, "blocked");
        assert!(store.agent_needs_you("mupu"));
        sent(&mut store, "mupu", Ok(()));
        assert!(
            store.agent_needs_you("mupu"),
            "the new block was never seen"
        );
    }

    #[test]
    fn a_late_viewer_answer_does_not_undo_a_send_refusal() {
        let why = Refusal {
            error: "sender refused".into(),
            detail: "web-vile already exists".into(),
        };
        for late in [Ok("web-me".into()), Err(None), Err(Some(409))] {
            let mut store = zoomed("mupu", Some("listening"));
            assert_eq!(store.viewer, Attribution::Unknown);
            assert!(store.viewer_asked, "the GET is out");
            edit(&mut store, "mupu", "hi");
            send(&mut store, "mupu", false);
            sent(&mut store, "mupu", Err(Failure::Unattributed(why.clone())));
            let effects = store.apply(Event::Viewer(late.clone()));
            assert!(effects.is_empty(), "{late:?}: {effects:?}");
            assert_eq!(store.viewer, Attribution::Refused(Some(why.clone())));
            assert!(!store.viewer_asked && !store.viewer_retry, "settled");
            assert_eq!(store.can_send("mupu"), Err(ReadOnly::Refused));
        }
    }

    #[test]
    fn an_attribution_refusal_makes_every_box_read_only() {
        let mut store = zoomed("mupu", Some("listening"));
        store.apply(Event::Viewer(Ok("web-me".into())));
        assert_eq!(store.can_send("mupu"), Ok(()));
        for error in ["attribution required", "sender refused"] {
            let mut store = store.clone();
            edit(&mut store, "mupu", "hi");
            send(&mut store, "mupu", false);
            let why = Refusal {
                error: error.into(),
                detail: "web-vile already exists".into(),
            };
            sent(&mut store, "mupu", Err(Failure::Unattributed(why.clone())));
            assert_eq!(store.viewer, Attribution::Refused(Some(why)));
            assert_eq!(store.can_send("mupu"), Err(ReadOnly::Refused));
            assert_eq!(store.prefs.drafts["mupu"], "hi", "the draft stays");
            // A hello does not ask again: the refusal holds until the app restarts.
            assert!(
                store
                    .apply(hello("b1"))
                    .iter()
                    .all(|e| *e != Effect::Fetch(Fetch::Viewer))
            );
            assert_eq!(store.can_send("mupu"), Err(ReadOnly::Refused));
        }
        // Any other 409 is this send's alone.
        edit(&mut store, "mupu", "hi");
        send(&mut store, "mupu", false);
        sent(
            &mut store,
            "mupu",
            Err(Failure::Refused("peer not found".into())),
        );
        assert_eq!(store.viewer, Attribution::Attributed("web-me".into()));
        assert!(store.ready("mupu"));
    }
}

mod notes {
    use super::*;
    use crate::store::notes::{Stamp, Step as N, transfer_text};

    /// `testdata/notes-web.json`: rows and hand-off text made by web's own code.
    fn web() -> serde_json::Value {
        serde_json::from_str(include_str!("../../testdata/notes-web.json")).unwrap()
    }

    fn web_row(i: usize) -> StateRow {
        serde_json::from_value(web()["rows"][i].clone()).unwrap()
    }

    fn stamp(now: i64, id: &str, write: &str) -> Stamp {
        Stamp {
            now,
            id: id.into(),
            write: write.into(),
        }
    }

    /// The rows a step queued (sent at once or after the POST in flight).
    fn note(store: &mut Store, step: N) -> Vec<StateRow> {
        let before = store.sync[&Ns::Notes].outbox.clone();
        store.apply(Event::Note(step));
        let after = store.sync[&Ns::Notes].outbox.values();
        after
            .filter(|r| before.get(&r.key) != Some(r))
            .cloned()
            .collect()
    }

    fn pulled(store: &mut Store, rows: Vec<StateRow>, rev: u64) {
        let step = Step::Pulled(StateRows { rows, rev });
        store.apply(Event::Sync {
            ns: Ns::Notes,
            step,
        });
    }

    fn texts(store: &Store, agent: &str) -> Vec<String> {
        store.notes_of(agent).map(|n| n.text.clone()).collect()
    }

    #[test]
    fn add_capture_edit_and_delete_write_webs_record_shape() {
        let mut store = loaded();
        let (quoted, plain, gone) = (web_row(0), web_row(1), web_row(2));
        // A capture: the quote trimmed, the source this agent's transcript, as web's capture writes it.
        let capture = N::Add {
            group: "mupu".into(),
            text: " ask it to split this ".into(),
            quote: Some("\nthe reducer owns\nevery mutation  ".into()),
            stamp: stamp(quoted.updated, &quoted.key, &quoted.write_id),
        };
        let mut got = note(&mut store, capture);
        // Web's record was edited once (`updated` > `created`); a fresh one has them equal.
        got[0].value["created"] = quoted.value["created"].clone();
        assert_eq!(got, [quoted]);
        // A typed note has no quote or source keys at all.
        let add = N::Add {
            group: "mupu".into(),
            text: plain.value["text"].as_str().unwrap().into(),
            quote: None,
            stamp: stamp(plain.updated, &plain.key, &plain.write_id),
        };
        assert_eq!(note(&mut store, add), std::slice::from_ref(&plain));
        assert_eq!(store.outbox()[&Ns::Notes].len(), 2);
        // An edit supersedes the version it saw, even with a clock behind it.
        let edit = N::Edit {
            id: plain.key.clone(),
            text: "check it twice".into(),
            stamp: stamp(5, "-", "w-edit"),
        };
        let edited = note(&mut store, edit).pop().unwrap();
        assert_eq!(
            (edited.updated, &*edited.write_id),
            (plain.updated + 1, "w-edit")
        );
        assert_eq!(edited.value["text"], "check it twice");
        assert_eq!(edited.value["created"], plain.value["created"]);
        // Unchanged, or emptied without a quote: nothing written.
        for text in ["check it twice", "  "] {
            let edit = N::Edit {
                id: plain.key.clone(),
                text: text.into(),
                stamp: stamp(9e12 as i64, "-", "w"),
            };
            assert!(note(&mut store, edit).is_empty(), "{text:?}");
        }
        // A delete is web's tombstone: `{id}` only.
        let delete = N::Delete {
            id: plain.key.clone(),
            stamp: stamp(gone.updated, "-", &gone.write_id),
        };
        assert_eq!(note(&mut store, delete), [gone]);
        assert_eq!(texts(&store, "mupu"), ["ask it to split this"]);
        // Blank adds write nothing.
        let blank = N::Add {
            group: "mupu".into(),
            text: " ".into(),
            quote: Some("\n".into()),
            stamp: stamp(1, "n", "w"),
        };
        assert!(note(&mut store, blank).is_empty());
    }

    #[test]
    fn notes_are_filtered_per_agent_and_a_newer_web_row_wins() {
        let mut store = loaded();
        let (quoted, plain) = (web_row(0), web_row(1));
        pulled(&mut store, vec![quoted.clone(), plain.clone()], 1);
        let general = StateRow {
            key: "g".into(),
            value: json!({"id": "g", "group": "general", "text": "for the rail", "created": 1}),
            ..row("g", 1, "w")
        };
        pulled(&mut store, vec![general], 2);
        assert_eq!(
            texts(&store, "mupu"),
            ["ask it to split this", "check the outbox after a 409"]
        );
        assert!(store.notes_of("riko").next().is_none());
        assert!(store.notes_of("general").count() == 1);

        // Edited here, then web's later edit of the same note arrives: web's wins, and the queued
        // row is dropped rather than sent over it.
        let edit = N::Edit {
            id: plain.key.clone(),
            text: "native's".into(),
            stamp: stamp(plain.updated + 10, "-", "w-native"),
        };
        store.apply(Event::Note(edit));
        let mut web_edit = plain.clone();
        web_edit.updated += 20;
        web_edit.value["text"] = "web's".into();
        pulled(&mut store, vec![web_edit], 3);
        assert_eq!(texts(&store, "mupu"), ["ask it to split this", "web's"]);
        assert!(queued(&store, Ns::Notes).is_empty());
        // An older remote row loses to the local edit.
        let edit = N::Edit {
            id: quoted.key.clone(),
            text: "mine".into(),
            stamp: stamp(quoted.updated + 50, "-", "w-native"),
        };
        store.apply(Event::Note(edit));
        pulled(&mut store, vec![quoted], 4);
        assert_eq!(texts(&store, "mupu")[0], "mine");
        assert_eq!(queued(&store, Ns::Notes).len(), 1);
    }

    #[test]
    fn transfer_text_is_webs() {
        let mut store = loaded();
        pulled(&mut store, vec![web_row(0), web_row(1)], 1);
        let got: Vec<String> = store.notes_of("mupu").map(transfer_text).collect();
        let want: Vec<String> = serde_json::from_value(web()["handoff"].clone()).unwrap();
        assert_eq!(got, want);
        // File and diff sources (web's file panes can file notes on an agent) fence the quote.
        let mut n = store.notes_of("mupu").next().unwrap().clone();
        n.source =
            Some(json!({"kind": "diff", "path": "a.rs", "base": "main", "start": 3, "end": 5}));
        n.quote = Some("x ``` y".into());
        n.text = String::new();
        assert_eq!(transfer_text(&n), "a.rs:3-5 (vs main)\n````\nx ``` y\n````");
    }

    #[test]
    fn hand_off_appends_to_the_draft_and_deletes_the_notes_at_once() {
        let mut store = super::composer::zoomed("mupu", Some("listening"));
        pulled(&mut store, vec![web_row(0), web_row(1)], 1);
        store.prefs.drafts.insert("mupu".into(), "first".into());
        let effects = store.apply(Event::Note(N::HandOff {
            agent: "mupu".into(),
            stamp: stamp(1, "-", "w-h"),
        }));
        let want: Vec<String> = serde_json::from_value(web()["handoff"].clone()).unwrap();
        assert_eq!(
            store.prefs.drafts["mupu"],
            format!("first\n\n{}", want.join("\n\n"))
        );
        let tombs = sends(&effects).concat();
        assert_eq!(tombs.len(), 2);
        assert!(tombs.iter().all(|r| r.deleted && r.write_id == "w-h"));
        assert!(effects.contains(&Effect::Persist(Persist::Prefs)));
        assert!(effects.contains(&Effect::Persist(Persist::Outbox)));
        assert!(store.notes_of("mupu").next().is_none());
        // Nothing left: nothing happens.
        let again = N::HandOff {
            agent: "mupu".into(),
            stamp: stamp(2, "-", "w"),
        };
        assert!(store.apply(Event::Note(again)).is_empty());
    }

    #[test]
    fn hand_off_waits_for_a_box_that_can_take_it() {
        // A retired agent's box is read-only, and a send in flight holds its draft: the notes stay.
        let mut store = super::composer::zoomed("mupu", Some("retired"));
        pulled(&mut store, vec![web_row(1)], 1);
        let hand = || N::HandOff {
            agent: "mupu".into(),
            stamp: stamp(1, "-", "w"),
        };
        assert!(store.apply(Event::Note(hand())).is_empty());
        let mut store = super::composer::zoomed("mupu", Some("listening"));
        pulled(&mut store, vec![web_row(1)], 1);
        store.prefs.drafts.insert("mupu".into(), "hi".into());
        store.apply(Event::Compose(crate::store::composer::Step::Send {
            agent: "mupu".into(),
            file_back: false,
        }));
        assert!(store.apply(Event::Note(hand())).is_empty());
        assert_eq!(store.notes_of("mupu").count(), 1);
    }

    #[test]
    fn alt_enter_queues_the_draft_as_a_note_and_clears_the_box() {
        let mut store = super::composer::zoomed("mupu", Some("listening"));
        store
            .prefs
            .drafts
            .insert("mupu".into(), "  later: ask about tests ".into());
        let queue = |s: &str| N::Queue {
            agent: s.into(),
            stamp: stamp(7, "q1", "w-q"),
        };
        let effects = store.apply(Event::Note(queue("mupu")));
        assert!(!store.prefs.drafts.contains_key("mupu"));
        assert!(effects.contains(&Effect::Persist(Persist::Prefs)));
        let rows = sends(&effects).concat();
        assert_eq!(rows.len(), 1);
        assert_eq!(
            rows[0].value,
            json!({"id": "q1", "group": "mupu", "text": "later: ask about tests", "created": 7})
        );
        assert_eq!(texts(&store, "mupu"), ["later: ask about tests"]);
        // A blank draft queues nothing.
        store.prefs.drafts.insert("mupu".into(), "  ".into());
        assert!(store.apply(Event::Note(queue("mupu"))).is_empty());
        assert_eq!(store.prefs.drafts["mupu"], "  ");
    }
}
