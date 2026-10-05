import type { FileViewMode } from './fileTabs'

export type FileBodyKind = 'image' | 'binary' | 'text'

export type ImageLoad =
  | { src: string, status: 'loading' }
  | { src: string, status: 'loaded', width: number, height: number }
  | { src: string, status: 'error', reason: string | null }

// fileBodyKind decides the file panel body. Only the server's sniffed
// image_mime makes an image, never the extension; historical revisions have
// no image endpoint and keep their Binary card. A raster image always shows
// as an image; an SVG is text, so it shows as an image only in Rendered mode.
// A failed load falls back to what the file would otherwise show.
export function fileBodyKind(input: {
  binary: boolean
  imageMime?: string
  revision: boolean
  viewMode: FileViewMode
  imageFailed: boolean
}): FileBodyKind {
  const image = Boolean(input.imageMime) && !input.revision && !input.imageFailed && (input.binary || input.viewMode === 'rendered')
  if (image) return 'image'
  return input.binary ? 'binary' : 'text'
}

// svgRenderable says whether the Rendered/Source toggle applies: a text file
// the server sniffed as an image (SVG).
export function svgRenderable(binary: boolean, imageMime: string | undefined, revision: boolean) {
  return !binary && Boolean(imageMime) && !revision
}

export function imageLoadFor(load: ImageLoad | null, src: string): ImageLoad {
  return load && load.src === src ? load : { src, status: 'loading' }
}

export function imageDimensions(load: ImageLoad) {
  return load.status === 'loaded' ? `${load.width.toLocaleString()} × ${load.height.toLocaleString()} px` : null
}

export function imageFailureText(failure: ImageLoad | null) {
  if (!failure || failure.status !== 'error') return null
  return `The image could not be shown${failure.reason ? `: ${failure.reason}` : ''}.`
}

export function binaryDetail(size: string, failure: ImageLoad | null) {
  const base = `No text content is available for this ${size} file.`
  const reason = imageFailureText(failure)
  return reason ? `${reason} ${base}` : base
}
