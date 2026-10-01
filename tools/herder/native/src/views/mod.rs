//! GPUI views. They render from `&Store` and dispatch `Event`s through the shell; domain state never
//! changes here. Each view owns the GPUI widget entities it renders with (`ListState`, `TextareaState`,
//! `EditorState`); those are widget state, not domain state, and never go through the store. Every size
//! comes from `theme::type_scale`.
//!
//! One file per surface, added by the unit that needs it: `lens` (U2, the home rows and cards), `space`
//! (U2, the zoom shell and tabs), `transcript` (U3), `composer` (U4), `notes` (U5). `theme` holds the
//! palette and the type scale. This file holds what they share: the key table and its help, the
//! agent chrome (glyph, label, pill), and the window's `Frame` with the working-dot `Pulse`.

pub mod lens;
pub mod markdown;
pub mod space;
#[cfg(test)]
mod tests;
pub mod theme;
pub mod transcript;

use crate::store::fleet::{Agent, Status};
use crate::store::{Event, Store};
use gpui_kit::*;
use std::cell::RefCell;
use std::rc::Rc;
use std::sync::atomic::{AtomicU32, Ordering};
use std::time::Duration;
use theme::{TypeScale, pal};

actions!(herder, [Quit, TextBigger, TextSmaller, TextReset]);

/// Navigation letters bind here (ARCHITECTURE §4): a predicate sees the whole focus stack, so a
/// focused Input or Terminal anywhere below turns them off. App-wide chords bind on `Lens` alone.
pub const HOME: &str = "Lens && !Input && !Terminal";
pub const SPACE: &str = "Space && !Input && !Terminal";

pub fn bind(cx: &mut App) {
    cx.bind_keys(bindings());
}

/// Every binding in the app; `tests/keys.rs` checks none of the letters fires in an Input.
pub fn bindings() -> Vec<KeyBinding> {
    use lens::Nav::*;
    use space::Zoomed;
    let home = [
        ("left", Step(-1)),
        ("h", Step(-1)),
        ("right", Step(1)),
        ("l", Step(1)),
        ("j", StepRow(1)),
        ("k", StepRow(-1)),
        ("1", Place(crate::store::spaces::Row::Focus)),
        ("2", Place(crate::store::spaces::Row::Watch)),
        ("3", Place(crate::store::spaces::Row::Background)),
        ("v", CycleVisible),
        ("m", Read),
        ("u", Unread),
        ("t", CardText),
        ("s", CardSize),
        ("?", Help),
        ("enter", ZoomIn),
    ];
    let next = [("n", NextNeeding(false)), ("shift-n", NextNeeding(true))];
    use transcript::Scroll;
    let scroll = [
        ("j", Scroll::Lines(1)),
        ("k", Scroll::Lines(-1)),
        ("space", Scroll::Pages(1)),
        ("shift-space", Scroll::Pages(-1)),
        ("g", Scroll::Top),
        ("shift-g", Scroll::Bottom),
    ];
    let zoom = [
        ("escape", Zoomed::Out),
        ("[", Zoomed::Space(-1)),
        ("]", Zoomed::Space(1)),
        ("tab", Zoomed::Agent(1)),
        ("shift-tab", Zoomed::Agent(-1)),
    ];
    let mut keys = vec![
        KeyBinding::new("cmd-q", Quit, Some("Lens")),
        KeyBinding::new("cmd-=", TextBigger, Some("Lens")),
        KeyBinding::new("cmd-shift-=", TextBigger, Some("Lens")),
        KeyBinding::new("cmd--", TextSmaller, Some("Lens")),
        KeyBinding::new("cmd-0", TextReset, Some("Lens")),
    ];
    let home = home.into_iter().chain(next);
    keys.extend(home.map(|(k, a)| KeyBinding::new(k, a, Some(HOME))));
    keys.extend(next.map(|(k, a)| KeyBinding::new(k, a, Some(SPACE))));
    keys.extend(zoom.map(|(k, a)| KeyBinding::new(k, a, Some(SPACE))));
    keys.extend(scroll.map(|(k, a)| KeyBinding::new(k, a, Some(SPACE))));
    keys
}

