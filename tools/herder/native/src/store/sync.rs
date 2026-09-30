//! `/api/state` sync for one namespace, per ARCHITECTURE §6 and web's `stateSync.ts`.
//!
//! No persisted cursor: every boot pulls `since=0`, and the cursor then lives here for the session. Rows
//! resolve last-write-wins on `(updated, writeID)`. The outbox is durable (the shell writes
//! `outbox.json` before any send) and its cleanup is version-aware, because the server's `accepted`
//! list omits idempotent and losing rows:
//! - a 2xx POST retires every queued row whose version is equal to or older than the one sent, so an
//!   edit made while the POST was in flight stays queued;
//! - after every pull, a queued row is discarded when the pulled row for its key is equal or newer;
//! - 409 means local-only until a pull succeeds; 413 holds the outbox until the next local edit;
//!   anything else retries with backoff 500 ms → 10 s.
//!
//! `Sync` reports what a step changed (`Changes`), so the store re-derives and persists only then.

use crate::api::{StateRow, StateRows};
use crate::store::{Effect, Fetch, Write};
use serde::{Deserialize, Serialize};
use std::cmp::Ordering;
use std::collections::BTreeMap;

/// The state namespaces this client syncs. The serde names are the server's.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
pub enum Ns {
    #[serde(rename = "spaces")]
    Spaces,
    #[serde(rename = "spaces.members")]
    Members,
    #[serde(rename = "notes")]
    Notes,
}

impl Ns {
    pub const ALL: [Ns; 3] = [Ns::Spaces, Ns::Members, Ns::Notes];

    pub fn name(self) -> &'static str {
        match self {
            Ns::Spaces => "spaces",
            Ns::Members => "spaces.members",
            Ns::Notes => "notes",
        }
    }

    pub fn from_name(name: &str) -> Option<Ns> {
        Ns::ALL.into_iter().find(|ns| ns.name() == name)
    }
}

const BACKOFF_MS: u64 = 500;
const BACKOFF_MAX_MS: u64 = 10_000;

/// Why sending is paused.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Hold {
    /// 409: this Mac is not attributed; edits stay local until a pull succeeds.
    LocalOnly,
    /// 413: the server refused the batch; held until the next local edit.
    TooLarge,
}

/// One thing that happened to a namespace's sync: a network answer, a timer or a local edit.
#[derive(Clone, Debug)]
pub enum Step {
    Pulled(StateRows),
    /// A failed request: its HTTP status, or `None` for a transport failure (or, for a POST, an
    /// `outbox.json` that could not be saved, so nothing was sent).
    PullFailed(Option<u16>),
    Posted,
    PostFailed(Option<u16>),
    /// A backoff elapsed.
    Retry,
    /// A local edit: full rows with `(updated, writeID)` already set.
    Edit(Vec<StateRow>),
}

/// What a step changed, for the store: rows re-derive the domain and refresh the snapshot; a changed
/// outbox is saved (the shell also saves it before every send).
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Changes {
    pub rows: bool,
    pub outbox: bool,
}

#[derive(Clone, Debug)]
pub struct Sync {
    ns: Ns,
    /// The winning row per key, tombstones included (a tombstone still wins comparisons).
    pub rows: BTreeMap<String, StateRow>,
    /// Local edits not yet known to be on the server, the newest per key. Mirrored to `outbox.json`.
    pub outbox: BTreeMap<String, StateRow>,
    pub hold: Option<Hold>,
    cursor: u64,
    pulling: bool,
    pull_again: bool,
    /// The rows of the POST in flight.
    sending: Option<Vec<StateRow>>,
    retry_pending: bool,
    backoff_ms: u64,
    changes: Changes,
}

impl Sync {
    pub fn new(ns: Ns) -> Self {
        Sync {
            ns,
            rows: BTreeMap::new(),
            outbox: BTreeMap::new(),
            hold: None,
            cursor: 0,
            pulling: false,
            pull_again: false,
            sending: None,
            retry_pending: false,
            backoff_ms: BACKOFF_MS,
            changes: Changes::default(),
        }
    }

    /// Reduce one step; the effects go to `out`.
    pub fn apply(&mut self, step: Step, out: &mut Vec<Effect>) -> Changes {
        match step {
            Step::Pulled(rows) => self.pulled(rows, out),
            Step::PullFailed(status) => {
                self.pulling = false;
                self.refused(status, out);
            }
            Step::Posted => self.posted(out),
            Step::PostFailed(status) => {
                self.sending = None;
                self.refused(status, out);
            }
            Step::Retry => {
                // The backoff elapsed: pull (which sends after). A 409 cancels the retry.
                self.retry_pending = false;
                if self.hold != Some(Hold::LocalOnly) {
                    self.pull(out);
                }
            }
            Step::Edit(rows) => {
                // Apply, queue, and send unless held for attribution. It lifts a 413 hold.
                self.queue(rows);
                if self.hold == Some(Hold::TooLarge) {
                    self.hold = None;
                }
                self.send(out);
            }
        }
        std::mem::take(&mut self.changes)
    }

    /// Last-write-wins merge into `rows`.
    fn merge(&mut self, rows: impl IntoIterator<Item = StateRow>) {
        for row in rows {
            if newer(&row, self.rows.get(&row.key)) {
                self.rows.insert(row.key.clone(), row);
                self.changes.rows = true;
            }
        }
    }

