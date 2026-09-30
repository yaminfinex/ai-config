//! What stays on this Mac, under `~/Library/Application Support/herder-native/`: `prefs.json` (text
//! scale, rows, visible agents, seen marks, drafts), `outbox.json` (unsent state rows, saved before
//! every send) and `snapshot.json` (the last board and state rows, applied synchronously at boot
//! before anything live starts).
//!
//! One writer for all of them (`Disk`): every save carries a sequence number from `next_seq`, an older
//! save never lands on top of a newer one, and each file is written to a temporary name and renamed
//! into place.

use crate::store::{Outbox, Prefs, Snapshot};
use serde::Serialize;
use serde::de::DeserializeOwned;
use std::collections::HashMap;
use std::io;
use std::path::PathBuf;
use std::sync::Mutex;
use std::sync::atomic::{AtomicU64, Ordering};

pub const PREFS: &str = "prefs.json";
pub const OUTBOX: &str = "outbox.json";
pub const SNAPSHOT: &str = "snapshot.json";

static SEQ: AtomicU64 = AtomicU64::new(0);

/// Take a sequence number on the foreground when the save is decided, then hand it to the writer.
pub fn next_seq() -> u64 {
    SEQ.fetch_add(1, Ordering::SeqCst) + 1
}

/// Serialize on the caller's thread (cheap next to the write), then hand the bytes to `Disk::write`.
pub fn encode(value: &impl Serialize) -> Vec<u8> {
    serde_json::to_vec_pretty(value).expect("local state serializes")
}

/// The local state directory and its writer.
pub struct Disk {
    dir: PathBuf,
    /// The highest sequence number saved so far, per file name.
    written: Mutex<HashMap<&'static str, u64>>,
}

impl Disk {
    /// `~/Library/Application Support/herder-native`.
    pub fn home() -> Disk {
        let home = std::env::var("HOME").unwrap_or_else(|_| ".".into());
        Disk::at(PathBuf::from(home).join("Library/Application Support/herder-native"))
    }

    pub fn at(dir: PathBuf) -> Disk {
        Disk {
            dir,
            written: Mutex::default(),
        }
    }

    fn load<T: DeserializeOwned>(&self, name: &str) -> Option<T> {
        let bytes = std::fs::read(self.dir.join(name)).ok()?;
        match serde_json::from_slice(&bytes) {
            Ok(v) => Some(v),
            Err(e) => {
                eprintln!("local: ignoring unreadable {name}: {e}");
                None
            }
        }
    }

    pub fn load_prefs(&self) -> Option<Prefs> {
        self.load(PREFS)
    }

    pub fn load_outbox(&self) -> Option<Outbox> {
        self.load(OUTBOX)
    }

    pub fn load_snapshot(&self) -> Option<Snapshot> {
        self.load(SNAPSHOT)
    }

    /// Save `bytes` as `name`. `Ok` when saved or when a newer save (a higher `seq`) already landed,
    /// since that one holds everything this one did; `Err` when the disk refused.
    pub fn write(&self, name: &'static str, bytes: &[u8], seq: u64) -> io::Result<()> {
        let mut written = self.written.lock().unwrap_or_else(|e| e.into_inner());
        let last = written.entry(name).or_default();
        if seq <= *last {
            return Ok(());
        }
        std::fs::create_dir_all(&self.dir)?;
        let (path, tmp) = (self.dir.join(name), self.dir.join(format!("{name}.tmp")));
        std::fs::write(&tmp, bytes)?;
        std::fs::rename(&tmp, &path)?;
        *last = seq;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn an_older_save_never_lands_on_a_newer_one_and_a_refusal_is_reported() {
        let dir = std::env::temp_dir().join(format!("herder-native-local-{}", std::process::id()));
        let disk = Disk::at(dir.clone());
        let (old, new) = (next_seq(), next_seq());
        disk.write(OUTBOX, b"new", new).unwrap();
        disk.write(OUTBOX, b"old", old).unwrap();
        assert_eq!(std::fs::read(dir.join(OUTBOX)).unwrap(), b"new");

        // A directory that cannot exist: its parent is a file.
        let blocked = Disk::at(dir.join(OUTBOX).join("x"));
        assert!(blocked.write(OUTBOX, b"x", next_seq()).is_err());
        std::fs::remove_dir_all(&dir).unwrap();
    }
}
