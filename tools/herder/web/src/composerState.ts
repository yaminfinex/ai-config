export const composerDraftPrefix = 'herder.web.messageDraft.v1:'

type ComposerStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

export function resolveComposerStorage(accessor: () => ComposerStorage = () => window.localStorage): ComposerStorage | null {
  try {
    return accessor()
  } catch {
    return null
  }
}

export function composerDraftKey(agentName: string) {
  return `${composerDraftPrefix}${encodeURIComponent(agentName)}`
}

export function composerFieldId(agentName: string) {
  return `message-${encodeURIComponent(agentName)}`
}

export function readComposerDraft(agentName: string, storage: Pick<ComposerStorage, 'getItem'> | null = resolveComposerStorage()) {
  try {
    return storage?.getItem(composerDraftKey(agentName)) ?? ''
  } catch {
    return ''
  }
}

export function persistComposerDraft(agentName: string, text: string, storage: Pick<ComposerStorage, 'setItem' | 'removeItem'> | null = resolveComposerStorage()) {
  try {
    if (!storage) return
    if (text) storage.setItem(composerDraftKey(agentName), text)
    else storage.removeItem(composerDraftKey(agentName))
  } catch {
    // Draft persistence is best-effort when browser storage is unavailable.
  }
}

export function isComposerSendShortcut(event: Pick<KeyboardEvent, 'key' | 'ctrlKey' | 'metaKey'> & { shiftKey?: boolean }) {
  return event.key === 'Enter' && !event.shiftKey && (event.ctrlKey || event.metaKey)
}

export type ComposerMeasureKey = { name: string, value: string }

// The auto-resizing field must collapse to zero before measuring so it can
// contract — but collapsing on every keystroke forces a reflow with the field at
// zero height, which transiently grows the sibling transcript and makes the
// browser clamp its scroll off the bottom, tripping the jump-to-bottom button
// while the owner is only typing. Skip the collapse only when the draft strictly
// grew by appended text (typing or pasting at the end): scrollHeight already
// reflects the taller content, so no collapse is needed. Any other edit — a
// deletion, a mid-draft insertion, a paste that replaces a selection (which can
// add characters yet remove rendered lines), or an agent switch — may shorten
// the field, so measure honestly from zero.
export function composerShouldRemeasureFromZero(previous: ComposerMeasureKey, next: ComposerMeasureKey) {
  return next.name !== previous.name || !next.value.startsWith(previous.value)
}

export function blurComposerOnEscape(event: { key: string, currentTarget: Pick<HTMLElement, 'blur'> }) {
  if (event.key !== 'Escape') return false
  event.currentTarget.blur()
  return true
}

// Selecting an agent means "I want to talk": focus its composer once the
// panel exists. New panels mount asynchronously, so retry a bounded number
// of frames and give up quietly (e.g. the agent went read-only, no textarea).
export function focusComposerWhenReady<Handle>(
  query: () => { focus: () => void } | null,
  schedule: (callback: () => void) => Handle,
  attempts = 20,
  cancel?: (handle: Handle) => void,
) {
  let cancelled = false
  let scheduled: Handle | undefined
  const attempt = (remaining: number) => {
    if (cancelled) return
    const composer = query()
    if (composer) return composer.focus()
    if (remaining > 0) scheduled = schedule(() => {
      scheduled = undefined
      attempt(remaining - 1)
    })
  }
  attempt(attempts)
  return () => {
    cancelled = true
    if (cancel && scheduled !== undefined) cancel(scheduled)
  }
}
