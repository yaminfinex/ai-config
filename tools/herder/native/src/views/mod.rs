//! GPUI views. They render from `&Store` and dispatch `Event`s through the shell; domain state never
//! changes here. Each view owns the GPUI widget entities it renders with (`ListState`, `TextareaState`,
//! `EditorState`); those are widget state, not domain state, and never go through the store. Every size
//! comes from `theme::type_scale`.
//!
//! Planned files, one per surface, added by the unit that needs them:
//! `lens` (U2, the home rows and cards), `space` (U2, the zoom shell and tabs), `transcript` (U3),
//! `composer` (U4), `notes` (U5). `theme` (palette and type scale) is here from A0.

pub mod theme;
