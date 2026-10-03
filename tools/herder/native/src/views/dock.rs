//! The dock in the zoom (DK2): the zoomed space's agent panels (`panel`) as tabs in groups, split side
//! by side or stacked, on the kit's dock (`DockArea`: groups, splits, drag and drop, resize, maximize).
//! A space's members are its pinned tabs; a tab of an agent that is not a member is a preview (italic,
//! a hollow dot), at most one in a group, replaced by the next one opened there and pinned (made a
//! member) by a double-click, a send the store takes, or a drag to another group. Closing a pinned tab
//! removes the member, as web's does. A member removed on another device stays as a preview, or closes
//! if its group has one (`leave`).
//!
//! The zoom's agent is the focused panel's: focus moving into a panel moves it (`follow`, read at each
//! sync: after every action and whenever the shell renders, not on a timer), and an action that names
//! another agent opens or shows its tab and focuses it (`sync`). "Beside" is the first other group in
//! the layout, not the nearest one on screen (a declared limitation). The store is told
//! what is on screen, the focused agent and each other group's shown tab (`Move::View`), whenever that
//! changes. The tab strip is drawn to web's measurements (`tabs`); the rest of the dock's look is the
//! kit's.
//!
//! Each space's dock is kept as laid out (`Event::Layout` on every change; `layouts.json` is written at
//! most every 250 ms, the latest dump at the end of a fixed window opened by the first change) and opened that way on the next zoom in, reconciled with the members (`restore`); a dock
//! that cannot be restored opens on the members in one group. Maximize is not kept.

use crate::store::spaces::{Move, Space};
use crate::store::{Event, Store};
use crate::views::lens::Ui;
use crate::views::panel::{AgentPanel, Panel};
use crate::views::space::{Anim, Zoom};
use crate::views::{Host, tabs};
use gpui_kit::component::Placement;
use gpui_kit::component::dock::*;
use gpui_kit::*;
use std::rc::Rc;
use std::sync::Arc;

/// Close a tab (`cmd-w`, its ×, a middle-click): `None` is the focused one.
#[derive(Clone, Debug, PartialEq, Action)]
#[action(namespace = dock, no_json)]
pub struct Close(pub Option<SharedString>);

/// Pin a preview tab: its agent joins the space (a double-click on it).
#[derive(Clone, Debug, PartialEq, Action)]
#[action(namespace = dock, no_json)]
pub struct Pin(pub SharedString);

/// Maximize the focused group, or put it back (`alt-enter`, the group's □).
#[derive(Clone, Copy, Debug, PartialEq, Action)]
#[action(namespace = dock, no_json)]
pub struct Maximize;

/// `cmd-1`…`cmd-9`: the focused group's tab, from 1.
#[derive(Clone, Copy, Debug, PartialEq, Action)]
#[action(namespace = dock, no_json)]
pub struct Nth(pub usize);

/// What an action asks of the dock, done at the next `sync` (an action has no window).
#[derive(Clone, Debug, PartialEq)]
pub enum Ask {
    /// Open the zoom's agent beside the focused group, not in it (`alt`-click).
    Beside,
    /// The focused group's next or previous tab, wrapping (`tab`, `alt-right`), or its `Nth` (from 0).
    Step(isize),
    Nth(usize),
    Close(String),
    Maximize,
}

/// The dock of the zoomed space.
pub struct Dock {
    pub(super) space: String,
    pub(super) area: Entity<DockArea>,
    /// The members as last synced: one that joins since (another device) opens as a tab.
    members: Vec<String>,
    /// Each group's tabs as last synced: a tab found in another group was dragged there; a group with
    /// the same tabs in another order had one dropped in it, the one it shows (a drop shows it).
    groups: Vec<(NodeId, Vec<PanelId>)>,
    /// The group focus was last in, where a tab opens.
    group: Option<NodeId>,
    /// The agent whose panel held focus at the last sync: focus found in another since moved there (a
    /// click in it), and its agent becomes the zoom's.
    held: Option<String>,
    /// What the store was last told is on screen: the focused agent and those beside it.
    pub(super) told: Option<(Option<String>, Vec<String>)>,
    _changed: Subscription,
}

