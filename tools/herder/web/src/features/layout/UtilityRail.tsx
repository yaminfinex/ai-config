import type { ReactNode } from 'react'
import { subscribeDOMEvent } from '../../shared/lifecycle'
import {
  maximumRailWidth,
  minimumRailWidth,
  railWidthFromKey,
  resizedRailWidth,
  type RailSide,
} from './utilityRailModel'

export function UtilityRail({ side, label, detail, headingStart, width, collapsed, onWidth, onToggle, children }: {
  side: RailSide
  label: string
  detail?: ReactNode
  headingStart?: ReactNode
  width: number
  collapsed: boolean
  onWidth: (width: number) => void
  onToggle: () => void
  children: ReactNode
}) {
  const toggleTitle = `${collapsed ? 'Open' : 'Collapse'} ${label} rail`
  const toggle = <button type="button"
    className={`rail-toggle${collapsed ? ' rail-toggle-collapsed' : ''} rail-toggle-${side}`}
    aria-label={toggleTitle} title={toggleTitle} onClick={onToggle}>
    <span aria-hidden="true">{side === 'left' ? collapsed ? '›' : '‹' : collapsed ? '‹' : '›'}</span>
  </button>

  if (collapsed) return toggle

  const startResize = (event: React.PointerEvent) => {
    const startX = event.clientX
    const move = (moveEvent: PointerEvent) => onWidth(resizedRailWidth(width, side, moveEvent.clientX - startX))
    let disposeUp: () => void = () => undefined
    const disposeMove = subscribeDOMEvent<PointerEvent>(window, 'pointermove', move)
    const stop = () => { disposeMove(); disposeUp() }
    disposeUp = subscribeDOMEvent(window, 'pointerup', stop)
  }
  const rail = <aside className={`utility-rail utility-rail-${side}`} aria-label={`${label} rail`} style={{ width }} tabIndex={-1}>
    <header className="rail-heading">{headingStart}<strong>{label}</strong>{detail && <span>{detail}</span>}{toggle}</header>
    {children}
  </aside>
  const resizer = <div className={`utility-rail-resizer utility-rail-resizer-${side}`} role="separator"
    aria-label={`Resize ${label.toLowerCase()} rail`} aria-orientation="vertical"
    aria-valuemin={minimumRailWidth} aria-valuemax={maximumRailWidth} aria-valuenow={width} tabIndex={0}
    onPointerDown={startResize} onKeyDown={(event) => {
      if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
      onWidth(railWidthFromKey(width, side, event.key))
      event.preventDefault()
    }} />
  return side === 'left' ? <>{rail}{resizer}</> : <>{resizer}{rail}</>
}
