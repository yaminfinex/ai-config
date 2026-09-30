//! What stays on this Mac, under `~/Library/Application Support/herder-native/`:
//! `prefs.json` (text scale; U1 adds rows, visible agents, seen marks, drafts, hotkey), `outbox.json`
//! (unsent state rows, written before each send attempt; U1) and `snapshot.json` (the last board, spaces,
//! members and notes, applied synchronously at boot before anything live starts; U1).
//!
//! One writer for all of them: every save carries a sequence number from `next_seq`, an older save never
//! lands on top of a newer one, and each file is written to a temporary name and renamed into place.

use crate::store::Prefs;
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::sync::atomic::{AtomicU64, Ordering};

static SEQ: AtomicU64 = AtomicU64::new(0);
/// The highest sequence number written so far, per file name.
static WRITTEN: Mutex<Vec<(&'static str, u64)>> = Mutex::new(Vec::new());

fn dir() -> PathBuf {
    let home = std::env::var("HOME").unwrap_or_else(|_| ".".into());
    PathBuf::from(home).join("Library/Application Support/herder-native")
}

/// Take a sequence number on the foreground when the save is decided, then hand it to the writer.
pub fn next_seq() -> u64 {
    SEQ.fetch_add(1, Ordering::SeqCst) + 1
}

pub fn load_prefs() -> Option<Prefs> {
    let bytes = std::fs::read(dir().join("prefs.json")).ok()?;
    serde_json::from_slice(&bytes).ok()
}

/// Best effort: a failed save is logged, never fatal. Stale saves (a lower `seq` than one already
/// written for this file) are dropped.
pub fn save_prefs(prefs: &Prefs, seq: u64) {
    let bytes = serde_json::to_vec_pretty(prefs).expect("prefs serialize");
    write_atomic("prefs.json", &bytes, seq);
}

fn write_atomic(name: &'static str, bytes: &[u8], seq: u64) {
    let mut written = WRITTEN.lock().unwrap_or_else(|e| e.into_inner());
    let slot = match written.iter_mut().find(|(n, _)| *n == name) {
        Some((_, last)) => last,
        None => {
            written.push((name, 0));
            &mut written.last_mut().expect("just pushed").1
        }
    };
    if seq <= *slot {
        return;
    }
    let path = dir().join(name);
    match replace_file(&path, bytes) {
        Ok(()) => *slot = seq,
        Err(e) => eprintln!("local: could not save {}: {e}", path.display()),
    }
}

fn replace_file(path: &Path, bytes: &[u8]) -> std::io::Result<()> {
    let dir = path.parent().expect("file has a parent");
    std::fs::create_dir_all(dir)?;
    let tmp = path.with_extension("tmp");
    std::fs::write(&tmp, bytes)?;
    std::fs::rename(&tmp, path)
}
