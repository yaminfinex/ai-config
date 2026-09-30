//! Pure state. Events in; state and effects out. No GPUI, no I/O, no clocks: the time of day arrives
//! inside events, so every reduction is deterministic and replays from fixtures.
//!
//! The one mutation rule: `Store::apply` is the only place state changes, and the shell calls it on
//! the foreground thread only. Views read `&Store`; they never hold `&mut`.
//!
//! - `fleet`: agents and their status, derived from the board.
//! - `spaces`: spaces, their members, the owner's row choices and visible agent, needs-you/seen.
//! - `transcript`: entries → compact items, paging cursors, tool/result pairing.
//! - `notes`: notes and drafts, with the `/api/state` sync cursors and outbox.

pub mod fleet;
pub mod notes;
pub mod spaces;
pub mod transcript;

use crate::api::Board;
use serde::{Deserialize, Serialize};

/// Connection state, for the status dot and read-only decisions.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub enum Conn {
    #[default]
    Offline,
    /// Live; `build` is the server's `hello.buildIdentity`, so a change shows "server updated".
    Live { build: String },
}

/// Owner preferences that live on this Mac (`local::prefs.json`). U1 grows it.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Prefs {
    /// App-wide text scale; every size is a token times this (see `views::theme`).
    pub text_scale: f32,
}

impl Default for Prefs {
    fn default() -> Self {
        Prefs { text_scale: 1.0 }
    }
}

const SCALE_STEP: f32 = 1.1;
const SCALE_RANGE: std::ops::RangeInclusive<f32> = 0.7..=1.8;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TextScale {
    Bigger,
    Smaller,
    Reset,
}

/// Something that happened: a network result, a user action, a tick or a boot-time load.
#[derive(Clone, Debug)]
pub enum Event {
    PrefsLoaded(Prefs),
    Connected { build: String },
    Disconnected,
    Board(Board),
    TextScale(TextScale),
}

/// Something the shell must now do off the store: fetch, send, persist, notify.
#[derive(Clone, Debug, PartialEq)]
pub enum Effect {
    Persist(Prefs),
}

#[derive(Clone, Debug, Default)]
pub struct Store {
    pub conn: Conn,
    pub prefs: Prefs,
    pub fleet: fleet::Fleet,
}

impl Store {
    /// Reduce one event. Returns the effects it implies, in order.
    pub fn apply(&mut self, event: Event) -> Vec<Effect> {
        match event {
            Event::PrefsLoaded(p) => self.prefs = p,
            Event::Connected { build } => self.conn = Conn::Live { build },
            Event::Disconnected => self.conn = Conn::Offline,
            Event::Board(board) => self.fleet.ingest(&board),
            Event::TextScale(step) => {
                let s = self.prefs.text_scale;
                let next = match step {
                    TextScale::Bigger => s * SCALE_STEP,
                    TextScale::Smaller => s / SCALE_STEP,
                    TextScale::Reset => 1.0,
                };
                self.prefs.text_scale =
                    (next.clamp(*SCALE_RANGE.start(), *SCALE_RANGE.end()) * 100.0).round() / 100.0;
                return vec![Effect::Persist(self.prefs.clone())];
            }
        }
        Vec::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::store::fleet::Status;

    fn board() -> Board {
        serde_json::from_str(include_str!("../../testdata/fleet.json")).expect("fleet.json decodes")
    }

    #[test]
    fn board_event_fills_the_fleet_and_disconnect_keeps_it() {
        let mut store = Store::default();
        assert_eq!(store.conn, Conn::Offline);
        store.apply(Event::Connected {
            build: "source:abc".into(),
        });
        assert_eq!(
            store.conn,
            Conn::Live {
                build: "source:abc".into()
            }
        );

        let effects = store.apply(Event::Board(board()));
        assert!(effects.is_empty());
        let mupu = store
            .fleet
            .agents
            .get("mupu")
            .expect("mupu is on the recorded board");
        assert_eq!(mupu.tool, "claude");
        assert_eq!(mupu.status(), Status::Done);
        assert_eq!(mupu.workspace, "~");
        assert!(store.fleet.agents.len() > 10);

        store.apply(Event::Disconnected);
        assert_eq!(store.conn, Conn::Offline);
        assert!(
            store.fleet.agents.contains_key("mupu"),
            "the last board survives a drop"
        );
    }

    #[test]
    fn text_scale_steps_clamps_and_persists() {
        let mut store = Store::default();
        assert_eq!(
            store.apply(Event::TextScale(TextScale::Bigger)),
            vec![Effect::Persist(Prefs { text_scale: 1.1 })]
        );
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
}
