import assert from 'node:assert/strict'
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('../src/features/spaces/SpacesSection.tsx', import.meta.url), 'utf8')
const switcher = readFileSync(new URL('../src/features/spaces/SpaceSwitcher.tsx', import.meta.url), 'utf8')
const app = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8')
const controller = readFileSync(new URL('../src/features/workspace/useWorkspaceController.ts', import.meta.url), 'utf8')
const attentionHook = readFileSync(new URL('../src/features/workspace/useSpaceAttention.ts', import.meta.url), 'utf8')
const switcherHook = readFileSync(new URL('../src/features/workspace/useSpaceSwitcher.ts', import.meta.url), 'utf8')
const styles = readFileSync(new URL('../src/styles.css', import.meta.url), 'utf8')

test('the focused active space starts rename with Enter or F2', () => {
  assert.match(source, /event\.key === 'Enter' \|\| event\.key === 'F2'/)
  assert.match(source, /if \(space\.id === props\.activeID\) beginRename\(space\)/)
})

test('space rows do not advertise browser-owned Meta or Control arrow shortcuts', () => {
  assert.doesNotMatch(source, /aria-keyshortcuts=[^\n]*(?:Meta|Control)\+Arrow/)
})

test('the history button renders only when something can be reopened', () => {
  assert.match(source, /\{props\.recent\.length > 0 && <div ref=\{historyMenu\}/)
  assert.match(source, /className="space-history" aria-label="Recently closed spaces" title="Recently closed spaces"\s+aria-haspopup="menu" aria-expanded=\{historyOpen\}/)
})

test('the history menu is bounded to the eight most recent entries and only renders while open', () => {
  assert.match(source, /\{historyOpen && <div className="space-history-menu" role="menu" aria-label="Recently closed spaces">\s+\{props\.recent\.slice\(0, 8\)\.map/)
})

test('the inline reopen row is gone', () => {
  assert.doesNotMatch(source, /space-reopen|>reopen \{space\.name\}/)
})

test('choosing a history item reopens that space and closes the menu (reddens: close dropped)', () => {
  assert.match(source, /props\.reopen\(space\.id\); setHistoryOpen\(false\)/)
})

test('the history menu effect registers Escape to close (reddens: Escape listener dropped)', () => {
  const effect = source.slice(source.indexOf('if (!historyOpen) return'), source.indexOf('}, [historyOpen])'))
  assert.match(effect, /if \(event\.key === 'Escape'\) setHistoryOpen\(false\)/)
  assert.match(effect, /document\.addEventListener\('keydown', escape, true\)/)
})

test('rows reorder vertically: the drop side follows the pointer against the row midpoint', () => {
  assert.equal((source.match(/event\.clientY >= rect\.top \+ rect\.height \/ 2/g) ?? []).length, 2, 'drag over and drop use the same vertical midpoint')
  // The space menu opens at the pointer; the drag handlers never read it.
  assert.doesNotMatch(source.slice(source.indexOf('onDragStart='), source.indexOf('onDragEnd=')), /clientX/)
  assert.match(source, /const destination = targetIndex - \(sourceIndex < targetIndex \? 1 : 0\) \+ \(after \? 1 : 0\)\s+props\.reorder\(sourceID, destination\)/)
})

test('the section folds through the persisted preference and names its controlled list', () => {
  assert.match(source, /aria-expanded=\{!props\.collapsed\} aria-controls="spaces-list"\s+onClick=\{\(\) => props\.onCollapsed\(!props\.collapsed\)\}/)
  assert.match(source, /\{!props\.collapsed && <ul id="spaces-list"/)
  assert.match(controller, /collapsed: layout\.spacesCollapsed,\s+onCollapsed: layout\.setSpacesCollapsed/)
})

test('a folded section still shows the total waiting and blocked marks with a text alternative', () => {
  assert.match(source, /props\.collapsed && <><AttentionMarks attention=\{total\} \/><span className="visually-hidden">, \{attentionLabel\(total\)\}<\/span><\/>/)
})

test('each row names its waiting and blocked agents and marks the current space', () => {
  assert.match(source, /aria-current=\{active \? 'true' : undefined\}\s+aria-label=\{`\$\{space\.name\}, \$\{attentionLabel\(attention\)\}`\}/)
  assert.match(source, /<span className="space-label">\{space\.name\}<\/span><AttentionMarks attention=\{attention\} \/>/)
  assert.match(source, /<span className="space-marks" aria-hidden="true">/)
})

test('the degraded section keeps an honest status and the live announcement stays polite', () => {
  assert.match(source, /<div className="spaces-degraded" role="status"/)
  assert.match(source, /role="status" aria-live="polite">\{props\.announcement\}/)
})

test('the section grows with its rows up to half the rail, then its list scrolls', () => {
  assert.match(styles, /\.spaces-section \{[^}]*max-height: 50%;[^}]*flex: 0 0 auto; flex-direction: column; min-height: 0;/)
  assert.match(styles, /\.spaces-list \{ min-height: 0; flex: 0 1 auto;[^}]*overflow-y: auto/)
  assert.doesNotMatch(styles, /\.spaces-list \{[^}]*max-height/)
  assert.match(styles, /\.space-label \{[^}]*overflow: hidden; text-overflow: ellipsis; white-space: nowrap/)
})

test('the switcher is a listbox that tracks and announces the highlighted option', () => {
  assert.match(switcher, /role="listbox" aria-label="Switch space"/)
  assert.match(switcher, /aria-activedescendant=\{highlighted \? optionID\(highlighted\) : undefined\}/)
  assert.match(switcher, /role="option" id=\{optionID\(space\.id\)\} key=\{space\.id\} aria-selected=\{space\.id === highlighted\}/)
  assert.match(switcher, /role="status" aria-live="assertive">\{current \?/)
  assert.match(switcher, /if \(state\.phase !== 'holding'\) return null/)
})

test('the app mounts the switcher with the controller state and attention', () => {
  assert.match(app, /<SpaceSwitcher state=\{workspace\.spaceSwitcher\.state\} spaces=\{workspace\.spaces\.items\} activeID=\{workspace\.spaces\.activeID\}\s+attention=\{workspace\.spaces\.attention\} onChoose=\{workspace\.spaceSwitcher\.choose\} \/>/)
})

test('the controller derives attention and the MRU switcher from the live workspace', () => {
  assert.match(controller, /useSpaceAttention\(\{ apiRef, revision, board: boardQuery\.data, spaces, activeSpaceID, activeAgents: agentNames \}\)/)
  assert.match(controller, /useSpaceSwitcher\(\{ enabled: Boolean\(spacesRuntime\.store\), spaces, activeSpaceID, switchSpace: switchSpaceFocusing \}\)/)
  assert.match(controller, /attention: spaceAttention\.attention/)
  assert.match(controller, /spaceSwitcher,/)
})

test('the attention hook prunes, seeds weakly after the first pull, and reads after the dwell through the shared store', () => {
  assert.match(attentionHook, /const open = Object\.values\(openBySpace\)\.flat\(\)/)
  assert.match(attentionHook, /const keep = markerKeepSet\(board, open\)\s+if \(keep\) store\.prune\(keep\)/)
  assert.match(attentionHook, /if \(pulled\) store\.apply\(seedUpdates\(store\.markers\(\), board, open\), \{ weak: true \}\)/)
  assert.match(attentionHook, /const dwelled = dwelledAgents\(viewing, Date\.now\(\)\)/)
  assert.match(attentionHook, /store\.apply\(readUpdates\(\{ markers: store\.markers\(\), board, viewed: dwelled, positions, armed, now: Date\.now\(\) \}\)\)/)
  assert.match(attentionHook, /nextArmed\(previous, markers, viewedKey\.split\('\\n'\)\.filter\(Boolean\), dockReady\)/)
  assert.doesNotMatch(attentionHook, /localStorage\.setItem|writeReadMarkers/, 'markers persist only through the read-markers store')
  assert.match(attentionHook, /group\.api\.isVisible/)
  assert.match(attentionHook, /useDOMEvent\(document, 'visibilitychange'/)
  assert.match(attentionHook, /storedSpaceAgents\(localStorage, space\.id\)/)
})

test('every switch, whatever started it, touches the MRU order, which lives beside the switcher', () => {
  assert.match(switcherHook, /useEffect\(\(\) => \{\s+if \(activeSpaceID\) setMRU\(\(order\) => touchSpaceMRU\(order, activeSpaceID\)\)\s+\}, \[activeSpaceID\]\)/)
  assert.match(switcherHook, /writeSpaceMRU\(localStorage, mru, mruState\.current\)/)
  assert.match(switcherHook, /mruSpaceIDs\(mru, spaces, activeSpaceID\)/)
  assert.doesNotMatch(attentionHook, /MRU|mru/)
})

test('the switcher scrolls the highlighted option into view on open and on every step', () => {
  assert.match(switcher, /const shownID = state\.phase === 'holding' \? state\.order\[state\.index\] : undefined/)
  assert.match(switcher, /if \(shownID\) document\.getElementById\(optionID\(shownID\)\)\?\.scrollIntoView\(\{ block: 'nearest' \}\)\s+\}, \[shownID\]\)/)
  assert.ok(switcher.indexOf('useEffect(') < switcher.indexOf('return null'), 'the hook runs before the hidden early return')
})

test('the switcher hook binds through the shell shortcut layer and commits through switchSpace', () => {
  assert.match(switcherHook, /bindSpaceSwitcher\(window,/)
  assert.match(switcherHook, /if \(result\.commit\) switchSpace\(result\.commit\)/)
  assert.doesNotMatch(switcherHook, /setTimeout|RevealDelay/, 'the list opens on the first keydown')
})

test('the bottom SpaceStrip and its overflow model are gone', () => {
  assert.equal(existsSync(new URL('../src/features/spaces/SpaceStrip.tsx', import.meta.url)), false)
  assert.equal(existsSync(new URL('../src/features/spaces/spaceOverflowModel.ts', import.meta.url)), false)
  const spaces = readdirSync(new URL('../src/features/spaces/', import.meta.url))
    .map((name) => readFileSync(new URL(`../src/features/spaces/${name}`, import.meta.url), 'utf8')).join('\n')
  assert.doesNotMatch(`${app}\n${controller}\n${spaces}`, /SpaceStrip|spaceOverflow|workspace-switcher-slot/)
  assert.doesNotMatch(styles, /\.space-strip|\.space-chip|\.space-overflow|\.space-more|\.space-measure-rack|\.workspace-switcher-slot/)
})

test('a space row opens "Mark all read" from its context menu only while it has unread', () => {
  assert.match(source, /if \(spaceMenuItems\(attentionOf\(space\.id\)\)\.length === 0\) return\n\s+event\.preventDefault\(\)/)
  assert.match(source, /onContextMenu=\{\(event\) => openSpaceMenu\(space,/)
  assert.match(source, /event\.key === 'ContextMenu' \|\| event\.key === 'F10' && event\.shiftKey/)
  assert.match(source, /props\.markAllRead\(menuSpace\)/)
  assert.match(source, /const spaceMenu = usePositionedMenu\(\)/)
  assert.match(controller, /markAllRead: spaceAttention\.markSpaceRead,/)
  assert.match(attentionHook, /const markSpaceRead = useCallback\(\(spaceID: string\) => markRead\(attention\[spaceID\]\?\.unread \?\? \[\]\)/)
  assert.match(attentionHook, /if \(agentUnread\(findAgentRow\(queryClient\.getQueryData<Board>\(queryKeys\.fleet\), name\), store\.markers\(\)\[name\]\)\) markRead\(\[name\]\)\n\s+else markUnread\(name\)/, '⌥U toggles')
  // markRead and toggleRead read the board from the cache, so a fleet refresh
  // leaves them, and every panel callback built on them, unchanged.
  assert.match(attentionHook, /\}, \[queryClient, store\]\)\n\n\s+\/\/ toggleRead/)
  assert.match(attentionHook, /\}, \[markRead, markUnread, queryClient, store\]\)/)
})