impl Dock {
    /// The groups, each with its tabs and the one it shows (a maximized group alone shows).
    fn tabs(&self, cx: &App) -> Vec<(NodeId, Vec<PanelId>, usize)> {
        let area = self.area.read(cx);
        let Some(tree) = area.layout(DockPlacement::Center) else {
            return Vec::new();
        };
        let mut out = Vec::new();
        tree.root().walk(&mut |n| {
            if let PaneRef::Tabs { panels, active_ix } = n.kind() {
                out.push((n.id(), panels.to_vec(), active_ix));
            }
        });
        out
    }

    /// The tabs on screen: each group's shown one, or the maximized group's.
    fn shown(&self, cx: &App) -> Vec<PanelId> {
        let max = self.area.read(cx).zoomed_group();
        let groups = self.tabs(cx).into_iter();
        let groups = groups.filter(|(node, _, _)| max.is_none_or(|m| m == *node));
        groups.filter_map(|(_, p, ix)| p.get(ix).copied()).collect()
    }

    fn group_of(&self, id: PanelId, cx: &App) -> Option<NodeId> {
        let tree = self.area.read(cx).layout(DockPlacement::Center)?;
        tree.find_panel_node(id)
    }
}

pub(super) fn agent_of(ui: &Ui, id: PanelId) -> Option<&str> {
    ui.panels
        .iter()
        .find(|(_, p)| p.id == id)
        .map(|(a, _)| a.as_str())
}

pub(super) fn members(store: &Store, space: &str) -> Vec<String> {
    let space = store.spaces.iter().find(|s| s.id == space);
    space
        .into_iter()
        .flat_map(Space::agents)
        .map(String::from)
        .collect()
}

/// Before a frame, and after every action: the zoomed space's dock, its tabs as asked, the zoom's agent
/// shown and focused there, a panel for each tab (shown or hidden as its tab is), and none for anything
/// else (the lens once the morph back has landed). What the store must be told comes back.
pub fn sync<H: Host>(
    ui: &mut Ui,
    store: &Store,
    window: &mut Window,
    cx: &mut Context<H>,
) -> Vec<Event> {
    let leaving = ui.anim.as_ref().and_then(Anim::leaving).cloned();
    let live = ui.zoom.is_some();
    let Some(zoom) = ui.zoom.clone().or(leaving) else {
        ui.asks.clear();
        ui.dock = None;
        drop_panels(ui, window, cx, |_| false);
        return Vec::new();
    };
    if ui.dock.as_ref().is_none_or(|d| d.space != zoom.space) {
        drop_panels(ui, window, cx, |_| false);
        let dock = open(ui, store, &zoom, window, cx);
        ui.dock = Some(dock);
    }
    let mut beside = false;
    for ask in std::mem::take(&mut ui.asks) {
        match ask {
            Ask::Beside => beside = true,
            Ask::Close(agent) => close(ui, &agent, window, cx),
            Ask::Maximize => maximize(ui, window, cx),
            Ask::Step(by) => pick(ui, cx, |at, n| {
                (at as isize + by).rem_euclid(n as isize) as usize
            }),
            Ask::Nth(i) => pick(ui, cx, |_, _| i),
        }
    }
    if live {
        follow(ui, window, cx);
        join(ui, store, window, cx);
        leave(ui, store, window, cx);
        show(ui, beside, window, cx);
    }
    let Some(dock) = ui.dock.as_mut() else {
        return Vec::new();
    };
    let shown = dock.shown(cx);
    // A group maximized over the zoom's agent's, or a tab in a dock that had none (a member added on
    // another device): the zoom moves to what shows, and focus with it from an empty dock.
    let on = |a: &str| ui.panels.get(a).is_some_and(|p| shown.contains(&p.id));
    if live && !ui.zoomed_agent().is_some_and(on) {
        let first = shown
            .first()
            .and_then(|id| agent_of(ui, *id))
            .map(String::from);
        let gained = ui.zoomed_agent().is_none();
        if let (Some(first), Some(zoom)) = (first, ui.zoom.as_mut()) {
            zoom.agent = Some(first);
            if gained {
                window.focus(ui.focus_target(), cx);
            }
        }
    }
    let Some(dock) = ui.dock.as_mut() else {
        return Vec::new();
    };
    let tabs = dock.tabs(cx);
    dock.groups = tabs
        .into_iter()
        .map(|(node, panels, _)| (node, panels))
        .collect();
    dock.members = members(store, &zoom.space);
    let held: Vec<PanelId> = dock.groups.iter().flat_map(|g| g.1.clone()).collect();
    drop_panels(ui, window, cx, |p| held.contains(&p.id));
    let mut strand = false;
    let focused = window.focused(cx);
    for p in ui.panels.values_mut() {
        let on = shown.contains(&p.id);
        if p.shown && !on {
            strand |= focused
                .as_ref()
                .is_some_and(|f| p.focus.contains(f, window));
            p.hide();
        }
        p.shown |= on;
    }
    if strand {
        window.focus(ui.focus_target(), cx);
    }
    if live {
        tell(ui, &zoom.space, cx)
    } else {
        Vec::new()
    }
}