/// The key help (`?`), in two monospace columns.
const HELP: &str = "\
lens
← → h l j k   move
enter         zoom in
n / N         next needing you / and zoom in
1 2 3         focus / watch / background
v             next visible agent
m / u         read / mark the space unread
t             status · title ↔ cwd
s             card size
?             this help

zoomed in
esc           back to the lens
[ ]           previous / next space
tab ⇧tab      next / previous agent
n / N         next space needing you
j k           scroll
space ⇧space  page down / up
g G           top of loaded (reads older) / end";

pub(super) fn help(t: TypeScale) -> Div {
    let lines = HELP.lines().map(|l| div().min_h(t.line).child(l));
    let panel = div()
        .p(t.px(24.))
        .bg(rgb(pal::PANEL))
        .border_1()
        .border_color(rgb(pal::RULE))
        .rounded(t.px(10.))
        .children(lines);
    let scrim = div().absolute().inset_0().bg(rgba(0x060A1099));
    scrim.flex().items_center().justify_center().child(panel)
}

/// What views need from the shell that owns them, so they depend on this trait and not on the shell:
/// the store to read beside the lens's view state, and the one path to a state change.
pub trait Host: Sized + 'static {
    fn parts(&mut self) -> (&Store, &mut lens::Ui);
    fn view(&self) -> (&Store, &lens::Ui);
    fn dispatch(&mut self, event: Event, cx: &mut Context<Self>);
}

/// An action handler: `f` reads the store, moves the view state and returns the events to dispatch;
/// then focus follows the zoom (the `Space` context is live only while its element has focus), and a
/// transition that started gets its end scheduled.
pub fn on<A: Action, H: Host>(
    cx: &mut Context<H>,
    f: impl Fn(&Store, &mut lens::Ui, &A) -> Vec<Event> + 'static,
) -> impl Fn(&A, &mut Window, &mut App) + 'static {
    cx.listener(move |host: &mut H, action: &A, window, cx| {
        let (store, ui) = host.parts();
        let before = ui.anim.as_ref().map(space::Anim::seq);
        let events = f(store, ui, action);
        let target = ui.focus_target().clone();
        if !target.is_focused(window) {
            window.focus(&target, cx);
        }
        if let Some(anim) = ui.anim.as_ref().filter(|a| Some(a.seq()) != before) {
            let (seq, done) = (anim.seq(), anim.duration());
            cx.spawn(async move |host, cx| {
                cx.background_executor().timer(done).await;
                host.update(cx, |host, cx| {
                    host.parts().1.settle(seq);
                    cx.notify();
                })
            })
            .detach();
        }
        for event in events {
            host.dispatch(event, cx);
        }
        cx.notify();
    })
}

pub(super) fn working(agent: &Agent) -> bool {
    agent.status() == Status::Working
}

/// The status glyph in its navigation-light colour. A working agent's dot is painted by `Pulse` over
/// this slot, so pulsing never re-renders the view around it.
pub(super) fn glyph(agent: &Agent, dots: &Dots, t: TypeScale) -> Div {
    let slot = div().flex_none().relative().w(t.px(12.)).h(t.line);
    let (g, color) = match agent.status() {
        Status::Working => return slot.child(dots.slot()),
        Status::Blocked => ("■", pal::PORT),
        Status::Done => ("✓", pal::SLATE),
        Status::Idle => ("○", pal::SLATE),
    };
    slot.text_color(rgb(color)).child(g)
}

pub(super) fn label(agent: &Agent, needs_you: bool) -> &'static str {
    match agent.status() {
        _ if needs_you => "your turn",
        Status::Working => "working",
        Status::Blocked => "blocked",
        Status::Done => "done",
        Status::Idle => "idle",
    }
}

pub(super) fn pill(n: usize, t: TypeScale) -> Div {
    div()
        .flex_none()
        .px(t.px(7.))
        .rounded_full()
        .bg(rgb(pal::ACC))
        .text_color(rgb(pal::GROUND))
        .text_size(t.small)
        .font_weight(FontWeight::BOLD)
        .child(n.to_string())
}

/// Secondary text.
pub(super) fn dim(text: impl Into<SharedString>) -> Div {
    div().text_color(rgb(pal::SLATE)).child(text.into())
}

