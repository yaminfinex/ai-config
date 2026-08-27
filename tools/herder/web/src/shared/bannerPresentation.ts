export type BannerTone = 'info' | 'error'

export function bannerSemantics(tone: BannerTone) {
  return tone === 'info'
    ? { className: 'banner info', role: 'status' as const, dismissible: false }
    : { className: 'banner error', role: 'alert' as const, dismissible: true }
}
