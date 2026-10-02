import { tinykeys, type KeybindingHandler, type KeybindingsMap } from 'tinykeys'

export type TabDirection = 'previous' | 'next'

export type ShellShortcutActions = {
  quickOpen: () => boolean | void
  closePanel: () => boolean | void
  openShortcutReference: () => boolean | void
  closeShortcutReference: () => boolean | void
  switchTab: (direction: TabDirection) => boolean | void
  switchSpace: (direction: TabDirection) => boolean | void
  focusFleet: () => boolean | void
  toggleNotesRail: () => boolean | void
  focusComposer: () => boolean | void
  goToTop: () => boolean | void
  goToBottom: () => boolean | void
  toggleMaximize: () => boolean | void
  toggleRead: () => boolean | void
}

export type ShortcutLabels = {
  closePanel: string
  quickOpen: string
  switchTabs: string
  switchSpaces: string
  spaceSwitcher: string
  focusFleet: string
  toggleNotesRail: string
  focusComposer: string
  sendRequest: string
  leaveComposer: string
  goToTop: string
  goToBottom: string
  toggleMaximize: string
  toggleRead: string
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
    spaceSwitcher: '⌥Tab / ⇧⌥Tab',
    focusFleet: '⌥1',
    toggleNotesRail: '⌥3',
    focusComposer: '⌥2',
    sendRequest: '⌘Enter',
    leaveComposer: 'Esc',
    goToTop: '⌥↑',
    goToBottom: '⌥↓',
    toggleMaximize: '⌥⏎',
    toggleRead: '⌥U',
    browserClose: '⌘W',
  } : {
    closePanel: 'Alt+W',
    quickOpen: 'Ctrl+K',
    switchTabs: 'Alt+Left / Alt+Right',
    switchSpaces: 'Shift+Alt+Left / Shift+Alt+Right',
    spaceSwitcher: 'Alt+Tab / Shift+Alt+Tab',
    focusFleet: 'Alt+1',
    toggleNotesRail: 'Alt+3',
    focusComposer: 'Alt+2',
    sendRequest: 'Ctrl+Enter',
    leaveComposer: 'Esc',
    goToTop: 'Alt+Up',
    goToBottom: 'Alt+Down',
    toggleMaximize: 'Alt+Enter',
    toggleRead: 'Alt+U',
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
    'Alt+ArrowUp': claimed(actions.goToTop, true),
    'Alt+ArrowDown': claimed(actions.goToBottom, true),
    'Alt+Enter': claimed(actions.toggleMaximize, true),
    'Alt+KeyU': claimed(actions.toggleRead, true),
    '$mod+PageUp': claimed(() => actions.switchTab('previous')),
    '$mod+PageDown': claimed(() => actions.switchTab('next')),
    'Alt+Digit1': claimed(actions.focusFleet, true),
    'Alt+Digit2': claimed(actions.focusComposer, true),
    'Alt+Digit3': claimed(actions.toggleNotesRail, true),
    ...(isMacPlatform(userAgent) ? { 'Meta+Slash': quickOpen } : {}),
  }
  return tinykeys(target, bindings, {
    // Target guards are deliberately owned by our individual callbacks.
    ignore: (event) => event.repeat || event.isComposing,
  })
}

export type SwitcherIntent = 'forward' | 'backward' | 'release' | 'cancel'

export type SpaceSwitcherKeys = {
  // cycle opens the switcher or moves it; false leaves Tab to the browser.
  cycle: (direction: 'forward' | 'backward') => boolean | void
  holding: () => boolean
  intent: (intent: SwitcherIntent) => void
}

// switcherKeyIntent maps a key pressed while the switcher is held; null
// keys are swallowed without effect. A key arriving without Alt means the
// Alt keyup was missed, so it commits.
export function switcherKeyIntent(event: Pick<KeyboardEvent, 'key' | 'altKey'>): SwitcherIntent | null {
  if (event.key === 'Escape') return 'cancel'
  if (event.key === 'ArrowDown') return 'forward'
  if (event.key === 'ArrowUp') return 'backward'
  if (event.key === 'Enter') return 'release'
  if (event.key === 'Alt' || event.key === 'Shift') return null
  return event.altKey ? null : 'release'
}

// bindSpaceSwitcher claims ⌥Tab (Alt+Tab) in the capture phase so the
// browser does not cycle focus and no panel swallows it first. Holding
// state then follows the Alt keyup; a window blur cancels (see
// spaceSwitcherModel for why blur cancels rather than commits).
export function bindSpaceSwitcher(target: Window, keys: SpaceSwitcherKeys) {
  const stopTinykeys = tinykeys(target, {
    'Alt+Tab': claimed(() => keys.cycle('forward')),
    'Shift+Alt+Tab': claimed(() => keys.cycle('backward')),
  }, { capture: true, ignore: (event) => event.isComposing })
  // While held, every key but ⌥Tab itself belongs to the switcher: it is
  // consumed here, in the window's capture phase, so no shell shortcut
  // (Alt+W, ⇧⌥←/→ …) or panel mutates the workspace under the overlay.
  const keydown = (event: KeyboardEvent) => {
    if (!keys.holding() || (event.key === 'Tab' && event.altKey)) return
    event.preventDefault()
    event.stopImmediatePropagation()
    const intent = switcherKeyIntent(event)
    if (intent) keys.intent(intent)
  }
  const keyup = (event: KeyboardEvent) => {
    if (keys.holding() && (event.key === 'Alt' || !event.altKey)) keys.intent('release')
  }
  const blur = () => { if (keys.holding()) keys.intent('cancel') }
  target.addEventListener('keydown', keydown, true)
  target.addEventListener('keyup', keyup, true)
  target.addEventListener('blur', blur)
  return () => {
    stopTinykeys()
    target.removeEventListener('keydown', keydown, true)
    target.removeEventListener('keyup', keyup, true)
    target.removeEventListener('blur', blur)
  }
}