/// Focus moved into a panel since the last sync (a click in it): its agent is the zoom's. Focus still
/// where it was leaves the zoom to an action that moved it (focus follows it, `views::on`).
fn follow(ui: &mut Ui, window: &Window, cx: &App) {
    let focused = window.focused(cx);
    let held = ui.panels.iter().find(|(_, p)| {
        p.shown
            && focused
                .as_ref()
                .is_some_and(|f| p.focus.contains(f, window))
    });
    // Focus is read against the frame last drawn: an element that took it since (a box just opened)
    // is not there yet, so finding no panel says nothing.
    let (Some(held), Some(dock)) = (held.map(|(a, _)| a.clone()), ui.dock.as_mut()) else {
        return;
    };
    let moved = dock.held.as_ref() != Some(&held);
    dock.held = Some(held.clone());
    if let (true, Some(zoom)) = (moved, ui.zoom.as_mut()) {
        zoom.agent = Some(held);
    }
}

/// Drop the panels `keep` does not keep; focus left in one goes to the zoom's.
fn drop_panels<H: Host>(
    ui: &mut Ui,
    window: &mut Window,
    cx: &mut Context<H>,
    keep: impl Fn(&Panel) -> bool,
) {
    let focused = window.focused(cx);
    let mut strand = false;
    ui.panels.retain(|_, p| {
        let kept = keep(p);
        strand |= !kept
            && focused
                .as_ref()
                .is_some_and(|f| p.focus.contains(f, window));
        kept
    });
    if strand {
        window.focus(ui.focus_target(), cx);
    }
}

/// The agent's panel, made if it has none, and its view for the dock.
fn panel<H: Host>(
    ui: &mut Ui,
    agent: &str,
    window: &mut Window,
    cx: &mut Context<H>,
) -> Arc<dyn BasePanelView> {
    let web = ui.web.clone();
    let p = ui.panels.entry(agent.to_string());
    let p = p.or_insert_with(|| Panel::new(agent, &web, window, cx));
    p.view.clone()
}

/// A space's dock, opened on its members as tabs in one group, and the zoom's agent if not one of them
/// (a preview).
fn open<H: Host>(
    ui: &mut Ui,
    store: &Store,
    zoom: &Zoom,
    window: &mut Window,
    cx: &mut Context<H>,
) -> Dock {
    let host = cx.entity().downgrade();
    let area = cx.new(|cx| {
        let skin = Rc::new(tabs::Skin {
            kit: DockSkin::new(cx),
            host,
        });
        DockArea::new(
            SharedString::from(format!("dock-{}", zoom.space)),
            Some(1),
            window,
            cx,
        )
        .with_renderer(skin)
    });
    let mut agents = members(store, &zoom.space);
    let saved = store.layouts.spaces.get(&zoom.space).cloned();
    let saved = saved.and_then(|v| serde_json::from_value::<PanelState>(v).ok());
    let tree = match saved.and_then(|s| restore(&s, &agents)) {
        Some(tree) => tree,
        None => {
            let preview = zoom.agent.clone().filter(|a| !agents.contains(a));
            agents.extend(preview);
            let at = agents.iter().position(|a| Some(a) == zoom.agent.as_ref());
            let active = at.unwrap_or(0);
            let tabs = (Tree::Tabs { agents, active }, None);
            Tree::Split(Axis::Horizontal, vec![tabs])
        }
    };
    let layout = build(ui, &tree, window, cx);
    area.update(cx, |a, cx| a.set_center(layout, window, cx));
    let changed = cx.subscribe_in(&area, window, |h: &mut H, _, e: &DockEvent, window, cx| {
        if let DockEvent::LayoutChanged = e {
            changed(h, window, cx);
        }
    });
    Dock {
        space: zoom.space.clone(),
        area,
        members: members(store, &zoom.space),
        groups: Vec::new(),
        group: None,
        held: None,
        told: None,
        _changed: changed,
    }
}

