import { useEffect } from 'react'
import type { SpaceDefinition } from './spacesModel.ts'
import { attentionLabel, quietAttention, type SpaceAttention } from './spaceAttentionModel.ts'
import type { SwitcherState } from './spaceSwitcherModel.ts'
import { AttentionMarks } from './SpacesSection.tsx'

// SpaceSwitcher paints the held ⌥Tab list. Keyboard focus stays where it
// was (the keys belong to the window binding), so the highlighted option
// is announced through a live region as well as aria-activedescendant.
// The list scrolls, so the highlighted option is scrolled into view on
// open and on every step.
const optionID = (id: string) => `space-switcher-${encodeURIComponent(id)}`

export function SpaceSwitcher({ state, spaces, activeID, attention, onChoose }: {
  state: SwitcherState
  spaces: SpaceDefinition[]
  activeID: string | null
  attention: Record<string, SpaceAttention>
  onChoose: (id: string) => void
}) {
  const shownID = state.phase === 'holding' ? state.order[state.index] : undefined
  useEffect(() => {
    if (shownID) document.getElementById(optionID(shownID))?.scrollIntoView({ block: 'nearest' })
  }, [shownID])
  if (state.phase !== 'holding') return null
  const byID = new Map(spaces.map((space) => [space.id, space]))
  const options = state.order.flatMap((id) => {
    const space = byID.get(id)
    return space ? [space] : []
  })
  const highlighted = state.order[state.index]
  const current = highlighted ? byID.get(highlighted) : undefined
  return <div className="space-switcher-backdrop" role="presentation">
    <div className="space-switcher" role="listbox" aria-label="Switch space" tabIndex={-1}
      aria-activedescendant={highlighted ? optionID(highlighted) : undefined}>
      {options.map((space) => {
        const marks = attention[space.id] ?? quietAttention
        return <div role="option" id={optionID(space.id)} key={space.id} aria-selected={space.id === highlighted}
          aria-label={`${space.name}, ${attentionLabel(marks)}${space.id === activeID ? ', current space' : ''}`}
          className={`space-switcher-option${space.id === highlighted ? ' highlighted' : ''}${space.id === activeID ? ' current' : ''}`}
          onPointerDown={(event) => { event.preventDefault(); onChoose(space.id) }}>
          <span className="space-label">{space.name}</span>
          {space.id === activeID && <span className="space-switcher-current">current</span>}
          <AttentionMarks attention={marks} />
        </div>
      })}
    </div>
    <span className="visually-hidden" role="status" aria-live="assertive">{current ? `${current.name}, ${attentionLabel(attention[current.id] ?? quietAttention)}` : ''}</span>
  </div>
}
