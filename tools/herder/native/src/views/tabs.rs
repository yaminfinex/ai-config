//! The dock's tab strip (DK2), drawn to web's measurements: the kit's own (`TabGroupSkin`) is private,
//! always draws a menu and has no top line. The rest of the dock's look is the kit's (`Skin`), and what
//! the tabs do is the dock's (`dock`). It behaves as Zed's (S2): tabs keep their width and the strip
//! scrolls, the shown one into view; those out of view are under +N; a tab's × is there on hover. Its
//! look is the owner's calm one (S3, `s3-calm-spec.md`): no boxes or dividers, the shown tab medium
//! over an underline, a dot only for news, faded edges where tabs are cut, and a ⤢ that turns into a
//! selected ⤡ while the group is maximized.

use crate::store::Store;
use crate::views::dock::{Close, Pin, agent_of, members};
use crate::views::space::{Anim, Tab};
use crate::views::theme::{SANS_T, TypeScale, pal, type_scale};
use crate::views::{Host, pill};
use gpui_kit::base::ResizeHandleContext;
use gpui_kit::component::Icon;
use gpui_kit::component::button::{Button, ButtonCustomVariant, ButtonVariants as _};
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

/// A group's tab strip (S3 A1): 32 tall on the group's ground under a 1px rule, sans 13; each tab its
/// status dot, its name, needs-you and × on hover; fades where tabs are cut; +N for those out of view
/// and the group's ⤢ (maximize) at the right, where the tabs do not scroll them away. The dock keeps
/// one for each group.
struct Strip<H> {
    host: WeakEntity<H>,
    /// The tabs' sideways scroll.
    scroll: ScrollHandle,
    /// The shown tab at the last render: a new one is scrolled into view.
    shown: Cell<Option<PanelId>>,
    /// The tab under the pointer, the one with a ×.
    hovered: Rc<Cell<Option<PanelId>>>,
    /// A tab to scroll into view (the shown one when it changes, or one picked under +N) and the tries
    /// left: each render on a laid out strip scrolls to it, and it is done only once a layout has the
    /// tab, as it is now, wholly in view and the same tabs out of view as +N was drawn for (a new tab's
    /// place, +N coming or going, move the view).
    reveal: Rc<Cell<Option<(PanelId, u8)>>>,
}

