import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { DiagramOverlay, centredView, clamp, clampScale, fitPadding, fitScale, overlayAction, panStep, wheelFactor, zoomAbout, zoomBounds, zoomStep } from '../src/shared/DiagramOverlay.ts'

const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('fitScale fits the whole diagram into the viewport with a margin, upscales small ones, and stays within the zoom bounds', () => {
  assert.deepEqual(zoomBounds, { min: 0.1, max: 8 })
  assert.equal(zoomStep, 1.25)
  assert.equal(fitPadding, 24)
  // wide: width is the limit
  assert.equal(fitScale({ width: 4000, height: 100 }, { width: 1048, height: 800 }), 0.25)
  // tall: height is the limit
  assert.equal(fitScale({ width: 100, height: 2000 }, { width: 1048, height: 824 }), (824 - 48) / 2000)
  // small: fit means larger than natural size
  assert.equal(fitScale({ width: 100, height: 100 }, { width: 448, height: 1000 }), 4)
  // tiny: capped at the upper bound; huge: capped at the lower bound
  assert.equal(fitScale({ width: 10, height: 10 }, { width: 1048, height: 1048 }), 8)
  assert.equal(fitScale({ width: 100000, height: 10 }, { width: 1048, height: 1048 }), 0.1)
  // nothing measured yet: natural size
  assert.equal(fitScale({ width: 0, height: 0 }, { width: 1048, height: 1048 }), 1)
  assert.equal(fitScale({ width: 400, height: 100 }, { width: 400, height: 100 }, 0), 1)
})

test('clamp and clampScale bound a value; centredView centres the diagram at a clamped scale', () => {
  assert.equal(clamp(5, 0, 10), 5)
  assert.equal(clamp(-1, 0, 10), 0)
  assert.equal(clamp(11, 0, 10), 10)
  assert.equal(clampScale(0.01), 0.1)
  assert.equal(clampScale(9), 8)
  assert.equal(clampScale(3), 3)
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
  assert.deepEqual(overlayAction('ArrowLeft'), { x: panStep, y: 0 })
  assert.deepEqual(overlayAction('ArrowRight'), { x: -panStep, y: 0 })
  assert.deepEqual(overlayAction('ArrowUp'), { x: 0, y: panStep })
  assert.deepEqual(overlayAction('ArrowDown'), { x: 0, y: -panStep })
  assert.equal(overlayAction('a'), undefined)
  assert.equal(overlayAction('Tab'), undefined)
  assert.equal(overlayAction('Enter'), undefined)
})

test('DiagramOverlay is a modal dialog holding the given SVG, a toolbar, and the zoom text; Escape and the close button call onClose', () => {
  let closed = 0
  const html = renderToStaticMarkup(createElement(DiagramOverlay, { svg: '<svg id="d"><g/></svg>', onClose: () => { closed += 1 } }))
  assert.match(html, /^<div class="diagram-overlay" role="dialog" aria-modal="true" aria-label="Diagram" tabindex="-1">/)
  assert.match(html, /<div class="diagram-overlay-toolbar" role="toolbar" aria-label="Diagram zoom"><button type="button" aria-label="Zoom out">−<\/button><span class="diagram-overlay-zoom" aria-live="polite">100 %<\/span><button type="button" aria-label="Zoom in">\+<\/button><button type="button" aria-label="Fit diagram to the window">fit<\/button><button type="button" aria-label="Show natural size">100 %<\/button><button type="button" aria-label="Close diagram">×<\/button><\/div>/)
  assert.match(html, /<div class="diagram-overlay-viewport"><div class="diagram-overlay-content" style="transform:translate\(0px, 0px\) scale\(1\)"><svg id="d"><g\/><\/svg><\/div><\/div><\/div>$/)
  assert.equal(closed, 0)
  const component = read('../src/shared/DiagramOverlay.ts')
  // the key handler closes on Escape through the pure map, and the close button calls onClose directly
  assert.match(component, /const action = overlayAction\(event\.key\)/)
  assert.match(component, /if \(action === 'close'\) onClose\(\)/)
  assert.match(component, /button\('×', 'Close diagram', onClose\)/)
  // a plain click on the backdrop closes; a drag pans
  assert.match(component, /if \(start && !start\.moved && !content\.current\?\.contains\(event\.target as Node\)\) onClose\(\)/)
  // focus moves in on open; the page behind does not scroll; wheel is a native non-passive listener
  assert.match(component, /root\.current\?\.focus\(\)/)
  assert.match(component, /document\.body\.style\.overflow = 'hidden'/)
  assert.match(component, /addEventListener\('wheel', onWheel, \{ passive: false \}\)/)
  assert.match(component, /useLayoutEffect\(\(\) => \{ show\('fit'\) \}, \[svg\]\)/, 'opening state is fit')
  assert.doesNotMatch(component, /from '(?!react)/u, 'no library: react only')
})

test('overlay CSS: fixed full-viewport above the launch dialog, blurred subtle backdrop, transform on the content wrapper', () => {
  const css = read('../src/styles.css')
  assert.match(css, /\.launch-agent-backdrop \{ position: fixed; z-index: 80;/)
  assert.match(css, /\.diagram-overlay \{ position: fixed; z-index: 90; inset: 0; background: var\(--overlay-subtle\); backdrop-filter: blur\(2px\);/)
  assert.match(css, /\.diagram-overlay-viewport \{ position: absolute; inset: 0; overflow: hidden; cursor: grab; touch-action: none;/)
  assert.match(css, /\.diagram-overlay-content \{ position: absolute; top: 0; left: 0;[^}]*transform-origin: 0 0; \}/)
  assert.match(css, /\.diagram-overlay-content svg \{ display: block; height: auto; \}/)
  const zIndexes = [...css.matchAll(/z-index: (\d+)/gu)].map((match) => Number(match[1]))
  assert.equal(Math.max(...zIndexes), 90, 'nothing sits above the diagram overlay')
})