/// A dock as laid out by agent: splits of groups, each group's tabs and the one it shows.
#[derive(Clone, Debug, PartialEq)]
pub(super) enum Tree {
    Split(Axis, Vec<(Tree, Option<Pixels>)>),
    Tabs { agents: Vec<String>, active: usize },
}

/// A space's saved dock (the kit's dump of its tree) as it can open now: each member once, where it
/// was; in each group at most one preview (an agent not a member), the one saved as its preview (`mark`)
/// else the first (a member removed while the app was closed shows as one); the members not in it
/// added to its first group; empty groups and splits gone. `None` when nothing is left, or the dump is
/// not a tree of agent tabs.
pub(super) fn restore(saved: &PanelState, members: &[String]) -> Option<Tree> {
    let mut placed = Vec::new();
    let mut tree = reconcile(saved, members, &mut placed)?;
    let missing = members.iter().filter(|m| !placed.contains(m)).cloned();
    first_tabs(&mut tree)?.extend(missing);
    Some(tree)
}

fn reconcile(state: &PanelState, members: &[String], placed: &mut Vec<String>) -> Option<Tree> {
    match &state.info {
        PanelInfo::Stack { sizes, axis } => {
            let axis = if *axis == 0 {
                Axis::Horizontal
            } else {
                Axis::Vertical
            };
            let children = state.children.iter().enumerate().filter_map(|(i, c)| {
                let size = sizes.get(i).copied().filter(|s| *s > Pixels::ZERO);
                Some((reconcile(c, members, placed)?, size))
            });
            let children: Vec<_> = children.collect();
            (!children.is_empty()).then_some(Tree::Split(axis, children))
        }
        PanelInfo::Tabs { active_index } => {
            fn agent(tab: &PanelState) -> Option<&str> {
                match &tab.info {
                    PanelInfo::Panel(info) => info.get("agent").and_then(|a| a.as_str()),
                    _ => None,
                }
            }
            let marked = |tab: &&PanelState| match &tab.info {
                PanelInfo::Panel(info) => info.get("preview") == Some(&true.into()),
                _ => false,
            };
            let outside = state.children.iter().filter(|t| {
                agent(t).is_some_and(|a| {
                    !members.iter().any(|m| m == a) && !placed.iter().any(|p| p == a)
                })
            });
            let preview = outside.clone().find(marked).or(outside.clone().next());
            let preview = preview.and_then(agent).map(String::from);
            let (mut agents, mut active) = (Vec::new(), 0);
            for (i, tab) in state.children.iter().enumerate() {
                let Some(agent) = agent(tab) else {
                    continue;
                };
                let member = members.iter().any(|m| m == agent);
                if placed.iter().any(|p| p == agent)
                    || (!member && preview.as_deref() != Some(agent))
                {
                    continue;
                }
                if i <= *active_index {
                    active = agents.len();
                }
                placed.push(agent.to_string());
                agents.push(agent.to_string());
            }
            (!agents.is_empty()).then_some(Tree::Tabs { agents, active })
        }
        PanelInfo::Panel(_) => None,
    }
}

fn first_tabs(tree: &mut Tree) -> Option<&mut Vec<String>> {
    match tree {
        Tree::Tabs { agents, .. } => Some(agents),
        Tree::Split(_, children) => children.first_mut().and_then(|c| first_tabs(&mut c.0)),
    }
}

/// The kit's layout for `tree`, a panel made for each agent.
fn build<H: Host>(
    ui: &mut Ui,
    tree: &Tree,
    window: &mut Window,
    cx: &mut Context<H>,
) -> DockLayout {
    match tree {
        Tree::Split(axis, children) => {
            let split = match axis {
                Axis::Horizontal => DockLayout::h_split(),
                Axis::Vertical => DockLayout::v_split(),
            };
            children.iter().fold(split, |split, (child, size)| {
                split.child(build(ui, child, window, cx), *size)
            })
        }
        Tree::Tabs { agents, active } => {
            let tabs = agents.iter().fold(DockLayout::tabs(), |tabs, agent| {
                tabs.panel_view(panel(ui, agent, window, cx), cx)
            });
            tabs.active_index(*active)
        }
    }
}

