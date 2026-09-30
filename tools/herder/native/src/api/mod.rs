//! Everything that talks to herder serve, and nothing else.
//!
//! - `types`: the wire models, decoded tolerantly (unknown fields are ignored, most fields default).
//! - `client`: blocking HTTP. Called from background threads only; it never touches GPUI or the store.
//! - `sse`: the `/api/events` frame parser and the reconnecting reader.
//!
//! Contract: `tools/herder/docs/web-api-contract.md`. Always the tailnet address (settled decision 6):
//! loopback is unattributed, which refuses every write and every `/api/state` read.

pub mod client;
pub mod sse;
pub mod types;

pub use types::*;
