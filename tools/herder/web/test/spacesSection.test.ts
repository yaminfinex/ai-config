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
  assert.doesNotMatch(source, /clientX/)
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

test('the rows scroll within a compact max height so the fleet tree keeps the rail', () => {
  assert.match(styles, /\.spaces-list \{[^}]*max-height: 176px;[^}]*overflow-y: auto/)
  assert.match(styles, /\.space-label \{[^}]*overflow: hidden; text-overflow: ellipsis; white-space: nowrap/)
})

test('the switcher is a listbox that tracks and announces the highlighted option', () => {
  assert.match(switcher, /role="listbox" aria-label="Switch space"/)
  assert.match(switcher, /aria-activedescendant=\{highlighted \? optionID\(highlighted\) : undefined\}/)
  assert.match(switcher, /role="option" id=\{optionID\(space\.id\)\} key=\{space\.id\} aria-selected=\{space\.id === highlighted\}/)
  assert.match(switcher, /role="status" aria-live="assertive">\{current \?/)
  assert.match(switcher, /if \(state\.phase !== 'holding' \|\| !state\.shown\) return null/)
})

test('the app mounts the switcher with the controller state and attention', () => {
  assert.match(app, /<SpaceSwitcher state=\{workspace\.spaceSwitcher\.state\} spaces=\{workspace\.spaces\.items\} activeID=\{workspace\.spaces\.activeID\}\s+attention=\{workspace\.spaces\.attention\} onChoose=\{workspace\.spaceSwitcher\.choose\} \/>/)
})

test('the controller derives attention and the MRU switcher from the live workspace', () => {
  assert.match(controller, /useSpaceAttention\(\{ apiRef, revision, board: boardQuery\.data, spaces, activeSpaceID, activeAgents: agentNames \}\)/)
  assert.match(controller, /useSpaceSwitcher\(\{ enabled: Boolean\(spacesRuntime\.store\), mruOrder: spaceAttention\.mruOrder, switchSpace \}\)/)
  assert.match(controller, /attention: spaceAttention\.attention/)
  assert.match(controller, /spaceSwitcher,/)
})

test('the attention hook seeds, marks read after the dwell and persists through the versioned stores', () => {
  assert.match(attentionHook, /seedReadMarkers\(current, board, Object\.values\(openBySpace\)\.flat\(\)\)/)
  assert.match(attentionHook, /markViewedRead\(seeded, board, dwelledAgents\(viewing, Date\.now\(\)\)\)/)
  assert.match(attentionHook, /writeReadMarkers\(localStorage, markers, markerState\.current\)/)
  assert.match(attentionHook, /group\.api\.isVisible/)
  assert.match(attentionHook, /useDOMEvent\(document, 'visibilitychange'/)
  assert.match(attentionHook, /storedSpaceAgents\(localStorage, space\.id\)/)
})

test('every switch, whatever started it, touches the MRU order', () => {
  assert.match(attentionHook, /useEffect\(\(\) => \{\s+if \(activeSpaceID\) setMRU\(\(order\) => touchSpaceMRU\(order, activeSpaceID\)\)\s+\}, \[activeSpaceID\]\)/)
  assert.match(attentionHook, /writeSpaceMRU\(localStorage, mru, mruState\.current\)/)
})

test('the switcher hook binds through the shell shortcut layer and commits through switchSpace', () => {
  assert.match(switcherHook, /bindSpaceSwitcher\(window,/)
  assert.match(switcherHook, /if \(result\.commit\) switchSpace\(result\.commit\)/)
  assert.match(switcherHook, /switcherRevealDelayMs/)
})

test('the bottom SpaceStrip and its overflow model are gone', () => {
  assert.equal(existsSync(new URL('../src/features/spaces/SpaceStrip.tsx', import.meta.url)), false)
  assert.equal(existsSync(new URL('../src/features/spaces/spaceOverflowModel.ts', import.meta.url)), false)
  const spaces = readdirSync(new URL('../src/features/spaces/', import.meta.url))
    .map((name) => readFileSync(new URL(`../src/features/spaces/${name}`, import.meta.url), 'utf8')).join('\n')
  assert.doesNotMatch(`${app}\n${controller}\n${spaces}`, /SpaceStrip|spaceOverflow|workspace-switcher-slot/)
  assert.doesNotMatch(styles, /\.space-strip|\.space-chip|\.space-overflow|\.space-more|\.space-measure-rack|\.workspace-switcher-slot/)
})
