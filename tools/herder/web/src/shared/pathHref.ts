const hasScheme = /^[a-z][a-z\d+.-]*:/iu

// Markdown classifies the original href without decoding its displayed title.
export function isLocalHref(href: string): boolean {
  return Boolean(href) && !href.startsWith('//') && (!hasScheme.test(href) || /^file:/iu.test(href))
}

// Filesystem lookup decodes a local href once and removes its file: prefix.
export function pathFromHref(href: string): string | null {
  if (!isLocalHref(href)) return null
  const path = href.replace(/^file:(?:\/\/)?/iu, '')
  try {
    return decodeURIComponent(path)
  } catch {
    return path
  }
}
