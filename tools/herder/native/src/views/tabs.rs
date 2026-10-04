//! The dock's tab strip (DK2), drawn to web's measurements: the kit's own (`TabGroupSkin`) is private,
//! always draws a menu and has no top line. The rest of the dock's look is the kit's (`Skin`), and what
//! the tabs do is the dock's (`dock`). It behaves as Zed's (S2): tabs keep their width and the strip
//! scrolls, the shown one into view; those out of view are under +N; a tab's × is there on hover; the
//! maximized group's □ is selected.

use crate::store::Store;
use crate::views::dock::{Close, Pin, agent_of, members};
use crate::views::space::{Anim, Tab};
use crate::views::theme::{MONO_T, TypeScale, pal, type_scale};
use crate::views::{Host, pill};
use gpui_kit::base::ResizeHandleContext;
use gpui_kit::component::Sizable as _;
use gpui_kit::component::button::{Button, ButtonVariants as _};
use gpui_kit::component::dock::*;
use gpui_kit::component::menu::{DropdownMenu as _, PopupMenu, PopupMenuItem};
use gpui_kit::prelude::FluentBuilder as _;
use gpui_kit::*;
use std::cell::Cell;
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
            scroll: ScrollHandle::new(),
            shown: Cell::new(None),
            hovered: Rc::default(),
            reveal: Rc::default(),
        })
    }
}

/// A group's tab strip, web's (`.dv-tabs-and-actions-container`, measured): 32 tall on the panel colour
/// under a 1px rule; each tab mono 12, divided by a rule, the shown one on the ground under a 2px blue
/// line, the focused group's in ink and the rest dim; a preview italic after a hollow dot; the status
/// dot, the tool, needs-you, and × on hover; +N for those out of view and the group's □ (maximize) at
/// the right, where the tabs do not scroll them away. The dock keeps one for each group.
struct Strip<H> {
    host: WeakEntity<H>,
    /// The tabs' sideways scroll.
    scroll: ScrollHandle,
    /// The shown tab at the last render: a new one is scrolled into view.
    shown: Cell<Option<PanelId>>,
    /// The tab under the pointer, the one with a ×.
    hovered: Rc<Cell<Option<PanelId>>>,
    /// A tab to scroll into view (the shown one when it changes, or one picked under +N) and the tries
    /// left: it waits for a laid out strip and holds until the tab is wholly in view, as the first
    /// layouts move the view (+N coming or going).
    reveal: Rc<Cell<Option<(PanelId, u8)>>>,
}

/// Layouts a reveal may take before it gives up (a tab wider than the strip is never wholly in view).
const TRIES: u8 = 4;

impl<H: Host> TabGroupRenderer for Strip<H> {
    fn frame(&self, _: &TabGroupContext, _: &mut Window, _: &mut App) -> Stateful<Div> {
        div().id("tab-group").bg(rgb(pal::GROUND))
    }

