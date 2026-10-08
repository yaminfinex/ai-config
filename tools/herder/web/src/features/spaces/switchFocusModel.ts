// After a space switch the owner usually wants to type to the agent they
// were last talking to there, so a deliberate switch (a rail click, the ⌥Tab
// switcher, ⇧⌥←/→, a space chosen in ⌘K) lands the caret at the end of that
// agent's composer draft: ⌘K, the space name, Enter, then type. Focus only
// moves when the new space's active panel is an agent whose composer is
// visible and enabled, and never out of a text field the owner was typing in
// (a space rename). The quick-open search is not such a field: it says so
// with the 'quick-open' origin, since the palette closes on the choice. A
// composer counts as ours, so ⌥Tab from one composer lands in the next.
// Focus alone never marks anything read: viewing still follows the dwell rule.

export type SwitchFocusInput = {
  // The element that had focus when the switch began, or 'quick-open' when
  // the space was chosen in the ⌘K palette.
  origin: 'composer' | 'text-field' | 'quick-open' | 'other'
  activePanelKind: string | undefined
  // The active group's composer once rendered; null until it mounts.
  composer: { disabled: boolean, visible: boolean } | null
}

export type SwitchFocusDecision = 'focus' | 'wait' | 'leave'

export function switchFocusDecision({ origin, activePanelKind, composer }: SwitchFocusInput): SwitchFocusDecision {
  if (origin === 'text-field' || activePanelKind !== 'agent') return 'leave'
  if (!composer) return 'wait'
  return composer.visible && !composer.disabled ? 'focus' : 'leave'
}

const textField = 'input:not([type=button], [type=checkbox], [type=radio], [type=range], [type=submit], [type=reset]), textarea, select, [contenteditable]:not([contenteditable=false])'

export function focusOrigin(element: Pick<Element, 'matches'> | null): Exclude<SwitchFocusInput['origin'], 'quick-open'> {
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
