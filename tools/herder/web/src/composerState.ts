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

export function isComposerSendShortcut(event: Pick<KeyboardEvent, 'key' | 'ctrlKey' | 'metaKey'>) {
  return event.key === 'Enter' && (event.ctrlKey || event.metaKey)
}