    fn render_tab_bar(
        &self,
        group: &TabGroupContext,
        window: &mut Window,
        cx: &mut App,
    ) -> AnyElement {
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
        // Each drawn tab: its place in the group, its panel and its agent.
        let drawn: Vec<(usize, PanelId, String)> = ids
            .iter()
            .enumerate()
            .filter_map(|(ix, id)| Some((ix, *id, agent_of(ui, *id)?.to_string())))
            .collect();
        if self.shown.replace(shown) != shown
            && let Some(id) = shown
        {
            self.reveal.set(Some((id, TRIES)));
        }
        let out = hidden(&self.scroll, drawn.len());
        if let Some((id, tries)) = self.reveal.get()
            && laid(&self.scroll)
        {
            let at = drawn.iter().position(|d| d.1 == id);
            let unseen =
                |at: &usize| out.contains(at) || self.scroll.bounds_for_item(*at).is_none();
            match at.filter(|at| tries > 0 && unseen(at)) {
                Some(at) => {
                    self.scroll.scroll_to_item(at);
                    self.reveal.set(Some((id, tries - 1)));
                }
                None => self.reveal.set(None),
            }
        }
        let view = window.current_view();
        let tabs: Vec<AnyElement> = drawn
            .iter()
            .map(|(ix, id, agent)| {
                let state = TabState {
                    shown: Some(*id) == shown,
                    lit: here && Some(*id) == shown,
                    preview: !members.contains(agent),
                };
                let hover = Hover {
                    id: *id,
                    on: self.hovered.clone(),
                    view,
                };
                tab(store, agent, *ix, state, hover, group, t, cx)
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
        let scroller = div()
            .id("tab-scroll")
            .flex_1()
            .min_w_0()
            .h_full()
            .flex()
            .overflow_x_scroll()
            .track_scroll(&self.scroll)
            .children(tabs)
            .child(rest);
        // Laid out, the tabs out of view may not be those +N was drawn for (a resize, a scroll into
        // view), or a reveal is still to land: draw again.
        let check = {
            let (scroll, was, n) = (self.scroll.clone(), out.clone(), drawn.len());
            let reveal = self.reveal.clone();
            let prepaint = move |_, _: &mut Window, cx: &mut App| {
                let pending = reveal.get().is_some() && laid(&scroll);
                if pending || hidden(&scroll, n) != was {
                    cx.defer(move |cx| cx.notify(view));
                }
            };
            canvas(prepaint, |_, _, _, _| {}).absolute().size_0()
        };
        let names: Vec<(SharedString, PanelId)> = out
            .iter()
            .filter_map(|&at| drawn.get(at))
            .map(|d| (SharedString::from(d.2.clone()), d.1))
            .collect();
        let reveal = self.reveal.clone();
        let more = (!names.is_empty()).then(|| {
            let label = format!("+{}", names.len());
            let menu = move |mut menu: PopupMenu, _: &mut Window, _: &mut Context<PopupMenu>| {
                for (name, id) in &names {
                    // Shown already (scrolled away, or narrowed out), the pick still reveals it.
                    let (pick, id, reveal) = (Tab(name.clone()), *id, reveal.clone());
                    let picked = move |_: &ClickEvent, window: &mut Window, cx: &mut App| {
                        reveal.set(Some((id, TRIES)));
                        cx.notify(view);
                        window.dispatch_action(pick.boxed_clone(), cx);
                    };
                    menu = menu.item(PopupMenuItem::new(name.clone()).on_click(picked));
                }
                menu.scrollable(true)
            };
            let button = Button::new("tab-more").xsmall().ghost().label(label);
            let button = button
                .text_color(rgb(pal::SLATE))
                .dropdown_menu(menu)
                .anchor(Anchor::TopRight);
            let el = div()
                .flex_none()
                .h_full()
                .px(t.css(4.))
                .flex()
                .items_center();
            el.border_l_1().border_color(rgb(pal::RULE)).child(button)
        });
        let zoomed = group.is_zoomed();
        let max = div()
            .id("tab-max")
            .flex_none()
            .h_full()
            .px(t.css(9.))
            .flex()
            .items_center()
            .cursor_pointer()
            .when(zoomed, |el| el.bg(rgb(pal::SELECT)))
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
                    .border_color(rgb(if zoomed { pal::INK } else { pal::SLATE })),
            );
        div()
            .id("tab-strip")
            .flex_none()
            .overflow_hidden()
            .relative()
            .flex()
            .h(t.css(32.))
            .bg(rgb(pal::PANEL))
            .border_b_1()
            .border_color(rgb(pal::RULE))
            .font_family(MONO_T)
            .text_size(t.css(12.))
            .child(scroller.test_support())
            .child(check)
            .children(more)
            .child(max.test_support())
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

/// The strip has been laid out: it has a view to scroll in.
fn laid(scroll: &ScrollHandle) -> bool {
    scroll.bounds().size.width > px(0.)
}

/// The tabs out of view, by place in the strip: not wholly inside it where the last layout put them.
fn hidden(scroll: &ScrollHandle, n: usize) -> Vec<usize> {
    let (view, dx) = (scroll.bounds(), scroll.offset().x);
    let items = (0..n).map(|ix| scroll.bounds_for_item(ix).map(|b| (b.left(), b.right())));
    out_of_view((view.left(), view.right()), dx, items)
}

/// The items, each `(left, right)` unscrolled or unknown, not wholly inside `view` scrolled by `dx`; an
/// unlaid view has none out.
pub(super) fn out_of_view(
    (left, right): (Pixels, Pixels),
    dx: Pixels,
    items: impl Iterator<Item = Option<(Pixels, Pixels)>>,
) -> Vec<usize> {
    if right <= left {
        return Vec::new();
    }
    let slack = px(0.5);
    items
        .enumerate()
        .filter_map(|(ix, b)| {
            let (l, r) = b?;
            (l + dx < left - slack || r + dx > right + slack).then_some(ix)
        })
        .collect()
}

/// Whether the pointer is on a tab, kept by its strip, the view to draw again when it moves.
struct Hover {
    id: PanelId,
    on: Rc<Cell<Option<PanelId>>>,
    view: EntityId,
}

#[derive(Clone, Copy)]
struct TabState {
    shown: bool,
    /// Shown in the focused group.
    lit: bool,
    preview: bool,
}

/// One tab (web's `.herder-dock-tab`), its own width up to 220: a click shows it, a double-click pins
/// it, a middle-click or its × (there under the pointer) closes it; it drags to another place in the
/// dock.
#[allow(clippy::too_many_arguments)]
fn tab(
    store: &Store,
    agent: &str,
    ix: usize,
    s: TabState,
    hover: Hover,
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
    // Away from the pointer its place stays, so the tab keeps its width.
    let slot = div().flex_none().h_full().w(t.css(25.)).ml_auto();
    let close = if hover.on.get() != Some(hover.id) {
        slot.into_any_element()
    } else {
        let name = name.clone();
        slot.id(SharedString::from(format!("tab-close-{agent}")))
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
            .test_support()
            .into_any_element()
    };
    let (pick, pinned, gone) = (
        Tab(name.clone()),
        Pin(name.clone()),
        Close(Some(name.clone())),
    );
    let drag = group.drag_panel(ix, cx).filter(|_| group.is_draggable());
    let g = group.clone();
    let Hover { id, on, view } = hover;
    div()
        .id(SharedString::from(format!("tab-{agent}")))
        .relative()
        .flex_none()
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
        .on_hover(move |&over, _, cx| {
            if over {
                on.set(Some(id));
            } else if on.get() == Some(id) {
                on.set(None);
            }
            cx.notify(view);
        })
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
        .test_support()
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
