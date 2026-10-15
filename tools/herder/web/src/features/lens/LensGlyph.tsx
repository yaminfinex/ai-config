import { statusGlyph, type LensStatus } from './lensModel.ts'

// LensGlyph is an agent's status mark on a card and in the peek.
export function LensGlyph({ status }: { status: LensStatus | null }) {
  if (!status) return null
  if (status === 'working') return <span className="lens-glyph working" aria-label="working"><span className="lens-pulse" /></span>
  return <span className={`lens-glyph ${status}`} aria-label={status}>{statusGlyph[status]}</span>
}
