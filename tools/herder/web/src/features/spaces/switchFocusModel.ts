// Arriving at an agent means "I want to talk to it" (owner #463121): every
// way of landing on one (a zoom from the lens, n/N, ⇧⌥←/→, ⌥Tab,
// ⌘K, the Spaces list, a sidebar or notes agent, a tab, ⌥←/→, a mention,
// back/forward) puts the caret at the end of its composer draft when the
// agent is writable, else focuses its panel, so the keys are the panel's.
// Focus never moves out of a text field the owner was typing in (a space
// rename). The quick-open search is not such a field: it says so with the
// 'quick-open' origin, since the palette closes on the choice. A composer
// counts as ours, so ⌥Tab from one composer lands in the next. A file,
// folder or changes panel keeps focus where it is. Focus alone never marks anything
// read: viewing still follows the dwell rule.

export type ArriveInput = {
  // The element that had focus when the arrival began, or 'quick-open' when
  // it was chosen in the ⌘K palette.
  origin: 'composer' | 'text-field' | 'quick-open' | 'other'
  activePanelKind: string | undefined
  // The arriving agent's composer once rendered; null until it mounts.
  composer: { disabled: boolean } | null
  // Whether the agent's panel is drawn to take focus instead.
  panel: boolean
  // The last try: a composer that has not mounted by now never will (a
  // retired agent), so the panel takes the keys.
  lastTry: boolean
}

export type ArriveDecision = 'composer' | 'panel' | 'wait' | 'leave'

export function arriveDecision({ origin, activePanelKind, composer, panel, lastTry }: ArriveInput): ArriveDecision {
  if (origin === 'text-field' || activePanelKind !== 'agent') return 'leave'
  if (composer && !composer.disabled) return 'composer'
  if (composer && panel) return 'panel'
  if (!lastTry) return 'wait'
  return panel ? 'panel' : 'leave'
}

const textField = 'input:not([type=button], [type=checkbox], [type=radio], [type=range], [type=submit], [type=reset]), textarea, select, [contenteditable]:not([contenteditable=false])'

export function focusOrigin(element: Pick<Element, 'matches'> | null): Exclude<ArriveInput['origin'], 'quick-open'> {
  if (!element) return 'other'
  if (element.matches('textarea[data-composer]')) return 'composer'
  return element.matches(textField) ? 'text-field' : 'other'
}

// The caret goes after any draft, and the field scrolls to show it.
export function focusAtEnd(field: HTMLTextAreaElement) {
  field.focus({ preventScroll: true })
  const end = field.value.length
  field.setSelectionRange(end, end)
  field.scrollTop = field.scrollHeight
}
