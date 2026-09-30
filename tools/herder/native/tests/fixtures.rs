//! The recorded fixtures under testdata/ decode into the `api` types. This is the api harness test;
//! the units grow it as they add endpoints.

use herder_native::api::sse::{Frame, read_frames};
use herder_native::api::{
    AgentDetail, Board, Entries, Kind, Member, MembersValue, NoteValue, Refusal, SpaceValue,
    StateRows, Viewer, Wire,
};
use herder_native::store::fleet::agent_rows;
use std::collections::HashSet;
use std::path::PathBuf;

fn fixture(rel: &str) -> String {
    let p = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("testdata")
        .join(rel);
    std::fs::read_to_string(&p).unwrap_or_else(|e| panic!("{}: {e}", p.display()))
}

#[test]
fn fleet_board_decodes_with_agents() {
    let board: Board = serde_json::from_str(&fixture("fleet.json")).unwrap();
    let names: Vec<&str> = agent_rows(&board)
        .into_iter()
        .map(|(_, p)| p.agent.as_str())
        .collect();
    assert!(names.contains(&"mupu"), "{names:?}");
    assert!(names.len() > 10);
}

#[test]
fn sse_fixture_yields_hello_then_fleet() {
    let mut frames: Vec<Frame> = Vec::new();
    read_frames(fixture("events.sse").as_bytes(), |f| frames.push(f)).unwrap();
    assert_eq!(
        frames.iter().map(|f| f.event.as_str()).collect::<Vec<_>>(),
        ["hello", "fleet"]
    );
    assert!(
        matches!(Wire::decode(&frames[0].event, &frames[0].data), Wire::Hello(h) if !h.build_identity.is_empty())
    );
    let Wire::Fleet(board) = Wire::decode(&frames[1].event, &frames[1].data) else {
        panic!("the second frame is the board")
    };
    assert!(!agent_rows(&board).is_empty());
}

#[test]
fn viewer_and_agent_details_decode() {
    let viewer: Viewer = serde_json::from_str(&fixture("viewer.json")).unwrap();
    assert!(!viewer.viewer.is_empty());
    for agent in ["mupu", "conductor-line", "grill-confirm-lubo"] {
        let d: AgentDetail =
            serde_json::from_str(&fixture(&format!("agents/{agent}/detail.json"))).unwrap();
        assert_eq!(d.name, agent);
        assert!(d.session_id.is_some());
    }
}

#[test]
fn entries_cover_every_kind_and_page_backward() {
    let mut seen = HashSet::new();
    for agent in ["mupu", "conductor-line", "grill-confirm-lubo", "riko"] {
        let tail: Entries =
            serde_json::from_str(&fixture(&format!("agents/{agent}/tail.json"))).unwrap();
        assert_eq!(tail.window.mode, "tail");
        assert!(tail.next_offset.is_some());
        seen.extend(tail.entries.iter().map(|e| e.kind));

        let before: Entries =
            serde_json::from_str(&fixture(&format!("agents/{agent}/before.json"))).unwrap();
        assert_eq!(before.window.mode, "before");
        assert_eq!(before.session_id, tail.session_id);
        let prev = before.prev_offset.expect("before= pages carry prevOffset");
        assert_eq!(prev, before.entries[0].byte_offset);
        assert!(
            before
                .entries
                .iter()
                .all(|e| e.byte_offset < tail.window.from)
        );
    }
    let every = [
        Kind::HumanPrompt,
        Kind::HcomDeliveryStub,
        Kind::HcomDelivery,
        Kind::TaskNotification,
        Kind::InjectedSystem,
        Kind::CommandStdout,
        Kind::CompactDivider,
        Kind::AssistantText,
        Kind::Thinking,
        Kind::ToolUse,
        Kind::ToolResult,
        Kind::TurnDuration,
        Kind::SystemChip,
    ];
    let missing: Vec<_> = every.iter().filter(|k| !seen.contains(k)).collect();
    assert!(missing.is_empty(), "kinds not in the fixtures: {missing:?}");
}

#[test]
fn state_namespaces_decode_into_shared_values() {
    let spaces: StateRows = serde_json::from_str(&fixture("state-spaces.json")).unwrap();
    let live: Vec<SpaceValue> = spaces
        .rows
        .iter()
        .filter(|r| !r.deleted)
        .map(|r| serde_json::from_value(r.value.clone()).unwrap())
        .collect();
    assert!(!live.is_empty());

    let members: StateRows = serde_json::from_str(&fixture("state-spaces.members.json")).unwrap();
    let mut agents = 0;
    for row in members.rows.iter().filter(|r| !r.deleted) {
        assert!(
            live.iter().any(|s| s.id == row.key),
            "members row {} names a live space",
            row.key
        );
        let v: MembersValue = serde_json::from_value(row.value.clone()).unwrap();
        agents += v
            .members
            .iter()
            .filter(|m| matches!(m, Member::Agent { .. }))
            .count();
    }
    assert!(agents > 0, "web has published space membership");

    let notes: StateRows = serde_json::from_str(&fixture("state-notes.json")).unwrap();
    assert!(notes.rev > 0);
    for row in notes.rows.iter().filter(|r| !r.deleted) {
        let n: NoteValue = serde_json::from_value(row.value.clone()).unwrap();
        assert_eq!(n.id, row.key);
    }
}

#[test]
fn a_refusal_body_decodes() {
    let r: Refusal =
        serde_json::from_str(r#"{"error":"state namespace not found","detail":"x"}"#).unwrap();
    assert_eq!(r.error, "state namespace not found");
}
