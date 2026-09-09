// Keep URL schemes out of filesystem lookup; decode only at the resolver boundary.
export function pathFromHref(href: string): string | null {
  if (!href || (/^[a-z][a-z\d+.-]*:/iu.test(href) && !/^file:/iu.test(href))) return null
  const path = href.replace(/^file:(?:\/\/)?/iu, '')
  try {
    return decodeURIComponent(path)
  } catch {
    return path
  }
}
