import { useCallback, useEffect, useRef, type RefObject } from 'react'

type EventTargetRef = RefObject<EventTarget | null>
type Target = EventTarget | EventTargetRef | null | undefined

function currentTarget(target: Target) {
  return target && 'current' in target ? target.current : target
}

export function subscribeDOMEvent<E extends Event>(
  target: EventTarget,
  type: string,
  handler: (event: E) => void,
  options?: boolean | AddEventListenerOptions,
) {
  const listener: EventListener = (event) => handler(event as E)
  target.addEventListener(type, listener, options)
  return () => target.removeEventListener(type, listener, typeof options === 'boolean' ? options : options?.capture)
}

export function useDOMEvent<E extends Event>(
  target: Target,
  type: string,
  handler: (event: E) => void,
  options?: boolean | AddEventListenerOptions,
  enabled = true,
) {
  const handlerRef = useRef(handler)
  handlerRef.current = handler
  const capture = typeof options === 'boolean' ? options : options?.capture
  useEffect(() => {
    const resolved = currentTarget(target)
    if (!enabled || !resolved) return
    return subscribeDOMEvent<E>(resolved, type, (event) => handlerRef.current(event), options)
  }, [capture, enabled, target, type])
}

export function useScheduledFrame() {
  const frame = useRef<number | undefined>(undefined)
  useEffect(() => () => {
    if (frame.current !== undefined) window.cancelAnimationFrame(frame.current)
  }, [])
  return useCallback((callback: () => void) => {
    if (frame.current !== undefined) window.cancelAnimationFrame(frame.current)
    frame.current = window.requestAnimationFrame(() => {
      frame.current = undefined
      callback()
    })
  }, [])
}

export function useSizeObserver<T extends Element>(
  ref: RefObject<T | null>,
  onChange: (target: T) => void,
  enabled = true,
  version?: unknown,
  related?: (target: T) => Element[],
) {
  const onChangeRef = useRef(onChange)
  const relatedRef = useRef(related)
  onChangeRef.current = onChange
  relatedRef.current = related
  useEffect(() => {
    const target = ref.current
    if (!enabled || !target || typeof ResizeObserver === 'undefined') return
    const update = () => onChangeRef.current(target)
    const observer = new ResizeObserver(update)
    observer.observe(target)
    relatedRef.current?.(target).forEach((element) => observer.observe(element))
    update()
    return () => observer.disconnect()
  }, [enabled, ref, version])
}

// startPointerDrag runs one pointer's drag from a pointerdown: the target
// captures the pointer, moves from any other pointer are ignored, and
// pointerup or pointercancel ends the drag. The disposer removes all three
// listeners and releases capture, so a mid-drag unmount leaves nothing behind.
export function startPointerDrag(event: { pointerId: number, currentTarget: EventTarget | null }, onMove: (event: PointerEvent) => void) {
  const { pointerId } = event
  const target = event.currentTarget as (EventTarget & { setPointerCapture?: (id: number) => void, releasePointerCapture?: (id: number) => void }) | null
  try { target?.setPointerCapture?.(pointerId) } catch { /* capture is best effort: a detached target still gets window moves */ }
  let disposed = false
  const dispose = () => {
    if (disposed) return
    disposed = true
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', end)
    window.removeEventListener('pointercancel', end)
    try { target?.releasePointerCapture?.(pointerId) } catch { /* already released */ }
  }
  const move = (moveEvent: PointerEvent) => { if (moveEvent.pointerId === pointerId) onMove(moveEvent) }
  const end = (endEvent: PointerEvent) => { if (endEvent.pointerId === pointerId) dispose() }
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', end)
  window.addEventListener('pointercancel', end)
  return dispose
}