/// The zoom's agent's tab shown, opened in the focused group if it has none (`beside`: in the group
/// beside it, or a new one to its right), a preview there replacing the group's.
fn show<H: Host>(ui: &mut Ui, beside: bool, window: &mut Window, cx: &mut Context<H>) {
    let Some(agent) = ui.zoomed_agent().map(String::from) else {
        return;
    };
    let view = panel(ui, &agent, window, cx);
    let (store_members, dock) = (
        ui.dock.as_ref().map(|d| d.members.clone()),
        ui.dock.as_ref(),
    );
    let Some(dock) = dock else {
        return;
    };
    let id = view.panel_id(cx);
    let tabs = dock.tabs(cx);
    let area = dock.area.clone();
    if let Some(node) = dock.group_of(id, cx) {
        let max = area.read(cx).zoomed_group();
        area.update(cx, |a, cx| {
            if max.is_some_and(|m| m != node) {
                a.set_zoomed_out(window, cx);
            }
            a.select_panel(id, window, cx);
        });
        if let Some(d) = ui.dock.as_mut() {
            d.group = Some(node);
        }
        return;
    }
    let here = dock.group.filter(|g| tabs.iter().any(|(n, _, _)| n == g));
    let here = here.or(tabs.first().map(|(n, _, _)| *n));
    let preview = |panels: &[PanelId]| {
        let members = store_members.clone().unwrap_or_default();
        panels
            .iter()
            .position(|p| agent_of(ui, *p).is_some_and(|a| !members.iter().any(|m| m == a)))
    };
    let target = match (here, beside) {
        (Some(here), true) => match tabs.iter().find(|(n, _, _)| *n != here) {
            Some((node, panels, _)) => Some((*node, preview(panels), false)),
            None => {
                let (node, placement) = (here, Placement::Right);
                let to = InsertTarget::Split {
                    node,
                    placement,
                    size: None,
                };
                place(&area, view, Some(to), &tabs, window, cx);
                return;
            }
        },
        (Some(here), false) => {
            let panels = tabs
                .iter()
                .find(|(n, _, _)| *n == here)
                .map(|t| t.1.clone());
            Some((here, panels.as_deref().and_then(preview), true))
        }
        (None, _) => None,
    };
    let is_member = store_members.is_some_and(|m| m.contains(&agent));
    let Some((node, old, _)) = target else {
        place(&area, view, None, &tabs, window, cx);
        return;
    };
    // A preview replaces the group's preview, in its place.
    let old = old.filter(|_| !is_member);
    let ix = old.map(|o| {
        tabs.iter()
            .find(|t| t.0 == node)
            .map_or(0, |t| t.1.len().min(o))
    });
    let to = InsertTarget::Tabs {
        node,
        ix,
        activate: true,
    };
    place(&area, view, Some(to), &tabs, window, cx);
    if let Some(o) = old {
        let gone = tabs
            .iter()
            .find(|t| t.0 == node)
            .and_then(|t| t.1.get(o).copied());
        if let Some(agent) = gone.and_then(|g| agent_of(ui, g)).map(String::from) {
            close(ui, &agent, window, cx);
        }
    }
}

/// Put a new tab into the dock at `to` (`None`: the first group). The kit adds it to the first group,
/// shown; that group goes back to showing what it did.
fn place(
    area: &Entity<DockArea>,
    view: Arc<dyn BasePanelView>,
    to: Option<InsertTarget>,
    tabs: &[(NodeId, Vec<PanelId>, usize)],
    window: &mut Window,
    cx: &mut App,
) {
    let id = view.panel_id(cx);
    let first = tabs.first().and_then(|(_, p, ix)| p.get(*ix).copied());
    area.update(cx, |a, cx| {
        a.add_panel_view(view, DockPlacement::Center, None, window, cx);
        if let Some(to) = to {
            a.move_panel(id, to, window, cx);
            if let Some(first) = first {
                a.select_panel(first, window, cx);
            }
            a.select_panel(id, window, cx);
        }
    });
}

