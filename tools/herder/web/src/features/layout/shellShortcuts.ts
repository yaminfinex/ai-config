import { tinykeys, type KeybindingHandler, type KeybindingsMap } from 'tinykeys'

export type TabDirection = 'previous' | 'next'

export type ShellShortcutActions = {
  quickOpen: () => boolean | void
  closePanel: () => boolean | void
  openShortcutReference: () => boolean | void
  closeShortcutReference: () => boolean | void
  switchTab: (direction: TabDirection) => boolean | void
  switchSpace: (direction: TabDirection) => boolean | void
  reorderSpace: (direction: TabDirection) => boolean | void
  focusFleet: () => boolean | void
  toggleNotesRail: () => boolean | void
  captureNote: () => boolean | void
  focusComposer: () => boolean | void
  goToTop: () => boolean | void
  goToBottom: () => boolean | void
  toggleMaximize: () => boolean | void
}

export type ShortcutLabels = {
  closePanel: string
  quickOpen: string
  switchTabs: string
  switchSpaces: string
  reorderSpace: string
  focusFleet: string
  toggleNotesRail: string
  captureNote: string
  focusComposer: string
  sendRequest: string
  leaveComposer: string
  goToTop: string
  goToBottom: string
  toggleMaximize: string
  browserClose: string
}

export function isMacPlatform(userAgent: string) {
  return userAgent.includes('Mac')
}

export function shortcutLabels(userAgent: string): ShortcutLabels {
  return isMacPlatform(userAgent) ? {
    closePanel: '⌥W',
    quickOpen: '⌘K',
    switchTabs: '⌥← / ⌥→',
    switchSpaces: '⇧⌥← / ⇧⌥→',
    reorderSpace: '⌘← / ⌘→',
    focusFleet: '⌥1',
    toggleNotesRail: '⌥3',
    captureNote: '⌥4',
    focusComposer: '⌥2',
    sendRequest: '⌘Enter',
    leaveComposer: 'Esc',
    goToTop: '⌥↑',
    goToBottom: '⌥↓',
    toggleMaximize: '⌥⏎',
    browserClose: '⌘W',
  } : {
    closePanel: 'Alt+W',
    quickOpen: 'Ctrl+K',
    switchTabs: 'Alt+Left / Alt+Right',
    switchSpaces: 'Shift+Alt+Left / Shift+Alt+Right',
    reorderSpace: 'Ctrl+Left / Ctrl+Right',
    focusFleet: 'Alt+1',
    toggleNotesRail: 'Alt+3',
    captureNote: 'Alt+4',
    focusComposer: 'Alt+2',
    sendRequest: 'Ctrl+Enter',
    leaveComposer: 'Esc',
    goToTop: 'Alt+Up',
    goToBottom: 'Alt+Down',
    toggleMaximize: 'Alt+Enter',
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
    'Shift+Alt+ArrowLeft': claimed(() => actions.switchSpace('previous'), true),
    'Shift+Alt+ArrowRight': claimed(() => actions.switchSpace('next'), true),
    'Meta+ArrowLeft': claimed(() => actions.reorderSpace('previous'), true),
    'Meta+ArrowRight': claimed(() => actions.reorderSpace('next'), true),
    'Control+ArrowLeft': claimed(() => actions.reorderSpace('previous'), true),
    'Control+ArrowRight': claimed(() => actions.reorderSpace('next'), true),
    'Alt+ArrowUp': claimed(actions.goToTop, true),
    'Alt+ArrowDown': claimed(actions.goToBottom, true),
    'Alt+Enter': claimed(actions.toggleMaximize, true),
    '$mod+PageUp': claimed(() => actions.switchTab('previous')),
    '$mod+PageDown': claimed(() => actions.switchTab('next')),
    'Alt+Digit1': claimed(actions.focusFleet, true),
    'Alt+Digit2': claimed(actions.focusComposer, true),
    'Alt+Digit3': claimed(actions.toggleNotesRail, true),
    'Alt+Digit4': claimed(actions.captureNote, true),
    ...(isMacPlatform(userAgent) ? { 'Meta+Slash': quickOpen } : {}),
  }
  return tinykeys(target, bindings, {
    // Target guards are deliberately owned by our individual callbacks.
    ignore: (event) => event.repeat || event.isComposing,
  })
}
