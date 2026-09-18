import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { DiagramOverlay, centredView, coverApplication, fitScale, overlayAction, resizedView, wheelFactor, zoomAbout } from '../src/shared/DiagramOverlay.ts'

const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('fitScale fits the whole diagram into the viewport with a margin, upscales small ones, and stays within the zoom bounds', () => {
  // wide: width is the limit
  assert.equal(fitScale({ width: 4000, height: 100 }, { width: 1048, height: 800 }), 0.25)
  // tall: height is the limit
  assert.equal(fitScale({ width: 100, height: 2000 }, { width: 1048, height: 824 }), (824 - 48) / 2000)
  // small: fit means larger than natural size
  assert.equal(fitScale({ width: 100, height: 100 }, { width: 448, height: 1000 }), 4)
  // tiny: capped at the upper bound; huge: fit always fits, even below the 0.1 zoom floor
  assert.equal(fitScale({ width: 10, height: 10 }, { width: 1048, height: 1048 }), 8)
  assert.equal(fitScale({ width: 100000, height: 10 }, { width: 1048, height: 1048 }), 0.01)
  // the reviewer's case: the 40-node flowchart (6402 px with the box) at 600×500 fits at 0.086, inside the viewport
  const forty = { width: 6402, height: 88 }
  const small = { width: 600, height: 500 }
  const fitted = centredView(forty, small, fitScale(forty, small))
  assert.ok(fitted.x >= 0 && fitted.x + forty.width * fitted.scale <= 600, JSON.stringify(fitted))
  // nothing measured yet: natural size
  assert.equal(fitScale({ width: 0, height: 0 }, { width: 1048, height: 1048 }), 1)
  assert.equal(fitScale({ width: 400, height: 100 }, { width: 400, height: 100 }, 0), 1)
})

test('centredView centres the diagram at a clamped scale; resizedView refits only a fitted view', () => {
  assert.equal(centredView({ width: 1, height: 1 }, { width: 1, height: 1 }, 0.01).scale, 0.01)
  assert.equal(centredView({ width: 1, height: 1 }, { width: 1, height: 1 }, 9).scale, 8)
  const content = { width: 4000, height: 100 }
  const fitted = centredView(content, { width: 1440, height: 1000 }, fitScale(content, { width: 1440, height: 1000 }))
  // the window shrinks: a fitted view is fitted to the new viewport
  assert.deepEqual(resizedView(fitted, true, content, { width: 600, height: 500 }), centredView(content, { width: 600, height: 500 }, 0.138))
  assert.equal(resizedView(fitted, true, content, { width: 600, height: 500 }).scale, (600 - 48) / 4000)
  // a browsed view (zoomed or panned) is left exactly where it is
  const browsed = { x: -9048.68, y: 365.7, scale: 3.05 }
  assert.equal(resizedView(browsed, false, content, { width: 600, height: 500 }), browsed)
  assert.deepEqual(centredView({ width: 400, height: 200 }, { width: 1000, height: 600 }, 1), { x: 300, y: 200, scale: 1 })
  assert.deepEqual(centredView({ width: 400, height: 200 }, { width: 1000, height: 600 }, 2), { x: 100, y: 100, scale: 2 })
  assert.deepEqual(centredView({ width: 400, height: 200 }, { width: 1000, height: 600 }, 100), { x: -1100, y: -500, scale: 8 })
})

test('zoomAbout keeps the content under the pointer fixed; wheelFactor maps deltas to bounded factors', () => {
  const view = { x: 100, y: 50, scale: 1 }
  // the content point under (300, 250) is (200, 200); after zooming ×2 it must still be under (300, 250)
  const zoomed = zoomAbout(view, 2, { x: 300, y: 250 })
  assert.deepEqual(zoomed, { x: -100, y: -150, scale: 2 })
  assert.equal(zoomed.x + 200 * zoomed.scale, 300)
  assert.equal(zoomed.y + 200 * zoomed.scale, 250)
  // zooming about the content origin never moves it
  assert.deepEqual(zoomAbout({ x: 40, y: 40, scale: 1 }, 0.5, { x: 40, y: 40 }), { x: 40, y: 40, scale: 0.5 })
  // the scale is clamped before the translation is derived
  assert.equal(zoomAbout(view, 50, { x: 0, y: 0 }).scale, 8)
  assert.equal(zoomAbout(view, 0, { x: 0, y: 0 }).scale, 0.1)
  // from a fit below the floor the user can only zoom in
  assert.equal(zoomAbout({ x: 0, y: 0, scale: 0.05 }, 0.04, { x: 0, y: 0 }).scale, 0.05)
  assert.equal(zoomAbout({ x: 0, y: 0, scale: 0.05 }, 0.0625, { x: 0, y: 0 }).scale, 0.0625)
  // wheel: down zooms out, up zooms in, lines are converted, big deltas are capped
  assert.ok(wheelFactor(100, 0) < 1 && wheelFactor(100, 0) > 0.7)
  assert.ok(wheelFactor(-100, 0) > 1 && wheelFactor(-100, 0) < 1.3)
  assert.equal(wheelFactor(3, 1), wheelFactor(48, 0))
  assert.equal(wheelFactor(10000, 0), wheelFactor(200, 0))
  assert.equal(wheelFactor(0, 0), 1)
})