/// A member that joined since the last sync (another device added it) opens as a tab in the focused
/// group, behind the one shown.
fn join<H: Host>(ui: &mut Ui, store: &Store, window: &mut Window, cx: &mut Context<H>) {
    let Some(dock) = ui.dock.as_ref() else {
        return;
    };
    let was = dock.members.clone();
    let now = members(store, &dock.space);
    let new: Vec<String> = now.into_iter().filter(|m| !was.contains(m)).collect();
    for agent in new {
        let view = panel(ui, &agent, window, cx);
        let Some(dock) = ui.dock.as_ref() else {
            return;
        };
        let id = view.panel_id(cx);
        if dock.group_of(id, cx).is_some() {
            continue;
        }
        let tabs = dock.tabs(cx);
        let node = dock.group.or(tabs.first().map(|t| t.0));
        let to = node.map(|node| InsertTarget::Tabs {
            node,
            ix: None,
            activate: false,
        });
        let area = dock.area.clone();
        place(&area, view, to, &tabs, window, cx);
    }
}

/// A member removed since the last sync (another device) stays open as a preview, unless its group has
/// one: then its tab closes, so a group keeps at most one preview, the one it had.
fn leave<H: Host>(ui: &mut Ui, store: &Store, window: &mut Window, cx: &mut Context<H>) {
    let Some(dock) = ui.dock.as_ref() else {
        return;
    };
    let now = members(store, &dock.space);
    let gone: Vec<String> = dock
        .members
        .iter()
        .filter(|m| !now.contains(m))
        .cloned()
        .collect();
    for agent in gone {
        let (Some(dock), Some(id)) = (ui.dock.as_ref(), ui.panels.get(&agent).map(|p| p.id)) else {
            continue;
        };
        let tabs = dock.tabs(cx);
        let Some((_, panels, _)) = tabs.iter().find(|t| t.1.contains(&id)) else {
            continue;
        };
        let others = panels.iter().filter(|p| **p != id);
        let preview = others
            .filter_map(|p| agent_of(ui, *p))
            .any(|a| !now.iter().any(|m| m == a));
        if !preview {
            continue;
        }
        let zoomed = ui.zoomed_agent() == Some(agent.as_str());
        close(ui, &agent, window, cx);
        if zoomed {
            window.focus(ui.focus_target(), cx);
        }
    }
}

/// Close `agent`'s tab. Closing the zoom's moves it to the tab now shown in that group, else any.
fn close<H: Host>(ui: &mut Ui, agent: &str, window: &mut Window, cx: &mut Context<H>) {
    let (Some(dock), Some(p)) = (ui.dock.as_ref(), ui.panels.get(agent)) else {
        return;
    };
    let node = dock.group_of(p.id, cx);
    let Some(entity) = p
        .view
        .as_any()
        .downcast_ref::<Entity<AgentPanel<H>>>()
        .cloned()
    else {
        return;
    };
    let area = dock.area.clone();
    area.update(cx, |a, cx| a.remove_panel(entity, window, cx));
    ui.panels.remove(agent);
    if ui.zoomed_agent() != Some(agent) {
        return;
    }
    let Some(dock) = ui.dock.as_ref() else {
        return;
    };
    let tabs = dock.tabs(cx);
    let same = tabs.iter().find(|t| Some(t.0) == node);
    let next = same
        .or(tabs.first())
        .and_then(|(_, p, ix)| p.get(*ix).copied());
    let next = next.and_then(|id| agent_of(ui, id)).map(String::from);
    if let Some(zoom) = ui.zoom.as_mut() {
        zoom.agent = next;
    }
}

/// The zoom moves to the tab `to` picks in the focused group, from where it is and how many there are.
fn pick(ui: &mut Ui, cx: &App, to: impl Fn(usize, usize) -> usize) {
    let (Some(dock), Some(p)) = (ui.dock.as_ref(), ui.panel()) else {
        return;
    };
    let id = p.id;
    let tabs = dock.tabs(cx);
    let Some((_, panels, _)) = tabs.iter().find(|t| t.1.contains(&id)) else {
        return;
    };
    let at = panels.iter().position(|p| *p == id).unwrap_or(0);
    let next = panels
        .get(to(at, panels.len()))
        .and_then(|id| agent_of(ui, *id));
    if let (Some(next), Some(zoom)) = (next.map(String::from), ui.zoom.as_mut()) {
        zoom.agent = Some(next);
    }
}

