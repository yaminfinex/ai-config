//! Store reductions from the recorded fixtures: state and effects, no network, no clock.

use super::*;
use crate::api::{Hello, Member, StateChanged, StateRows};
use crate::store::fleet::Status;
use crate::store::sync::Hold;
use serde_json::json;

fn board() -> Board {
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

fn fleet_frame(board: Board) -> Event {
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
fn loaded() -> Store {
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
        store.prefs.seen["mupu"], turn,
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

    let effects = store.apply(Event::Seen("mupu".into()));
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
fn the_viewer_is_asked_again_on_each_hello_until_the_server_answers() {
    let asks = |effects: Vec<Effect>| {
        effects
            .iter()
            .filter(|e| **e == Effect::Fetch(Fetch::Viewer))
            .count()
    };
    let mut store = Store::default();
    assert_eq!(asks(store.apply(Event::Boot)), 1);
    assert_eq!(asks(store.apply(hello("b1"))), 0, "one ask at a time");
    // The boot ask failed in transport (offline launch): unknown, not unattributed.
    store.apply(Event::Viewer(Err(None)));
    assert_eq!(asks(store.apply(hello("b1"))), 1, "a reconnect asks again");
    store.apply(Event::Viewer(Ok("web-me".into())));
    assert_eq!(store.viewer, Attribution::Attributed("web-me".into()));
    assert_eq!(asks(store.apply(hello("b1"))), 0);

    let mut store = Store::default();
    store.apply(Event::Boot);
    store.apply(Event::Viewer(Err(Some(409))));
    assert_eq!(store.viewer, Attribution::Refused);
    assert_eq!(asks(store.apply(hello("b1"))), 0, "a refusal is an answer");
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
