import { tinykeys, type KeybindingHandler, type KeybindingsMap } from 'tinykeys'

export type TabDirection = 'previous' | 'next'

export type ShellShortcutActions = {
  quickOpen: () => boolean | void
  closePanel: () => boolean | void
  openShortcutReference: () => boolean | void
  closeShortcutReference: () => boolean | void
  switchTab: (direction: TabDirection) => boolean | void
  focusFleet: () => boolean | void
  focusComposer: () => boolean | void
}

export type ShortcutLabels = {
  closePanel: string
  quickOpen: string
  switchTabs: string
  focusFleet: string
  focusComposer: string
  sendRequest: string
  browserClose: string
}

export function isMacPlatform(userAgent: string) {
  return userAgent.includes('Mac')
}

export function shortcutLabels(userAgent: string): ShortcutLabels {
  return isMacPlatform(userAgent) ? {
    closePanel: '⌥W',
    quickOpen: '⌘K',
    switchTabs: '⌥← / ⌥→ (legacy: ⌘PageUp / ⌘PageDown)',
    focusFleet: '⌥1',
    focusComposer: '⌥2',
    sendRequest: '⌘Enter',
    browserClose: '⌘W',
  } : {
    closePanel: 'Alt+W',
    quickOpen: 'Ctrl+K',
    switchTabs: 'Alt+Left / Alt+Right (legacy: Ctrl+PageUp / Ctrl+PageDown)',
    focusFleet: 'Alt+1',
    focusComposer: 'Alt+2',
    sendRequest: 'Ctrl+Enter',
    browserClose: 'Ctrl+W',
  }
}

export function isEditableShortcutTarget(target: EventTarget | null) {
  return typeof HTMLElement !== 'undefined' && target instanceof HTMLElement &&
    (target.isContentEditable || Boolean(target.closest('input, textarea, select')))
}

function claimed(action: () => boolean | void, guarded = false): KeybindingHandler {
  return (event) => {
    if (guarded && isEditableShortcutTarget(event.target)) return
    if (action() === false) return
    event.preventDefault()
  }
}

export function bindShellShortcuts(target: Window | HTMLElement, actions: ShellShortcutActions, userAgent: string) {
  const quickOpen = claimed(actions.quickOpen)
  const bindings: KeybindingsMap = {
    '$mod+KeyK': quickOpen,
    'Alt+KeyW': claimed(actions.closePanel, true),
    '[Shift]+?': claimed(actions.openShortcutReference, true),
    'Escape': claimed(actions.closeShortcutReference),
    'Alt+ArrowLeft': claimed(() => actions.switchTab('previous'), true),
    'Alt+ArrowRight': claimed(() => actions.switchTab('next'), true),
    '$mod+PageUp': claimed(() => actions.switchTab('previous')),
    '$mod+PageDown': claimed(() => actions.switchTab('next')),
    'Alt+Digit1': claimed(actions.focusFleet, true),
    'Alt+Digit2': claimed(actions.focusComposer, true),
    ...(isMacPlatform(userAgent) ? { 'Meta+Slash': quickOpen } : {}),
  }
  return tinykeys(target, bindings, {
    // Target guards are deliberately owned by our individual callbacks.
    ignore: (event) => event.repeat || event.isComposing,
  })
}