/// The working dots on screen: each slot records its bounds when laid out inside the visible part of
/// its scroll area, and `Pulse` paints them. The shell clears the list whenever it renders.
#[derive(Clone, Default)]
pub struct Dots(Rc<RefCell<DotState>>);

#[derive(Default)]
struct DotState {
    live: Vec<Bounds<Pixels>>,
    /// Nothing records (a transition or the help is over the lens).
    paused: bool,
    running: bool,
}

impl Dots {
    pub fn clear(&self, paused: bool) {
        let mut s = self.0.borrow_mut();
        s.live.clear();
        s.paused = paused;
    }

    fn slot(&self) -> impl IntoElement {
        let dots = self.clone();
        let record = move |b: Bounds<Pixels>, window: &mut Window, _: &mut App| {
            let mut s = dots.0.borrow_mut();
            if !s.paused && window.content_mask().bounds.intersects(&b) {
                s.live.push(b);
            }
        };
        canvas(record, |_, _, _, _| {}).absolute().size_full()
    }
}

/// Paints of the working dots and pointer moves over the window, for the harness's `cpu:` step.
pub static PULSE_PAINTS: AtomicU32 = AtomicU32::new(0);
pub static POINTER_MOVES: AtomicU32 = AtomicU32::new(0);
/// The working dot's pulse: 4 steps over 1.6 s (bright, half, dim, half), while any dot is on screen.
/// Each step repaints the window, so fewer steps cost less (ARCHITECTURE §Performance).
const PULSE_STEP: Duration = Duration::from_millis(400);
const PULSE_PHASES: u32 = 4;

/// The window's root: the shell, cached so it only re-renders when it notifies, with the pulse layer
/// over it. The pulse notifies only itself, so its steps repaint the dots and reuse everything else.
pub struct Frame {
    main: AnyView,
    pulse: Entity<Pulse>,
}

impl Frame {
    pub fn new(main: AnyView, dots: Dots, cx: &mut App) -> Self {
        let pulse = cx.new(|_| Pulse { dots, phase: 0 });
        Frame { main, pulse }
    }
}

impl Render for Frame {
    fn render(&mut self, _: &mut Window, _: &mut Context<Self>) -> impl IntoElement {
        let main = self
            .main
            .clone()
            .cached(StyleRefinement::default().size_full());
        let pulse = div().absolute().inset_0().child(self.pulse.clone());
        let moved = |_: &MouseMoveEvent, _: &mut Window, _: &mut App| {
            POINTER_MOVES.fetch_add(1, Ordering::Relaxed);
        };
        let frame = div().size_full().relative().on_mouse_move(moved);
        frame.child(main).child(pulse)
    }
}

pub struct Pulse {
    dots: Dots,
    phase: u32,
}

impl Render for Pulse {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let (dots, pulse) = (self.dots.clone(), cx.weak_entity());
        let p = (self.phase % PULSE_PHASES) as f32 / PULSE_PHASES as f32;
        let color = Hsla::from(rgb(pal::GREEN)).opacity(0.3 + 0.7 * (2.0 * p - 1.0).abs());
        let paint = move |_, _, window: &mut Window, cx: &mut App| {
            let mut s = dots.0.borrow_mut();
            for b in &s.live {
                let d = b.size.width.min(b.size.height) * 0.6;
                let dot = Bounds::centered_at(b.center(), size(d, d));
                window.paint_quad(fill(dot, color).corner_radii(d / 2.0));
            }
            PULSE_PAINTS.fetch_add(1, Ordering::Relaxed);
            if !s.live.is_empty() && !s.running {
                s.running = true;
                step(pulse.clone(), cx);
            }
        };
        canvas(|_, _, _| {}, paint).size_full()
    }
}

/// Step the pulse until no dot is on screen, then stop; the next dot laid out starts it again.
fn step(pulse: WeakEntity<Pulse>, cx: &mut App) {
    cx.spawn(async move |cx| {
        loop {
            cx.background_executor().timer(PULSE_STEP).await;
            let going = pulse.update(cx, |p, cx| {
                let mut s = p.dots.0.borrow_mut();
                s.running = !s.live.is_empty();
                if s.running {
                    p.phase = p.phase.wrapping_add(1);
                    cx.notify();
                }
                s.running
            });
            if !matches!(going, Ok(true)) {
                break;
            }
        }
    })
    .detach();
}
