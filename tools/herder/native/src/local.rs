//! What stays on this Mac, under `~/Library/Application Support/herder-native/`: `prefs.json` (text
//! scale, rows, visible agents, seen marks, drafts), `layouts.json` (each space's dock, DK2), `outbox.json` (unsent state rows, saved before
//! every send) and `snapshot.json` (the last board and state rows, applied synchronously at boot
//! before anything live starts). A panic's message and backtrace are appended to
//! `~/Library/Logs/herder-native/panic.log` (`log_panics`): the bundle has no stderr, and a panic in an
//! event handler aborts the app.
//!
//! One writer for all of them (`Disk`): every save carries a sequence number from `next_seq`, an older
//! save never lands on top of a newer one, and each file is written to a temporary name and renamed
//! into place.

use crate::store::spaces::Layouts;
use crate::store::{Outbox, Prefs, Snapshot};
use serde::Serialize;
use serde::de::DeserializeOwned;
use std::collections::HashMap;
use std::io;
use std::path::PathBuf;
use std::sync::Mutex;
use std::sync::atomic::{AtomicU64, Ordering};

pub const PREFS: &str = "prefs.json";
pub const LAYOUTS: &str = "layouts.json";
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

    pub fn load_layouts(&self) -> Option<Layouts> {
        self.load(LAYOUTS)
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

/// Append every panic's message, thread and backtrace to `dir/panic.log`, then run the hook that was
/// installed before (the default prints it and the panic goes on, so a panic across the platform's
/// event callback still aborts).
pub fn log_panics(dir: PathBuf) {
    let before = std::panic::take_hook();
    std::panic::set_hook(Box::new(move |info| {
        let backtrace = std::backtrace::Backtrace::force_capture();
        let at = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH);
        let at = at.map_or(0, |d| d.as_secs());
        let thread = std::thread::current();
        let thread = thread.name().unwrap_or("unnamed");
        let entry = format!("--- {at} (unix s), thread '{thread}'\n{info}\n{backtrace}\n");
        let _ = std::fs::create_dir_all(&dir).and_then(|()| {
            use std::io::Write;
            let path = dir.join("panic.log");
            let mut file = std::fs::OpenOptions::new()
                .create(true)
                .append(true)
                .open(path)?;
            file.write_all(entry.as_bytes())
        });
        before(info);
    }));
}

/// `~/Library/Logs/herder-native`.
pub fn logs() -> PathBuf {
    let home = std::env::var("HOME").unwrap_or_else(|_| ".".into());
    PathBuf::from(home).join("Library/Logs/herder-native")
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

    #[test]
    fn a_panic_is_appended_to_the_log_with_its_backtrace() {
        let dir = std::env::temp_dir().join(format!("herder-native-panic-{}", std::process::id()));
        log_panics(dir.clone());
        let caught = std::panic::catch_unwind(|| panic!("wheel went wrong"));
        // Back to the default hook, so other tests' panics print as usual.
        drop(std::panic::take_hook());
        assert!(caught.is_err());
        let log = std::fs::read_to_string(dir.join("panic.log")).unwrap();
        assert!(log.contains("wheel went wrong"), "{log}");
        assert!(
            log.contains("a_panic_is_appended_to_the_log"),
            "a backtrace: {log}"
        );
        std::fs::remove_dir_all(&dir).unwrap();
    }
}
