//! The dock in the zoom (DK2): the zoomed space's agent panels (`panel`) as tabs in groups, split side
//! by side or stacked, on the kit's dock (`DockArea`: groups, splits, drag and drop, resize, maximize).
//! A space's members are its pinned tabs; a tab of an agent that is not a member is a preview (italic,
//! a hollow dot), at most one in a group, replaced by the next one opened there and pinned (made a
//! member) by a double-click, a send, or a drag to another group. Closing a pinned tab removes the
//! member, as web's does.
//!
//! The zoom's agent is the focused panel's: focus moving into a panel moves it (`focused`), and an
//! action that names another agent opens or shows its tab and focuses it (`sync`). The store is told
//! what is on screen, the focused agent and each other group's shown tab (`Move::View`), whenever that
//! changes. The tab strip is drawn here to web's measurements (`Strip`); the rest of the dock's look is
//! the kit's.

use crate::store::spaces::{Move, Space};
use crate::store::{Event, Store};
use crate::views::lens::Ui;
use crate::views::panel::{AgentPanel, Panel};
use crate::views::space::{Anim, Tab, Zoom};
use crate::views::theme::{MONO_T, TypeScale, pal, type_scale};
use crate::views::{Host, pill};
use gpui_kit::base::ResizeHandleContext;
use gpui_kit::component::Placement;
use gpui_kit::component::dock::*;
use gpui_kit::prelude::FluentBuilder as _;
use gpui_kit::*;
use std::collections::HashMap;
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
    /// Each tab's group as last synced: a tab found in another was dragged there.
    groups: HashMap<PanelId, NodeId>,
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

fn agent_of(ui: &Ui, id: PanelId) -> Option<&str> {
    ui.panels
        .iter()
        .find(|(_, p)| p.id == id)
        .map(|(a, _)| a.as_str())
}

