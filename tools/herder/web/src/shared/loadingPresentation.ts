import type { ScreenFrame } from '../types.ts'
import type { BannerTone } from './bannerPresentation.ts'

export type LoadingNotice = { tone: BannerTone, detail: string }

export function transcriptNotice(pending: boolean, errorDetail: string): LoadingNotice | null {
  if (pending) return { tone: 'info', detail: 'Loading transcript…' }
  return errorDetail ? { tone: 'error', detail: errorDetail } : null
}

export function screenNotice(frame: ScreenFrame | undefined): LoadingNotice | null {
  if (!frame) return { tone: 'info', detail: 'Connecting to live pane…' }
  if (frame.status === 'unavailable') return { tone: 'error', detail: frame.detail || 'Pane screen is unavailable' }
  if (frame.truncated) return { tone: 'info', detail: 'Screen exceeds the 64 KiB live-frame budget; this snapshot is truncated.' }
  return null
}
