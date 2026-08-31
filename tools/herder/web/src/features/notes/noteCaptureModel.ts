import type { NoteSource } from './notesStore'

export function captureNoteText(quote: string, comment: string) {
  const selected = quote.trim()
  const aside = comment.trim()
  return aside ? `${selected}\n\n${aside}` : selected
}

export function capturePosition(rect: Pick<DOMRect, 'left' | 'bottom'>, viewportWidth: number, viewportHeight: number) {
  return {
    left: Math.max(8, Math.min(rect.left, viewportWidth - 260)),
    top: Math.max(8, Math.min(rect.bottom + 6, viewportHeight - 328)),
  }
}

export function captureSourceWithRange(source: NoteSource, start?: number, end?: number): NoteSource {
  if (source.kind === 'transcript' || start === undefined || end === undefined) return source
  return { ...source, start: Math.min(start, end), end: Math.max(start, end) }
}