fn members(store: &Store, space: &str) -> Vec<String> {
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
        show(ui, beside, window, cx);
    }
    let Some(dock) = ui.dock.as_mut() else {
        return Vec::new();
    };
    let shown = dock.shown(cx);
    // A group maximized over the zoom's agent's: the zoom moves to what shows.
    let on = |a: &str| ui.panels.get(a).is_some_and(|p| shown.contains(&p.id));
    if live && !ui.zoomed_agent().is_none_or(on) {
        let first = shown
            .first()
            .and_then(|id| agent_of(ui, *id))
            .map(String::from);
        if let (Some(first), Some(zoom)) = (first, ui.zoom.as_mut()) {
            zoom.agent = Some(first);
        }
    }
    let Some(dock) = ui.dock.as_mut() else {
        return Vec::new();
    };
    let tabs = dock.tabs(cx);
    dock.groups = tabs
        .iter()
        .flat_map(|(node, panels, _)| panels.iter().map(|p| (*p, *node)))
        .collect();
    dock.members = members(store, &zoom.space);
    let held: Vec<PanelId> = dock.groups.keys().copied().collect();
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
        let skin = Rc::new(Skin {
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
    let preview = zoom.agent.clone().filter(|a| !agents.contains(a));
    agents.extend(preview);
    let at = agents.iter().position(|a| Some(a) == zoom.agent.as_ref());
    let mut tabs = DockLayout::tabs();
    for agent in &agents {
        tabs = tabs.panel_view(panel(ui, agent, window, cx), cx);
    }
    let layout = DockLayout::h_split().child(tabs.active_index(at.unwrap_or(0)), None);
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
        groups: HashMap::new(),
        group: None,
        held: None,
        told: None,
        _changed: changed,
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
/// group is pinned, and a tab dragged anywhere is the zoom's and takes focus.
fn changed<H: Host>(h: &mut H, window: &mut Window, cx: &mut Context<H>) {
    let (store, ui) = h.parts();
    let Some(dock) = ui.dock.as_ref() else {
        return;
    };
    let moved: Vec<PanelId> = dock
        .tabs(cx)
        .iter()
        .flat_map(|(node, panels, _)| panels.iter().map(move |p| (*p, *node)))
        .filter(|(p, node)| dock.groups.get(p).is_some_and(|was| was != node))
        .map(|(p, _)| p)
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
    if let [id] = moved.as_slice()
        && let Some(agent) = agent_of(ui, *id).map(String::from)
        && let Some(zoom) = ui.zoom.as_mut()
    {
        zoom.agent = Some(agent);
    }
    events.extend(sync(ui, store, window, cx));
    if !moved.is_empty() && !ui.focus_target().is_focused(window) {
        window.focus(&ui.focus_target().clone(), cx);
    }
    for e in events {
        h.dispatch(e, cx);
    }
    cx.notify();
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

/// The dock's look: the kit's, but for the tab strip (`Strip`).
struct Skin<H> {
    kit: Rc<DockSkin>,
    host: WeakEntity<H>,
}

impl<H: Host> DockAreaRenderer for Skin<H> {
    fn render_split_handle(
        &self,
        handle: &ResizeHandleContext,
        window: &mut Window,
        cx: &mut App,
    ) -> Option<AnyElement> {
        self.kit.render_split_handle(handle, window, cx)
    }

    fn split_frame(
        &self,
        node: NodeId,
        axis: Axis,
        window: &mut Window,
        cx: &mut App,
    ) -> Stateful<Div> {
        self.kit
            .split_frame(node, axis, window, cx)
            .bg(rgb(pal::RULE))
    }

    fn build_placeholder(
        &self,
        state: &PanelState,
        window: &mut Window,
        cx: &mut App,
    ) -> Option<Arc<dyn BasePanelView>> {
        self.kit.build_placeholder(state, window, cx)
    }

    fn tab_group_renderer(&self) -> Rc<dyn TabGroupRenderer> {
        Rc::new(Strip {
            host: self.host.clone(),
        })
    }
}

/// A group's tab strip, web's (`.dv-tabs-and-actions-container`, measured): 32 tall on the panel colour
/// under a 1px rule; each tab mono 12, divided by a rule, the shown one on the ground under a 2px blue
/// line, the focused group's in ink and the rest dim; a preview italic after a hollow dot; the status
/// dot, the tool, needs-you, and ×; the group's □ (maximize) at the right.
struct Strip<H> {
    host: WeakEntity<H>,
}

impl<H: Host> TabGroupRenderer for Strip<H> {
    fn frame(&self, _: &TabGroupContext, _: &mut Window, _: &mut App) -> Stateful<Div> {
        div().id("tab-group").bg(rgb(pal::GROUND))
    }

    fn render_tab_bar(&self, group: &TabGroupContext, _: &mut Window, cx: &mut App) -> AnyElement {
        let Some(host) = self.host.upgrade() else {
            return div().into_any_element();
        };
        let (store, ui) = host.read(cx).view();
        let t = type_scale(store.prefs.text_scale);
        let space = ui
            .zoom
            .as_ref()
            .or(ui.anim.as_ref().and_then(Anim::leaving));
        let members = space.map(|z| members(store, &z.space)).unwrap_or_default();
        let focused = ui.zoomed_agent();
        let shown = group.active_panel().map(|p| p.panel_id(cx));
        let ids: Vec<PanelId> = group.panels().iter().map(|p| p.panel_id(cx)).collect();
        let here = ids
            .iter()
            .any(|id| agent_of(ui, *id) == focused && focused.is_some());
        let tabs: Vec<AnyElement> = ids
            .iter()
            .enumerate()
            .filter_map(|(ix, id)| {
                let agent = agent_of(ui, *id)?.to_string();
                let state = TabState {
                    shown: Some(*id) == shown,
                    lit: here && Some(*id) == shown,
                    preview: !members.contains(&agent),
                };
                Some(tab(store, &agent, ix, state, group, t, cx))
            })
            .collect();
        let (droppable, node, count) = (group.is_droppable(), group.node(), ids.len());
        let rest = div()
            .id("tab-rest")
            .h_full()
            .flex_1()
            .min_w(t.css(32.))
            .when(droppable, |el| {
                let g = group.clone();
                el.drag_over::<DragPanel>(|el, _, _, _| el.bg(rgb(pal::SELECT)))
                    .on_drop(move |d: &DragPanel, window, cx| {
                        let ix = (d.source() == node).then(|| count.saturating_sub(1));
                        g.drop_panel(d.clone(), ix, false, window, cx);
                    })
            });
        let max = div()
            .id("tab-max")
            .flex_none()
            .h_full()
            .px(t.css(9.))
            .flex()
            .items_center()
            .cursor_pointer()
            .hover(|s| s.bg(rgb(pal::WASH)))
            .on_click({
                let g = group.clone();
                move |_, window, cx| g.toggle_zoom(window, cx)
            })
            .child(
                div()
                    .size(t.css(11.))
                    .border_1()
                    .rounded(t.css(1.5))
                    .border_color(rgb(pal::SLATE)),
            );
        div()
            .id("tab-strip")
            .flex_none()
            .overflow_hidden()
            .flex()
            .h(t.css(32.))
            .bg(rgb(pal::PANEL))
            .border_b_1()
            .border_color(rgb(pal::RULE))
            .font_family(MONO_T)
            .text_size(t.css(12.))
            .children(tabs)
            .child(rest)
            .child(max)
            .into_any_element()
    }

    fn render_active_panel(
        &self,
        panel: AnyView,
        _: &TabGroupContext,
        _: &mut Window,
        _: &mut App,
    ) -> AnyElement {
        let style = StyleRefinement::default().size_full();
        div()
            .flex_1()
            .min_h_0()
            .flex()
            .child(panel.cached(style))
            .into_any_element()
    }

    fn render_drop_indicator(
        &self,
        indicator: DropIndicator,
        _: &mut Window,
        _: &mut App,
    ) -> Option<AnyElement> {
        let to = indicator.to();
        let el = div().absolute().left(to.origin().x).top(to.origin().y);
        let el = el
            .w(to.size().width)
            .h(to.size().height)
            .bg(rgba(0x31406B99));
        Some(
            el.border_2()
                .border_color(rgb(pal::BLUE))
                .into_any_element(),
        )
    }
}

#[derive(Clone, Copy)]
struct TabState {
    shown: bool,
    /// Shown in the focused group.
    lit: bool,
    preview: bool,
}

/// One tab (web's `.herder-dock-tab`): a click shows it, a double-click pins it, a middle-click or its
/// × closes it; it drags to another place in the dock.
fn tab(
    store: &Store,
    agent: &str,
    ix: usize,
    s: TabState,
    group: &TabGroupContext,
    t: TypeScale,
    cx: &App,
) -> AnyElement {
    let name = SharedString::from(agent.to_string());
    let a = store.fleet.agents.get(agent);
    let bus = a.map_or("", |a| a.bus_status.as_str());
    let tool: &str = match a.map(|a| a.tool.as_str()).unwrap_or("") {
        "claude" => "✱",
        "codex" => "⬡",
        "" | "-" => "",
        other => &other[..other.len().min(2)],
    };
    let dot = || div().flex_none().size(t.css(7.)).rounded_full();
    let ring = s
        .preview
        .then(|| dot().border_1().border_color(rgb(pal::BLUE)));
    let title = div()
        .overflow_hidden()
        .text_ellipsis()
        .whitespace_nowrap()
        .child(name.clone());
    let title = title.when(s.preview, |el| el.italic().text_color(rgb(pal::SLATE)));
    let label = div()
        .flex()
        .min_w_0()
        .items_center()
        .gap(t.css(5.))
        .children(ring)
        .child(title);
    let meta = div()
        .flex()
        .flex_none()
        .items_center()
        .gap(t.css(4.))
        .text_color(rgb(pal::SLATE));
    let meta = meta.child(dot().bg(rgb(dot_color(bus))));
    let meta = meta.when(!tool.is_empty(), |el| {
        el.child(div().text_size(t.css(10.)).child(tool.to_string()))
    });
    let meta = meta.when(store.agent_needs_you(agent), |el| el.child(pill(1, t)));
    let close = {
        let name = name.clone();
        div()
            .id(SharedString::from(format!("tab-close-{agent}")))
            .flex_none()
            .h_full()
            .w(t.css(25.))
            .ml_auto()
            .flex()
            .items_center()
            .justify_center()
            .pb(px(1.))
            .text_size(t.css(15.))
            .text_color(rgb(pal::SLATE))
            .hover(|s| s.bg(rgb(pal::WASH)).text_color(rgb(pal::INK)))
            .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .on_click(move |_, window, cx| {
                cx.stop_propagation();
                window.dispatch_action(Box::new(Close(Some(name.clone()))), cx);
            })
            .child("×")
    };
    let (pick, pinned, gone) = (
        Tab(name.clone()),
        Pin(name.clone()),
        Close(Some(name.clone())),
    );
    let drag = group.drag_panel(ix, cx).filter(|_| group.is_draggable());
    let g = group.clone();
    div()
        .id(SharedString::from(format!("tab-{agent}")))
        .relative()
        .min_w(t.css(48.))
        .max_w(t.css(220.))
        .h_full()
        .flex()
        .items_center()
        .gap(t.css(8.))
        .pl(t.css(11.))
        .border_r_1()
        .border_color(rgb(pal::RULE))
        .bg(rgb(if s.shown { pal::GROUND } else { pal::PANEL }))
        .text_color(rgb(if s.lit { pal::INK } else { pal::SLATE }))
        .cursor_pointer()
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .on_click(move |e, window, cx| match e.click_count() {
            2 => window.dispatch_action(pinned.boxed_clone(), cx),
            _ => window.dispatch_action(pick.boxed_clone(), cx),
        })
        .on_aux_click(move |e, window, cx| {
            if e.is_middle_click() {
                cx.stop_propagation();
                window.dispatch_action(gone.boxed_clone(), cx);
            }
        })
        .when_some(drag, |el, drag| {
            let name = name.clone();
            el.on_drag(drag, move |d, offset, _, cx| {
                d.set_drag_offset(offset);
                let name = name.clone();
                cx.new(|_| Dragged(name))
            })
        })
        .when(group.is_droppable(), |el| {
            el.drag_over::<DragPanel>(|el, _, _, _| el.border_l_2().border_color(rgb(pal::BLUE)))
                .on_drop(move |d: &DragPanel, window, cx| {
                    g.drop_panel(d.clone(), Some(ix), true, window, cx)
                })
        })
        .when(s.shown, |el| {
            el.child(
                div()
                    .absolute()
                    .top_0()
                    .left_0()
                    .right_0()
                    .h(px(2.))
                    .bg(rgb(pal::BLUE)),
            )
        })
        .child(label)
        .child(meta)
        .child(close)
        .into_any_element()
}

fn dot_color(bus: &str) -> u32 {
    match bus {
        "active" => pal::BLUE,
        "listening" => pal::OPERATOR,
        "blocked" => pal::RED,
        "retired" => pal::QUEUE_TITLE,
        _ => 0x565A66,
    }
}

/// A tab being dragged.
struct Dragged(SharedString);

impl Render for Dragged {
    fn render(&mut self, _: &mut Window, _: &mut Context<Self>) -> impl IntoElement {
        div()
            .px(px(11.))
            .py(px(6.))
            .bg(rgb(pal::GROUND))
            .border_1()
            .border_color(rgb(pal::EDGE))
            .text_color(rgb(pal::INK))
            .font_family(MONO_T)
            .text_size(px(12.))
            .opacity(0.85)
            .child(self.0.clone())
    }
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
