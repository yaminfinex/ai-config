//! What stays on this Mac, under `~/Library/Application Support/herder-native/`:
//! `prefs.json` (text scale; U1 adds rows, visible agents, seen marks, drafts, state-sync cursors and
//! outbox, hotkey) and `snapshot.json` (the last board, spaces and members, painted at launch before
//! the network answers; U1). Loaded once at boot into `Event`s; saved when an `Effect::Persist` says so.

use crate::store::Prefs;
use std::path::PathBuf;

fn dir() -> PathBuf {
    let home = std::env::var("HOME").unwrap_or_else(|_| ".".into());
    PathBuf::from(home).join("Library/Application Support/herder-native")
}

pub fn load_prefs() -> Option<Prefs> {
    let bytes = std::fs::read(dir().join("prefs.json")).ok()?;
    serde_json::from_slice(&bytes).ok()
}

/// Best effort: a failed save is logged, never fatal.
pub fn save_prefs(prefs: &Prefs) {
    let path = dir().join("prefs.json");
    let result = std::fs::create_dir_all(dir()).and_then(|_| {
        std::fs::write(
            &path,
            serde_json::to_vec_pretty(prefs).expect("prefs serialize"),
        )
    });
    if let Err(e) = result {
        eprintln!("prefs: could not save {}: {e}", path.display());
    }
}