fn maximize<H: Host>(ui: &mut Ui, window: &mut Window, cx: &mut Context<H>) {
    let (Some(dock), Some(p)) = (ui.dock.as_ref(), ui.panel()) else {
        return;
    };
    let Some(node) = dock.group_of(p.id, cx) else {
        return;
    };
    let area = dock.area.clone();
    area.update(cx, |a, cx| match a.is_zoomed() {
        true => a.set_zoomed_out(window, cx),
        false => a.set_zoomed_in(node, window, cx),
    });
}

/// What the store is told when what is on screen changed: the focused agent and the shown tabs beside it.
fn tell(ui: &mut Ui, space: &str, cx: &App) -> Vec<Event> {
    let agent = ui.zoomed_agent().map(String::from);
    let Some(dock) = ui.dock.as_ref() else {
        return Vec::new();
    };
    let shown = dock.shown(cx).into_iter().filter_map(|id| agent_of(ui, id));
    let beside: Vec<String> = shown
        .filter(|a| Some(*a) != agent.as_deref())
        .map(String::from)
        .collect();
    let now = Some((agent.clone(), beside.clone()));
    let Some(dock) = ui.dock.as_mut() else {
        return Vec::new();
    };
    if dock.told == now {
        return Vec::new();
    }
    dock.told = now;
    let space = space.to_string();
    vec![Event::Lens(Move::View {
        space,
        agent,
        beside,
    })]
}

/// The layout changed (a tab dragged, a split resized, a group closed): a preview dragged to another
/// group is pinned, and a tab dropped anywhere, its own group too, is the zoom's and takes focus.
fn changed<H: Host>(h: &mut H, window: &mut Window, cx: &mut Context<H>) {
    let (store, ui) = h.parts();
    let Some(dock) = ui.dock.as_ref() else {
        return;
    };
    let tabs = dock.tabs(cx);
    let was = |p: &PanelId| dock.groups.iter().find(|g| g.1.contains(p)).map(|g| g.0);
    let moved: Vec<PanelId> = tabs
        .iter()
        .flat_map(|(node, panels, _)| panels.iter().map(move |p| (*p, *node)))
        .filter(|(p, node)| was(p).is_some_and(|was| was != *node))
        .map(|(p, _)| p)
        .collect();
    let reordered = tabs.iter().filter(|(node, panels, _)| {
        let same = |old: &Vec<PanelId>| {
            old.len() == panels.len() && panels.iter().all(|p| old.contains(p))
        };
        dock.groups
            .iter()
            .any(|(n, old)| n == node && old != panels && same(old))
    });
    let reordered: Vec<PanelId> = reordered
        .filter_map(|(_, p, ix)| p.get(*ix).copied())
        .collect();
    let space = dock.space.clone();
    let mut events = Vec::new();
    let members = members(store, &space);
    for id in &moved {
        let Some(agent) = agent_of(ui, *id).map(String::from) else {
            continue;
        };
        if !members.contains(&agent) && !space.is_empty() {
            events.push(pin(&space, &agent));
        }
    }
    let dropped = match (moved.as_slice(), reordered.as_slice()) {
        ([id], _) | ([], [id]) => Some(*id),
        _ => None,
    };
    if let Some(id) = dropped
        && let Some(agent) = agent_of(ui, id).map(String::from)
        && let Some(zoom) = ui.zoom.as_mut()
    {
        zoom.agent = Some(agent);
    }
    events.extend(sync(ui, store, window, cx));
    // Kept for the next zoom in, and the next launch (`layouts.json`); an agent alone has no space.
    let dump = ui
        .dock
        .as_ref()
        .filter(|d| d.space == space && !space.is_empty());
    let mut dump = dump.map(|d| d.area.read(cx).dump(cx).center);
    if let Some(dump) = dump.as_mut() {
        mark(dump, &members);
    }
    if let Some(dock) = dump.and_then(|d| serde_json::to_value(d).ok()) {
        events.push(Event::Layout { space, dock });
    }
    if dropped.is_some() && !ui.focus_target().is_focused(window) {
        window.focus(&ui.focus_target().clone(), cx);
    }
    for e in events {
        h.dispatch(e, cx);
    }
    cx.notify();
}