test('overlayAction maps the keyboard: +/- zoom, 0 fit, 1 natural, arrows pan, Escape closes, anything else is ignored', () => {
  assert.equal(overlayAction('+'), 'in')
  assert.equal(overlayAction('='), 'in')
  assert.equal(overlayAction('-'), 'out')
  assert.equal(overlayAction('_'), 'out')
  assert.equal(overlayAction('0'), 'fit')
  assert.equal(overlayAction('1'), 'natural')
  assert.equal(overlayAction('Escape'), 'close')
  assert.deepEqual(overlayAction('ArrowLeft'), { pan: { x: 40, y: 0 } })
  assert.deepEqual(overlayAction('ArrowRight'), { pan: { x: -40, y: 0 } })
  assert.deepEqual(overlayAction('ArrowUp'), { pan: { x: 0, y: 40 } })
  assert.deepEqual(overlayAction('ArrowDown'), { pan: { x: 0, y: -40 } })
  assert.equal(overlayAction('a'), undefined)
  assert.equal(overlayAction('Tab'), undefined)
  assert.equal(overlayAction('Enter'), undefined)
})

test('DiagramOverlay is a modal dialog holding the given SVG, a toolbar, and the zoom text; every control goes through one dispatcher', () => {
  let closed = 0
  const html = renderToStaticMarkup(createElement(DiagramOverlay, { svg: '<svg id="d"><g/></svg>', onClose: () => { closed += 1 } }))
  assert.match(html, /^<div class="diagram-overlay" role="dialog" aria-modal="true" aria-label="Diagram" tabindex="-1">/)
  assert.match(html, /<div class="diagram-overlay-toolbar" role="toolbar" aria-label="Diagram zoom"><button type="button" aria-label="Zoom out">−<\/button><span class="diagram-overlay-zoom" aria-live="polite">100 %<\/span><button type="button" aria-label="Zoom in">\+<\/button><button type="button" aria-label="Fit diagram to the window">fit<\/button><button type="button" aria-label="Show natural size">100 %<\/button><button type="button" aria-label="Close diagram">×<\/button><\/div>/)
  assert.match(html, /<div class="diagram-overlay-viewport"><div class="diagram-overlay-content" style="transform:translate\(0px, 0px\) scale\(1\)"><svg id="d"><g\/><\/svg><\/div><\/div><\/div>$/)
  assert.equal(closed, 0)
  const component = read('../src/shared/DiagramOverlay.ts')
  // one dispatcher: the keyboard map, the toolbar buttons, the wheel and the drag all call apply(action)
  assert.match(component, /const apply = \(action: OverlayAction\) => \{/)
  assert.match(component, /const action = overlayAction\(event\.key\)[\s\S]*?apply\(action\)/)
  assert.match(component, /onClick: \(\) => apply\(action\)/)
  assert.match(component, /if \(action === 'close'\) onClose\(\)/)
  assert.match(component, /button\('×', 'Close diagram', 'close'\)/)
  // a click that began on the backdrop closes; the captured pointerup target is never consulted
  assert.match(component, /outside: !content\.current\?\.contains\(event\.target as Node\)/)
  assert.match(component, /if \(start && start\.outside && !start\.moved\) onClose\(\)/)
  assert.doesNotMatch(component, /onPointerUp = \(event/)
  // containment: focus moves in, Tab wraps inside, the covered application is inert, the page behind does not scroll
  assert.match(component, /root\.current\?\.focus\(\)/)
  assert.match(component, /dialogTabTargetIndex\(items\.indexOf\(document\.activeElement as HTMLElement\), items\.length, event\.shiftKey\)/)
  assert.match(component, /const uncover = coverApplication\(document\)[\s\S]*?return \(\) => \{\s*uncover\(\)/)
  assert.doesNotMatch(component, /document\.body\.children/, 'only the application is covered, never every body child')
  assert.match(component, /document\.body\.style\.overflow = 'hidden'/)
  assert.match(component, /addEventListener\('wheel', onWheel, \{ passive: false \}\)/)
  // fit on open; a fitted view follows the viewport size, any zoom or pan stops that
  assert.match(component, /useLayoutEffect\(\(\) => \{ apply\('fit'\) \}, \[svg\]\)/, 'opening state is fit')
  assert.match(component, /fitted\.current = action === 'fit'/)
  assert.match(component, /new ResizeObserver\(\(\) => \{ const measured = sizes\(\); setView\(\(current\) => resizedView\(current, fitted\.current, measured\.content, measured\.viewport\)\) \}\)/)
  // only the tested helpers are exported; constants and clamp stay private
  assert.deepEqual([...component.matchAll(/^export (?:function|const|type) (\w+)/gmu)].map((match) => match[1]).sort(), ['DiagramOverlay', 'OverlayAction', 'Point', 'Size', 'View', 'centredView', 'coverApplication', 'fitScale', 'overlayAction', 'resizedView', 'wheelFactor', 'zoomAbout'])
  assert.doesNotMatch(component, /from '(?!react|\.\.\/features\/launch\/launchModel)/u, 'no library: react and the existing dialog tab helper only')
})

test('overlay CSS: fixed full-viewport above the launch dialog, blurred subtle backdrop, transform on the content wrapper', () => {
  const css = read('../src/styles.css')
  assert.match(css, /\.launch-agent-backdrop \{ position: fixed; z-index: 80;/)
  assert.match(css, /\.diagram-overlay \{ position: fixed; z-index: 90; inset: 0; background: var\(--overlay-subtle\); backdrop-filter: blur\(2px\);/)
  assert.match(css, /\.diagram-overlay-viewport \{ position: absolute; inset: 0; overflow: hidden; cursor: grab; touch-action: none;/)
  assert.match(css, /\.diagram-overlay-content \{ position: absolute; top: 0; left: 0;[^}]*transform-origin: 0 0; \}/)
  assert.match(css, /\.diagram-overlay-content svg \{ display: block; height: auto; \}/)
  // Only the quick-open palette (a body-level layer the user may summon while reading a diagram) sits above the overlay.
  assert.match(css, /\.quick-open-backdrop \{ position: fixed; z-index: 100; inset: 0;/)
  const zIndexes = [...css.matchAll(/z-index: (\d+)/gu)].map((match) => Number(match[1]))
  assert.deepEqual(zIndexes.filter((z) => z > 90), [100], 'nothing but the palette sits above the diagram overlay')
})

test('coverApplication makes only #root inert on open; a sibling body child (the palette) stays interactive; close undoes exactly that', () => {
  const element = (inert = false) => {
    const attributes = new Map<string, string>(inert ? [['inert', '']] : [])
    return { attributes, hasAttribute: (name: string) => attributes.has(name), setAttribute: (name: string, value: string) => { attributes.set(name, value) }, removeAttribute: (name: string) => { attributes.delete(name) } }
  }
  const root = element()
  const palette = element()
  const body = new Map([['root', root]])
  const doc = { getElementById: (id: string) => (body.get(id) ?? null) as unknown as HTMLElement | null }
  const uncover = coverApplication(doc)
  assert.equal(root.hasAttribute('inert'), true, '#root is inert while the overlay is open')
  assert.equal(palette.hasAttribute('inert'), false, 'a later body child is never inert')
  uncover()
  assert.equal(root.hasAttribute('inert'), false, 'close restores the application')
  assert.equal(palette.hasAttribute('inert'), false)
  // a #root someone else made inert is left alone, on open and on close
  const alreadyInert = element(true)
  coverApplication({ getElementById: () => alreadyInert as unknown as HTMLElement })()
  assert.equal(alreadyInert.hasAttribute('inert'), true)
  // no #root (a test page): nothing to do, nothing thrown
  assert.doesNotThrow(() => coverApplication({ getElementById: () => null })())
})
