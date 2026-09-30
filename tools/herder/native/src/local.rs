//! What stays on this Mac, under `~/Library/Application Support/herder-native/`: `prefs.json` (text
//! scale, rows, visible agents, seen marks, drafts), `outbox.json` (unsent state rows, written before
//! each send attempt) and `snapshot.json` (the last board and state rows, applied synchronously at boot
//! before anything live starts).
//!
//! One writer for all of them: every save carries a sequence number from `next_seq`, an older save never
//! lands on top of a newer one, and each file is written to a temporary name and renamed into place.

use crate::store::{Outbox, Prefs, Snapshot};
use serde::Serialize;
use serde::de::DeserializeOwned;
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::sync::atomic::{AtomicU64, Ordering};

pub const PREFS: &str = "prefs.json";
pub const OUTBOX: &str = "outbox.json";
pub const SNAPSHOT: &str = "snapshot.json";

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

fn load<T: DeserializeOwned>(name: &str) -> Option<T> {
    let bytes = std::fs::read(dir().join(name)).ok()?;
    match serde_json::from_slice(&bytes) {
        Ok(v) => Some(v),
        Err(e) => {
            eprintln!("local: ignoring unreadable {name}: {e}");
            None
        }
    }
}

pub fn load_prefs() -> Option<Prefs> {
    load(PREFS)
}

pub fn load_outbox() -> Option<Outbox> {
    load(OUTBOX)
}

pub fn load_snapshot() -> Option<Snapshot> {
    load(SNAPSHOT)
}

/// Serialize on the caller's thread (cheap next to the write), then hand the bytes to `write`.
pub fn encode(value: &impl Serialize) -> Vec<u8> {
    serde_json::to_vec_pretty(value).expect("local state serializes")
}

/// Best effort: a failed save is logged, never fatal. Stale saves (a lower `seq` than one already
/// written for this file) are dropped.
pub fn write(name: &'static str, bytes: &[u8], seq: u64) {
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
