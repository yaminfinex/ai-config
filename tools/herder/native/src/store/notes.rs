//! Notes and drafts (U4, U5).
//!
//! Notes are the `notes` namespace rows web writes: per agent (`group`), with an optional quote and
//! source. Sync is pull-then-push with a persisted revision cursor and outbox; a `state-changed` frame
//! above the cursor pulls again, 409 means local-only, 413 holds the outbox until the next edit.
//! Drafts are one string per agent and stay on this Mac.