/// Layouts a reveal may take before it gives up (a tab wider than the strip is never wholly in view).
const TRIES: u8 = 6;

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
        let hovered = self.hovered.get();
        let mut target = None;
        if let Some((id, tries)) = self.reveal.get()
            && laid(&self.scroll)
        {
            target = drawn.iter().position(|d| d.1 == id).filter(|_| tries > 0);
            match target {
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
                    here,
                    hovered: hovered == Some(*id),
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
        // Over the scroller, where it cuts tabs: the ground fading in (S3 A6). No handlers, so the
        // pointer goes through.
        let fade = |angle: f32| {
            let ground = rgb(pal::GROUND);
            let clear = linear_color_stop(ground, 0.).opacity(0.);
            let bg = linear_gradient(angle, clear, linear_color_stop(ground, 0.85));
            div().absolute().top_0().h_full().w(t.css(40.)).bg(bg)
        };
        let scroller = div()
            .relative()
            .flex_1()
            .min_w_0()
            .h_full()
            .child(scroller.test_support())
            .when(out.left, |el| {
                el.child(fade(270.).left_0().id("tab-fade-left").test_support())
            })
            .when(out.right, |el| {
                el.child(fade(90.).right_0().id("tab-fade-right").test_support())
            });
        // Laid out (and scrolled), a reveal is done when its tab is in view and +N was drawn for this
        // layout. Otherwise, or when the tabs out of view are not those +N was drawn for (a resize, a
        // scroll into view), draw again.
        let check = {
            let (scroll, was, n) = (self.scroll.clone(), out.clone(), drawn.len());
            let reveal = self.reveal.clone();
            let prepaint = move |_, _: &mut Window, cx: &mut App| {
                let now = hidden(&scroll, n);
                let seen =
                    |at: usize| scroll.bounds_for_item(at).is_some() && !now.tabs.contains(&at);
                if laid(&scroll) && now == was && target.is_some_and(seen) {
                    reveal.set(None);
                }
                let pending = reveal.get().is_some() && laid(&scroll);
                if pending || now != was {
                    cx.defer(move |cx| cx.notify(view));
                }
            };
            canvas(prepaint, |_, _, _, _| {}).absolute().size_0()
        };
        let names: Vec<(SharedString, PanelId)> = out
            .tabs
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
            // The kit's button and menu (S3 A7), quiet: slate on nothing, the wash under the pointer and
            // while open, its label and chevron in ink under the pointer.
            let tint = ButtonCustomVariant::new(cx)
                .foreground(rgb(pal::SLATE).into())
                .hover(rgb(pal::WASH).into())
                .active(rgb(pal::WASH).into());
            let face = div()
                .flex()
                .items_center()
                .gap(t.css(3.))
                .font_family(SANS_T)
                .text_size(t.css(11.))
                .text_color(rgb(pal::SLATE))
                .group_hover("tab-more", |s| s.text_color(rgb(pal::INK)))
                .child(label)
                .child(icon(CHEVRON).size(t.css(11.)));
            Button::new("tab-more")
                .custom(tint)
                .group("tab-more")
                .h(t.css(22.))
                .px(t.css(6.))
                .rounded(t.css(4.))
                .child(face)
                .dropdown_menu(menu)
                .anchor(Anchor::TopRight)
        });
        // ⤢ in every group, dimmer away from the focus; maximized, the strip's one lit control: ⤡ on
        // the selection under a blue edge (S3 A7).
        let zoomed = group.is_zoomed();
        let max = div()
            .id("tab-max")
            .flex_none()
            .size(t.css(22.))
            .rounded(t.css(4.))
            .flex()
            .items_center()
            .justify_center()
            .cursor_pointer()
            .aria_label(if zoomed { "Restore" } else { "Maximize" })
            .aria_selected(zoomed)
            .map(|el| match zoomed {
                true => el
                    .bg(rgb(pal::SELECT))
                    .border_1()
                    .border_color(rgb(pal::BLUE))
                    .text_color(rgb(pal::INK))
                    .hover(|s| s.bg(rgb(pal::SELECT_HOVER))),
                false => el
                    .text_color(rgb(if here { pal::SLATE } else { pal::DIMMER }))
                    .hover(|s| s.bg(rgb(pal::WASH)).text_color(rgb(pal::INK))),
            })
            .on_click({
                let g = group.clone();
                move |_, window, cx| g.toggle_zoom(window, cx)
            })
            .child(icon(if zoomed { RESTORE } else { MAXIMIZE }).size(t.css(12.)));
        let end = div()
            .flex_none()
            .h_full()
            .flex()
            .items_center()
            .gap(t.css(2.))
            .pl(t.css(4.))
            .pr(t.css(8.))
            .children(more)
            .child(max.test_support());
        div()
            .id("tab-strip")
            .flex_none()
            .overflow_hidden()
            .relative()
            .flex()
            .h(t.css(32.))
            .bg(rgb(pal::GROUND))
            .border_b_1()
            .border_color(rgb(pal::RULE))
            .font_family(SANS_T)
            .text_size(t.css(13.))
            .child(scroller)
            .child(check)
            .child(end)
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

/// The tabs out of view where the last layout put them, and on which sides they are cut.
#[derive(Clone, Default, PartialEq)]
struct Out {
    /// By place in the strip: not wholly inside it.
    tabs: Vec<usize>,
    /// Some begins before the view's left edge.
    left: bool,
    /// Some ends past its right edge.
    right: bool,
}

