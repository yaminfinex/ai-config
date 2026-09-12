import { useEffect, useRef, type ReactNode } from 'react'
import { startPointerDrag } from '../../shared/lifecycle'
import {
  maximumRailWidth,
  minimumRailWidth,
  railWidthFromKey,
  resizedRailWidth,
  type RailSide,
} from './utilityRailModel'

export function UtilityRail({ side, label, headingStart, headingAction, subheading, width, collapsed, onWidth, onToggle, children }: {
  side: RailSide
  label: string
  headingStart?: ReactNode
  headingAction?: ReactNode
  // subheading renders on its own row directly under the heading line, for
  // controls too wide to share the row with the label and collapse button.
  subheading?: ReactNode
  width: number
  collapsed: boolean
  onWidth: (width: number) => void
  onToggle: () => void
  children: ReactNode
}) {
  const disposeDrag = useRef<(() => void) | null>(null)
  useEffect(() => () => disposeDrag.current?.(), [])
  const startResize = (event: React.PointerEvent) => {
    const startX = event.clientX
    disposeDrag.current?.()
    disposeDrag.current = startPointerDrag(event, (moveEvent) => onWidth(resizedRailWidth(width, side, moveEvent.clientX - startX)))
  }
  const rail = <aside className={`utility-rail utility-rail-${side}`} aria-label={`${label} rail`} style={{ width }} tabIndex={-1} hidden={collapsed}>
    <header className="rail-heading">{headingStart}<strong>{label}</strong>{headingAction}<button type="button"
      className={`rail-toggle rail-toggle-${side}`} aria-label={`Collapse ${label} rail`} title={`Collapse ${label} rail`}
      onClick={onToggle}><span aria-hidden="true">{side === 'left' ? '‹' : '›'}</span></button></header>
    {subheading && <div className="rail-subheading">{subheading}</div>}
    {children}
  </aside>
  const resizer = <div className={`utility-rail-resizer utility-rail-resizer-${side}`} role="separator" hidden={collapsed}
    aria-label={`Resize ${label.toLowerCase()} rail`} aria-orientation="vertical"
    aria-valuemin={minimumRailWidth} aria-valuemax={maximumRailWidth} aria-valuenow={width} tabIndex={0}
    onPointerDown={startResize} onKeyDown={(event) => {
      if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
      onWidth(railWidthFromKey(width, side, event.key))
      event.preventDefault()
    }} />
  return side === 'left' ? <>{rail}{resizer}</> : <>{resizer}{rail}</>
}

export function RailStatusToggle({ side, label, shortcut, collapsed, onToggle }: {
  side: RailSide
  label: string
  shortcut: string
  collapsed: boolean
  onToggle: () => void
}) {
  const state = collapsed ? 'collapsed' : 'open'
  const title = `${label} rail: ${state} (${shortcut})`
  return <button type="button" className={`rail-status-toggle rail-status-toggle-${side}`} aria-label={title}
    aria-pressed={!collapsed} title={title} onClick={onToggle}>
    <svg aria-hidden="true" viewBox="0 0 16 14" width="16" height="14"><rect x="1" y="1" width="14" height="12" rx="1" />
      {!collapsed && <path d={side === 'left' ? 'M2 2h4v10H2z' : 'M10 2h4v10h-4z'} />}</svg>
  </button>
}
