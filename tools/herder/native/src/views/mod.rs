//! Thin GPUI views. They render from `&Store` and dispatch `Event`s through the shell; they never
//! mutate state themselves. The only state a view owns is the kit widget state it needs (a `ListState`,
//! a `TextareaState`), held in the shell's `Ui` struct. Every size comes from `theme::type_scale`.
//!
//! Planned files, one per surface, added by the unit that needs them:
//! `lens` (U2, the home rows and cards), `space` (U2, the zoom shell and tabs), `transcript` (U3),
//! `composer` (U4), `notes` (U5). `theme` (palette and type scale) is here from A0.

pub mod theme;
