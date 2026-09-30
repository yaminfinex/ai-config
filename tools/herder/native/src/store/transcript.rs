//! One agent's transcript as the compact view renders it (U3). Pages arrive in both directions, so this
//! is not an append-only fold.
//!
//! One wire entry can yield several items (an `hcom_delivery` entry carries every delivery in that
//! injection; the fixtures show three at one offset), so the row key is `(byte_offset, sub)` with `sub`
//! the item's index within its entry. `items: BTreeMap<(u64, u16), Item>` — row order is key order, so a
//! `before=` page is an insertion and no stored index ever moves. Tool pairing uses
//! `calls: HashMap<tool_use_id, (u64, u16)>` plus `orphans: HashMap<tool_use_id, ToolResult>`: a result
//! whose call is known fills it, otherwise it waits in `orphans` until an older page brings the call.
//! Cursors: `next_offset` is set only by tail and `from=` responses; `prev_offset` is seeded from the
//! tail's `window.from` and then from each `before=` page's `prevOffset` (`0` = start of file); a
//! `before=` response never touches `next_offset`. Every request carries `(session_id, generation)` and
//! a response with a stale tag is dropped; `rewindow`/`reset` bumps the generation, clears everything and
//! re-reads the tail. Assistant text has `<internal>…</internal>` stripped and `<status>` unwrapped.
