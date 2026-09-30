//! Spaces and what is in them (U1), plus the owner's lens choices (U2).
//!
//! Spaces come from the `spaces` namespace and members from `spaces.members` (both server rows shared
//! with web, tombstones dropped, ordered by `order`; members in dock order). Local, never on the
//! server: the row each space sits in (focus / watch / background), the visible agent per space, and
//! the seen mark per agent, which is what "needs you" compares the agent's last activity against.
