//! The dock's tab strip (DK2), drawn to web's measurements: the kit's own (`TabGroupSkin`) is private,
//! always draws a menu and has no top line. The rest of the dock's look is the kit's (`Skin`), and what
//! the tabs do is the dock's (`dock`).

use crate::store::Store;
use crate::views::dock::{Close, Pin, agent_of, members};
use crate::views::space::{Anim, Tab};
use crate::views::theme::{MONO_T, TypeScale, pal, type_scale};
use crate::views::{Host, pill};
use gpui_kit::base::ResizeHandleContext;
use gpui_kit::component::dock::*;
use gpui_kit::prelude::FluentBuilder as _;
use gpui_kit::*;
use std::rc::Rc;
use std::sync::Arc;

/// The dock's look: the kit's, but for the tab strip (`Strip`).
pub(super) struct Skin<H> {
    pub(super) kit: Rc<DockSkin>,
    pub(super) host: WeakEntity<H>,
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
                        g.drop_panel(d.clone(), ix, true, window, cx);
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
            .pr(t.css(7.))
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
