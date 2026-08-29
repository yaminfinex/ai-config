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
