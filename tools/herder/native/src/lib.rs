//! herder native: the macOS client for herder serve, on GPUI.
//!
//! The module map, the one-way dependency rule and the line budgets live in `ARCHITECTURE.md`.
//! Dependencies point one way: `views` → `store` → `api` types; `shell` wires them together.
//! `tests/layering.rs` fails the build when a module reaches the wrong way.

pub mod api;
pub mod harness;
pub mod local;
pub mod platform_mac;
pub mod shell;
pub mod store;
pub mod views;
