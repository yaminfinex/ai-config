export type HtmlRawState = 'unavailable' | 'loading' | 'success' | 'error'

export type HtmlPreviewModel = {
  srcdocSource: 'content' | 'raw'
  renderedEnabled: boolean
  banner: string | null
}

export function htmlPreviewModel(isHtml: boolean, truncated: boolean, rawState: HtmlRawState, size: string): HtmlPreviewModel {
  if (!isHtml || !truncated) {
    return { srcdocSource: 'content', renderedEnabled: true, banner: null }
  }
  if (rawState === 'unavailable') {
    return { srcdocSource: 'content', renderedEnabled: false, banner: null }
  }
  return {
    srcdocSource: 'raw',
    renderedEnabled: true,
    banner: rawState === 'success' ? `Rendered from the full ${size}. The source view shows the first 256 KiB.` : null,
  }
}