/// Each tab of an agent not a member marked a preview in `state` (the kit's dump), so a restore keeps
/// it over a member removed since, which shows as one too (`restore`).
fn mark(state: &mut PanelState, members: &[String]) {
    if let PanelInfo::Panel(info) = &mut state.info
        && let Some(agent) = info.get("agent").and_then(|a| a.as_str())
        && !members.iter().any(|m| m == agent)
        && let Some(info) = info.as_object_mut()
    {
        info.insert("preview".into(), true.into());
    }
    for c in &mut state.children {
        mark(c, members);
    }
}

/// The event that makes `agent` a member of `space` (a preview pinned).
pub(super) fn pin(space: &str, agent: &str) -> Event {
    let stamp = crate::views::notes::stamp();
    let (space, agent) = (space.to_string(), agent.to_string());
    Event::Lens(Move::Pin {
        space,
        agent,
        stamp,
    })
}

/// A dock key or click: close a tab (a pinned one leaves the space), pin a preview, maximize.
pub(super) fn close_tab(store: &Store, ui: &mut Ui, agent: Option<&str>) -> Vec<Event> {
    let (Some(zoom), Some(agent)) = (ui.zoom.clone(), agent.or(ui.zoomed_agent())) else {
        return Vec::new();
    };
    let agent = agent.to_string();
    ui.asks.push(Ask::Close(agent.clone()));
    if !members(store, &zoom.space).contains(&agent) {
        return Vec::new();
    }
    let stamp = crate::views::notes::stamp();
    let space = zoom.space;
    vec![Event::Lens(Move::Unpin {
        space,
        agent,
        stamp,
    })]
}

pub(super) fn pin_tab(store: &Store, ui: &Ui, agent: &str) -> Vec<Event> {
    match ui.zoom.as_ref() {
        Some(z) if !z.alone() && !members(store, &z.space).iter().any(|m| m == agent) => {
            vec![pin(&z.space, agent)]
        }
        _ => Vec::new(),
    }
}

/// Whether the zoom's dock has no tabs (every one closed): the space's empty state shows instead.
pub(super) fn empty(ui: &Ui, cx: &App) -> bool {
    ui.dock
        .as_ref()
        .is_none_or(|d| d.tabs(cx).iter().all(|t| t.1.is_empty()))
}

/// The tabs, group by group as laid out, for the harness (`probe`).
pub(super) fn tab_names(ui: &Ui, cx: &App) -> Vec<String> {
    let Some(dock) = ui.dock.as_ref() else {
        return Vec::new();
    };
    let ids = dock.tabs(cx).into_iter().flat_map(|t| t.1);
    ids.filter_map(|id| agent_of(ui, id).map(String::from))
        .collect()
}

/// The dock as the harness reads it: groups split by ` | `, each tab's name, `*` on the one a group
/// shows, `~` on a preview, the zoom's in brackets; `max ` first while a group is maximized.
pub(super) fn describe(store: &Store, ui: &Ui, cx: &App) -> String {
    let Some(dock) = ui.dock.as_ref() else {
        return String::new();
    };
    let members = members(store, &dock.space);
    let group = |(_, panels, ix): (NodeId, Vec<PanelId>, usize)| {
        let tab = |(i, id): (usize, &PanelId)| {
            let agent = agent_of(ui, *id).unwrap_or("?");
            let mark = if i == ix { "*" } else { "" };
            let preview = if members.iter().any(|m| m == agent) {
                ""
            } else {
                "~"
            };
            match ui.zoomed_agent() == Some(agent) {
                true => format!("[{agent}{mark}{preview}]"),
                false => format!("{agent}{mark}{preview}"),
            }
        };
        panels
            .iter()
            .enumerate()
            .map(tab)
            .collect::<Vec<_>>()
            .join(" ")
    };
    let groups: Vec<String> = dock.tabs(cx).into_iter().map(group).collect();
    let max = if dock.area.read(cx).is_zoomed() {
        "max "
    } else {
        ""
    };
    format!("{max}{}", groups.join(" | "))
}