    /// Queue rows restored from `outbox.json` (or just edited). They are sent after the next pull or
    /// at once, whichever `send` allows.
    fn queue(&mut self, rows: Vec<StateRow>) {
        self.merge(rows.iter().cloned());
        for row in rows {
            if newer(&row, self.outbox.get(&row.key)) {
                self.outbox.insert(row.key.clone(), row);
                self.changes.outbox = true;
            }
        }
    }

    pub fn pull(&mut self, out: &mut Vec<Effect>) {
        if self.pulling {
            self.pull_again = true;
            return;
        }
        self.pulling = true;
        out.push(Effect::Fetch(Fetch::State {
            ns: self.ns,
            since: self.cursor,
        }));
    }

    /// A `state-changed` nudge: pull only when it is ahead of what we have.
    pub fn changed(&mut self, rev: u64, out: &mut Vec<Effect>) {
        if rev > self.cursor {
            self.pull(out);
        }
    }

    /// A pull answered: take the rows, drop the queued rows they dominate, send what is left.
    fn pulled(&mut self, got: StateRows, out: &mut Vec<Effect>) {
        self.pulling = false;
        if self.hold == Some(Hold::LocalOnly) {
            self.hold = None;
        }
        self.cursor = self.cursor.max(got.rev);
        for remote in &got.rows {
            if self
                .outbox
                .get(&remote.key)
                .is_some_and(|q| remote.version_cmp(q) != Ordering::Less)
            {
                self.outbox.remove(&remote.key);
                self.changes.outbox = true;
            }
        }
        self.merge(got.rows);
        if std::mem::take(&mut self.pull_again) {
            self.pull(out);
        }
        self.send(out);
    }

    /// A POST answered 2xx: retire what it covered, then pull.
    fn posted(&mut self, out: &mut Vec<Effect>) {
        for sent in self.sending.take().unwrap_or_default() {
            if self
                .outbox
                .get(&sent.key)
                .is_some_and(|q| q.version_cmp(&sent) != Ordering::Greater)
            {
                self.outbox.remove(&sent.key);
                self.changes.outbox = true;
            }
        }
        self.backoff_ms = BACKOFF_MS;
        self.pull(out);
    }

    fn refused(&mut self, status: Option<u16>, out: &mut Vec<Effect>) {
        match status {
            Some(409) => self.hold = Some(Hold::LocalOnly),
            Some(413) => self.hold = Some(Hold::TooLarge),
            _ if !self.retry_pending => {
                self.retry_pending = true;
                out.push(Effect::Retry {
                    ns: self.ns,
                    after_ms: self.backoff_ms,
                });
                self.backoff_ms = (self.backoff_ms * 2).min(BACKOFF_MAX_MS);
            }
            _ => {}
        }
    }

    /// Post a copy of the whole outbox, unless one is in flight, a hold is on, or a retry is waiting.
    fn send(&mut self, out: &mut Vec<Effect>) {
        if self.outbox.is_empty()
            || self.sending.is_some()
            || self.hold.is_some()
            || self.retry_pending
        {
            return;
        }
        let rows: Vec<StateRow> = self.outbox.values().cloned().collect();
        self.sending = Some(rows.clone());
        out.push(Effect::Send(Write::State { ns: self.ns, rows }));
    }
}

/// Every namespace's sync. Indexed by `&Ns`.
#[derive(Clone, Debug)]
pub struct Syncs(BTreeMap<Ns, Sync>);

impl Default for Syncs {
    fn default() -> Self {
        Syncs(Ns::ALL.into_iter().map(|ns| (ns, Sync::new(ns))).collect())
    }
}

impl std::ops::Index<&Ns> for Syncs {
    type Output = Sync;
    fn index(&self, ns: &Ns) -> &Sync {
        &self.0[ns]
    }
}

impl Syncs {
    pub fn get_mut(&mut self, ns: Ns) -> &mut Sync {
        self.0.get_mut(&ns).expect("every namespace has a sync")
    }

    /// Rows from disk: the snapshot's (`queued: false`) or the outbox's, which are also sent. Loading
    /// is not a change: nothing needs saving again.
    pub fn restore(&mut self, rows: BTreeMap<Ns, Vec<StateRow>>, queued: bool) {
        for (ns, rows) in rows {
            let s = self.get_mut(ns);
            if queued {
                s.queue(rows)
            } else {
                s.merge(rows)
            }
            s.changes = Changes::default();
        }
    }

    /// The winning rows (`snapshot.json`) or the queued ones (`outbox.json`), empty namespaces left out.
    pub fn export(&self, queued: bool) -> BTreeMap<Ns, Vec<StateRow>> {
        self.0
            .iter()
            .map(|(ns, s)| {
                let rows = if queued { &s.outbox } else { &s.rows };
                (*ns, rows.values().cloned().collect::<Vec<_>>())
            })
            .filter(|(_, rows)| !rows.is_empty())
            .collect()
    }
}

/// True when `row` beats `current` (or there is none).
fn newer(row: &StateRow, current: Option<&StateRow>) -> bool {
    current.is_none_or(|c| row.version_cmp(c) == Ordering::Greater)
}
