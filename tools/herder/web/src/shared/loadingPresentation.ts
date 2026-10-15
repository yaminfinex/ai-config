import type { BannerTone } from './bannerPresentation.ts'

export type LoadingNotice = { tone: BannerTone, detail: string }

export function transcriptNotice(pending: boolean, errorDetail: string): LoadingNotice | null {
  if (pending) return { tone: 'info', detail: 'Loading transcript…' }
  return errorDetail ? { tone: 'error', detail: errorDetail } : null
}

