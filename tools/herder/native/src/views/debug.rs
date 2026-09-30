//! A throwaway view that shows the data is live: the connection, then each space in lens order with its
//! members and each agent's status. Plain text, theme tokens only. U2's lens replaces it.

use crate::api::Member;
use crate::store::{Attribution, Conn, Store};
use crate::views::theme::TypeScale;
use gpui_kit::{Hsla, IntoElement, ParentElement, Styled, div, prelude::*};

pub fn render(store: &Store, t: TypeScale, muted: Hsla) -> impl IntoElement {
    let conn = match &store.conn {
        Conn::Offline => "offline".to_string(),
        Conn::Live { build } => format!("live · {build}"),
    };
    let updated = if store.server_updated {
        " · server updated"
    } else {
        ""
    };
    let viewer = match &store.viewer {
        Attribution::Unknown => "viewer unknown",
        Attribution::Attributed(v) => v.as_str(),
        Attribution::Refused => "unattributed",
    };
    let header = format!(
        "{conn}{updated} · {viewer} · {} agents · {} spaces · {} notes",
        store.fleet.agents.len(),
        store.spaces.len(),
        store.notes.len()
    );
    let spaces = store.spaces.iter().map(|space| {
        let members = space.members.iter().map(|m| match m {
            Member::Agent { name } => {
                let agent = store.fleet.agents.get(name);
                let status = agent.map_or("gone".to_string(), |a| format!("{:?}", a.status()));
                let unseen =
                    agent.is_some_and(|a| a.needs_you(store.prefs.seen.get(name).copied()));
                format!(
                    "  {name} · {status}{}",
                    if unseen { " · needs you" } else { "" }
                )
            }
            Member::File { root, path } => format!("  file {root}/{path}"),
        });
        div()
            .flex()
            .flex_col()
            .child(div().text_size(t.title).child(format!(
                "{} ({})",
                space.name,
                store.needs_you(space)
            )))
            .children(members.map(|line| div().text_color(muted).child(line)))
    });
    div()
        .id("debug")
        .size_full()
        .overflow_y_scroll()
        .p(t.body)
        .flex()
        .flex_col()
        .gap(t.body)
        .child(div().text_size(t.small).text_color(muted).child(header))
        .children(spaces)
}
