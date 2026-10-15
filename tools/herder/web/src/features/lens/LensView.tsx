import { useEffect, useRef } from 'react'
import { useStreamAlerts } from '../../stream/useFleetStream'
import { useSizeObserver } from '../../shared/lifecycle'
import { headerLine, revealScroll, statusGlyph, type LensStatus } from './lensModel.ts'
import type { LensCard, LensRowView } from './useLens.ts'

function Glyph({ status }: { status: LensStatus | null }) {
  if (!status) return null
  if (status === 'working') return <span className="lens-glyph working" aria-label="working"><span className="lens-pulse" /></span>
  return <span className={`lens-glyph ${status}`} aria-label={status}>{statusGlyph[status]}</span>
}

function Card({ card, onSelect, onZoom }: { card: LensCard, onSelect: (id: string) => void, onZoom: (id: string) => void }) {
  const label = `${card.name}${card.needs > 0 ? `, ${card.needs} need you` : ''}${card.look.selected ? ', selected' : ''}`
  return <div className={`lens-card${card.look.needs ? ' needs' : ''}${card.look.selected ? ' selected' : ''}${card.lines === 0 ? ' lens-compact' : ''}`}
    role="listitem" aria-current={card.look.selected ? 'true' : undefined} aria-label={label} data-space-id={card.id}
    style={{ width: card.width, opacity: card.look.opacity }}
    onClick={() => onSelect(card.id)} onDoubleClick={() => onZoom(card.id)}>
    <div className="lens-title"><strong>{card.name}</strong>{card.needs > 0 && <span className="lens-pill">{card.needs}</span>}</div>
    <div className="lens-who"><Glyph status={card.status} /><span className="lens-agent">{card.agent}</span>{card.others > 0 && <span className="lens-others">+{card.others}</span>}</div>
    {card.lines > 0 && <div className="lens-body" style={{ WebkitLineClamp: card.lines, height: `calc(${card.lines} * var(--lens-line))` }}>{card.text}</div>}
  </div>
}

// LensView is home: the header line, then the three rows of space cards.
// A click selects, a double-click zooms; the keys live in useLens. The
// selection is kept in view with the least scroll after every lens key and
// every resize.
export function LensView({ rows, spaces, needYou, selected, revealTick, onSelect, onZoom }: {
  rows: LensRowView[]
  spaces: number
  needYou: number
  selected: string | null
  revealTick: number
  onSelect: (id: string) => void
  onZoom: (id: string) => void
}) {
  const stream = useStreamAlerts()
  const scroller = useRef<HTMLDivElement | null>(null)
  const reveal = () => {
    const view = scroller.current
    const card = view?.querySelector<HTMLElement>('.lens-card.selected')
    if (!view || !card) return
    view.scrollTop = revealScroll(view.getBoundingClientRect(), card.getBoundingClientRect(), view.scrollTop)
  }
  // reveal reads the DOM the selection just drew.
  useEffect(reveal, [revealTick, selected])
  useSizeObserver(scroller, reveal)
  // A click on a card takes the keys back to the lens.
  const focusLens = (event: React.MouseEvent) => (event.currentTarget as HTMLElement).focus({ preventScroll: true })
  return <section className="lens" aria-label="Lens" tabIndex={-1} onClick={focusLens}>
    <div className="lens-rows" ref={scroller}>
      <div className="lens-header">{headerLine({ live: !stream.problems.stream, spaces, needYou, serverUpdated: stream.serverUpdated })}</div>
      {rows.map((row) => <section className={`lens-row lens-row-${row.row}`} key={row.row} aria-label={row.head}>
        <div className="lens-head">{row.head}</div>
        <div className="lens-grid" role="list" aria-label={`${row.row} spaces`}>
          {row.cards.map((card) => <Card card={card} key={card.id} onSelect={onSelect} onZoom={onZoom} />)}
        </div>
      </section>)}
    </div>
  </section>
}
