export const composerDraftPrefix = 'herder.web.messageDraft.v1:'

export function composerDraftKey(agentName: string) {
  return `${composerDraftPrefix}${encodeURIComponent(agentName)}`
}

export function readComposerDraft(storage: Pick<Storage, 'getItem'>, agentName: string) {
  try {
    return storage.getItem(composerDraftKey(agentName)) ?? ''
  } catch {
    return ''
  }
}

export function persistComposerDraft(storage: Pick<Storage, 'setItem' | 'removeItem'>, agentName: string, text: string) {
  try {
    if (text) storage.setItem(composerDraftKey(agentName), text)
    else storage.removeItem(composerDraftKey(agentName))
  } catch {
    // Draft persistence is best-effort when browser storage is unavailable.
  }
}

export function isComposerSendShortcut(event: Pick<KeyboardEvent, 'key' | 'ctrlKey' | 'metaKey'>) {
  return event.key === 'Enter' && (event.ctrlKey || event.metaKey)
}
