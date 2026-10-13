import type { GitHubLocation, GitHubRead } from '../../types'

export type GitHubLines = { start: number, end?: number }
export type GitHubLink = { url: string, title: string, copyTitle: string }

// GitHub renders these by default; a line anchor only resolves in the plain view.
const renderedByGitHub = /\.(?:md|markdown|mdown|mkdn|mkd|mdx|rst|adoc|asciidoc|org|textile|rdoc|ipynb|svg|csv|tsv|geojson|topojson|stl)$/iu

function encodedSegments(path: string) {
  return path.split('/').filter(Boolean).map(encodeURIComponent).join('/')
}

function lineAnchor(lines?: GitHubLines | null) {
  if (!lines || !Number.isInteger(lines.start) || lines.start < 1) return ''
  const end = lines.end && lines.end > lines.start ? lines.end : undefined
  return end ? `#L${lines.start}-L${end}` : `#L${lines.start}`
}

export function githubURL(location: GitHubLocation, lines?: GitHubLines | null) {
  const repo = `https://github.com/${encodeURIComponent(location.owner)}/${encodeURIComponent(location.repo)}`
  const path = encodedSegments(location.path)
  const base = `${repo}/${location.kind}/${encodeURIComponent(location.sha)}${path ? `/${path}` : ''}`
  if (location.kind !== 'blob') return base
  const anchor = lineAnchor(lines)
  return anchor && renderedByGitHub.test(location.path) ? `${base}?plain=1${anchor}` : `${base}${anchor}`
}

export function githubLinkTitle(location: GitHubLocation) {
  const short = location.sha.slice(0, 12)
  const where = `${location.owner}/${location.repo}`
  if (!location.at_head) {
    const local = location.differs ? 'your local copy differs' : 'your local copy matches it'
    return `Links to ${location.ref} (${short}) on ${where}: this checkout's commit is not on GitHub; ${local}.`
  }
  if (location.uncommitted) return `Links to your last commit (${short}) on ${where}; your local copy has uncommitted changes.`
  return `Open on GitHub: ${where} at ${short}.`
}

// githubLink is the link a header shows, or null when the server proved no
// GitHub location. Nothing is ever guessed from a path or a remote name.
export function githubLink(read: GitHubRead | undefined, lines?: GitHubLines | null): GitHubLink | null {
  const location = read?.github
  if (!location || !location.sha || !location.owner || !location.repo) return null
  if (location.kind !== 'blob' && location.kind !== 'tree') return null
  const url = githubURL(location, lines)
  return { url, title: githubLinkTitle(location), copyTitle: `Copy ${url}` }
}