fn hidden(scroll: &ScrollHandle, n: usize) -> Out {
    let (view, dx) = (scroll.bounds(), scroll.offset().x);
    let bounds = |ix| scroll.bounds_for_item(ix).map(|b| (b.left(), b.right()));
    let tabs = out_of_view((view.left(), view.right()), dx, (0..n).map(bounds));
    let before = |ix: &usize| bounds(*ix).is_some_and(|(l, _)| l + dx < view.left());
    Out {
        left: tabs.iter().any(before),
        right: tabs.iter().any(|ix| !before(ix)),
        tabs,
    }
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
    /// In the focused group.
    here: bool,
    hovered: bool,
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
    let bus = store
        .fleet
        .agents
        .get(agent)
        .map_or("", |a| a.bus_status.as_str());
    // Its slot always there, so news coming and going does not move the tab (S3 A2).
    let dot = div().id(SharedString::from(format!("tab-dot-{agent}")));
    let dot = (dot.flex_none().size(t.css(6.)).rounded_full())
        .when_some(news(bus), |el, c| el.bg(rgb(c)).aria_label(bus.to_string()));
    // The shown tab medium in both groups, so only becoming shown changes a tab's width (S3 A3).
    let ink = match (s.shown, s.here, s.hovered) {
        (true, true, _) => pal::INK,
        (true, false, _) | (false, _, true) => pal::CODE_INK,
        (false, _, false) => pal::SLATE,
    };
    let title = div()
        .min_w_0()
        .overflow_hidden()
        .text_ellipsis()
        .whitespace_nowrap()
        .text_color(rgb(ink))
        .when(s.shown, |el| el.font_weight(FontWeight::MEDIUM))
        .when(s.preview, |el| el.italic())
        .child(name.clone());
    let needs = store.agent_needs_you(agent).then(|| pill(1, t));
    // Away from the pointer its place stays, so the tab keeps its width.
    let slot = div().flex_none().size(t.css(14.));
    let close = if !s.hovered {
        slot.into_any_element()
    } else {
        let name = name.clone();
        slot.id(SharedString::from(format!("tab-close-{agent}")))
            .flex()
            .items_center()
            .justify_center()
            .rounded(t.css(3.))
            .text_color(rgb(pal::SLATE))
            .hover(|s| s.bg(rgb(pal::WASH)).text_color(rgb(pal::INK)))
            .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .on_click(move |_, window, cx| {
                cx.stop_propagation();
                window.dispatch_action(Box::new(Close(Some(name.clone()))), cx);
            })
            .child(icon(CLOSE).size(t.css(9.)))
            .test_support()
            .into_any_element()
    };
    // Under the name (and needs-you) of the shown tab, on the rule: blue in the focused group.
    let line = div()
        .absolute()
        .bottom_0()
        .left(t.css(20.))
        .right(t.css(24.))
        .h(px(2.))
        .rounded(px(1.))
        .bg(rgb(if s.here { pal::BLUE } else { pal::TAB_LINE }));
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
        .gap(t.css(6.))
        .pl(t.css(8.))
        .pr(t.css(4.))
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
        .when(s.shown, |el| el.child(line))
        .child(dot.test_support())
        .child(title)
        .children(needs)
        .child(close)
        .test_support()
        .into_any_element()
}

/// A tab's dot, only for news: the agent working, or stuck (S3 A5).
fn news(bus: &str) -> Option<u32> {
    match bus {
        "active" => Some(pal::BLUE),
        "blocked" => Some(pal::RED),
        _ => None,
    }
}

/// The strip's glyphs, Lucide's as the kit ships them (`gpui-kit-assets`): the app registers no asset
/// source, so they are drawn from their bytes.
const CHEVRON: &[u8] = br#"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6"/></svg>"#;
const CLOSE: &[u8] = br#"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>"#;
const MAXIMIZE: &[u8] = br#"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 3h6v6"/><path d="m21 3-7 7"/><path d="m3 21 7-7"/><path d="M9 21H3v-6"/></svg>"#;
const RESTORE: &[u8] = br#"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m14 10 7-7"/><path d="M20 10h-6V4"/><path d="m3 21 7-7"/><path d="M4 14h6v6"/></svg>"#;

/// A glyph in its parent's text colour.
fn icon(svg: &[u8]) -> Icon {
    Icon::default().data(svg)
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
            .font_family(SANS_T)
            .text_size(px(13.))
            .opacity(0.85)
            .child(self.0.clone())
    }
}
