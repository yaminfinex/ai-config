//! One agent's transcript as the compact view renders it (U3).
//!
//! Raw `api::Entry` rows fold into `Item`s: prompts, deliveries, task notifications, system chips,
//! compact dividers, assistant markdown (with `<internal>…</internal>` stripped and `<status>` unwrapped),
//! thinking, tool calls paired with their results by `tool_use_id`, and errors. Items keep their
//! `(session_id, byte_offset)` so the list has stable keys. Two cursors: `next_offset` reads forward on
//! `entry:` wakes, `prev_offset` pages backward with `before=` until it reaches `0`. A `reset` or
//! `rewindow` throws the window away and re-reads the tail.
