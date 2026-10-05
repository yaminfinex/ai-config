import assert from 'node:assert/strict'
import test from 'node:test'
import { fileImageURL, imageFailureReason } from '../src/api/client.ts'
import { initialFileViewMode } from '../src/features/files/fileTabs.ts'
import { binaryDetail, fileBodyKind, imageDimensions, imageFailureText, imageLoadFor, svgRenderable } from '../src/features/files/imageViewModel.ts'

const raster = { binary: true, imageMime: 'image/png', revision: false, viewMode: 'source' as const, imageFailed: false }

test('a raster image the server sniffed renders as an image in either view mode', () => {
  assert.equal(fileBodyKind(raster), 'image')
  assert.equal(fileBodyKind({ ...raster, viewMode: 'rendered' }), 'image')
})

test('only the server image_mime makes an image, never a binary file or the extension', () => {
  assert.equal(fileBodyKind({ ...raster, imageMime: undefined }), 'binary')
  assert.equal(fileBodyKind({ ...raster, imageMime: '' }), 'binary')
  assert.equal(fileBodyKind({ ...raster, binary: false, imageMime: undefined, viewMode: 'rendered' }), 'text')
})

test('historical revisions keep the Binary card', () => {
  assert.equal(fileBodyKind({ ...raster, revision: true }), 'binary')
  assert.equal(svgRenderable(false, 'image/svg+xml', true), false)
})

test('a failed image load falls back to the Binary card, or to source for SVG', () => {
  assert.equal(fileBodyKind({ ...raster, imageFailed: true }), 'binary')
  assert.equal(fileBodyKind({ binary: false, imageMime: 'image/svg+xml', revision: false, viewMode: 'rendered', imageFailed: true }), 'text')
})

test('an SVG is an image in Rendered mode and text in Source mode', () => {
  const svg = { binary: false, imageMime: 'image/svg+xml', revision: false, imageFailed: false }
  assert.equal(svgRenderable(false, 'image/svg+xml', false), true)
  assert.equal(svgRenderable(true, 'image/png', false), false)
  assert.equal(fileBodyKind({ ...svg, viewMode: 'rendered' }), 'image')
  assert.equal(fileBodyKind({ ...svg, viewMode: 'source' }), 'text')
  assert.equal(initialFileViewMode({ root: '/r', path: 'icon.SVG' }), 'rendered')
  assert.equal(initialFileViewMode({ root: '/r', path: 'icon.svg', line: 3 }), 'source')
  assert.equal(initialFileViewMode({ root: '/r', path: 'shot.png' }), 'source')
})

test('image load state resets when the source changes and reports dimensions', () => {
  const loaded = { src: 'a', status: 'loaded' as const, width: 1920, height: 1080 }
  assert.deepEqual(imageLoadFor(loaded, 'a'), loaded)
  assert.deepEqual(imageLoadFor(loaded, 'b'), { src: 'b', status: 'loading' })
  assert.deepEqual(imageLoadFor(null, 'a'), { src: 'a', status: 'loading' })
  assert.equal(imageDimensions(loaded), '1,920 × 1,080 px')
  assert.equal(imageDimensions({ src: 'a', status: 'loading' }), null)
})

test('the Binary fallback carries the load failure reason', () => {
  assert.equal(binaryDetail('10 bytes', null), 'No text content is available for this 10 bytes file.')
  assert.equal(binaryDetail('10 bytes', { src: 'a', status: 'loading' }), 'No text content is available for this 10 bytes file.')
  assert.equal(binaryDetail('10 bytes', { src: 'a', status: 'error', reason: null }), 'The image could not be shown. No text content is available for this 10 bytes file.')
  assert.equal(imageFailureText({ src: 'a', status: 'error', reason: 'not found: gone' }), 'The image could not be shown: not found: gone.')
})

test('image URLs encode root, path, and the read version', () => {
  assert.equal(fileImageURL('/a b', 'x/y.png', '2026-10-05T00:00:00Z'), '/api/files/image?root=%2Fa+b&path=x%2Fy.png&v=2026-10-05T00%3A00%3A00Z')
})

test('image failure reasons name the refusal, a decode failure, or a fetch failure', async () => {
  const refused = (async () => new Response(JSON.stringify({ error: 'refused by substrate', detail: 'not an allowed image type' }), { status: 409 })) as typeof fetch
  assert.equal(await imageFailureReason('/u', 'image/avif', refused), 'refused by substrate: not an allowed image type')
  const ok = (async () => new Response('bytes', { status: 200 })) as typeof fetch
  assert.equal(await imageFailureReason('/u', 'image/avif', ok), 'this browser could not decode the image/avif image')
  const offline = (async () => { throw new Error('offline') }) as typeof fetch
  assert.equal(await imageFailureReason('/u', 'image/png', offline), 'the image could not be fetched (offline)')
})
