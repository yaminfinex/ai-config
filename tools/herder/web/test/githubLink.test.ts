import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { githubLink, githubLinkTitle, githubURL } from '../src/features/git/githubLinkModel.ts'
import type { GitHubLocation, GitHubRead } from '../src/types.ts'

const sha = '0123456789abcdef0123456789abcdef01234567'
const location = (overrides: Partial<GitHubLocation> = {}): GitHubLocation => ({
  owner: 'acme', repo: 'widgets', path: 'src/main.go', kind: 'blob', sha, ref: 'HEAD',
  at_head: true, uncommitted: false, differs: false, ...overrides,
})
const read = (github?: GitHubLocation): GitHubRead => ({ root: '/repo', path: 'src/main.go', github, fetched_at: '2026-10-13T09:00:00Z', ...(github ? {} : { reason: 'origin is not a GitHub repository' }) })
const source = (path: string) => readFileSync(new URL(`../src/features/${path}`, import.meta.url), 'utf8')

test('a file links to its blob at the proved commit, with the panel line as an anchor', () => {
  assert.equal(githubURL(location()), `https://github.com/acme/widgets/blob/${sha}/src/main.go`)
  assert.equal(githubURL(location(), { start: 12, end: 12 }), `https://github.com/acme/widgets/blob/${sha}/src/main.go#L12`)
  assert.equal(githubURL(location(), { start: 12, end: 20 }), `https://github.com/acme/widgets/blob/${sha}/src/main.go#L12-L20`)
  assert.equal(githubLink(read(location()), { start: 7, end: 7 })?.url, `https://github.com/acme/widgets/blob/${sha}/src/main.go#L7`)
})

test('a line in a file GitHub renders asks for the plain view so the anchor resolves', () => {
  assert.equal(githubURL(location({ path: 'docs/guide.md' }), { start: 3 }), `https://github.com/acme/widgets/blob/${sha}/docs/guide.md?plain=1#L3`)
  assert.equal(githubURL(location({ path: 'docs/guide.md' })), `https://github.com/acme/widgets/blob/${sha}/docs/guide.md`)
})

test('a folder links to its tree and a repository root to the bare tree; neither carries a line', () => {
  assert.equal(githubURL(location({ kind: 'tree', path: 'tools/herder' }), { start: 4 }), `https://github.com/acme/widgets/tree/${sha}/tools/herder`)
  assert.equal(githubURL(location({ kind: 'tree', path: '' })), `https://github.com/acme/widgets/tree/${sha}`)
})

test('every path segment is percent-encoded and the separators are kept', () => {
  assert.equal(githubURL(location({ path: 'docs/read me #1%.md' })), `https://github.com/acme/widgets/blob/${sha}/docs/read%20me%20%231%25.md`)
  assert.equal(githubURL(location({ kind: 'tree', path: 'a b/c#d/50%' })), `https://github.com/acme/widgets/tree/${sha}/a%20b/c%23d/50%25`)
  assert.equal(githubURL(location({ path: 'q?x=1&y/ü.txt' })), `https://github.com/acme/widgets/blob/${sha}/q%3Fx%3D1%26y/%C3%BC.txt`)
})

test('no GitHub location means no link, never a guessed one', () => {
  assert.equal(githubLink(undefined), null)
  assert.equal(githubLink(read()), null)
  assert.equal(githubLink(read(location({ sha: '' }))), null)
  assert.equal(githubLink(read({ ...location(), kind: 'commit' as 'blob' })), null)
})

test('the tooltip says in plain words when the link is not the local copy', () => {
  assert.equal(githubLinkTitle(location()), 'Open on GitHub: acme/widgets at 0123456789ab.')
  assert.equal(githubLinkTitle(location({ uncommitted: true, differs: true })), 'Links to your last commit (0123456789ab) on acme/widgets; your local copy has uncommitted changes.')
  assert.equal(githubLinkTitle(location({ at_head: false, ref: 'origin/main', differs: true })), "Links to origin/main (0123456789ab) on acme/widgets: this checkout's commit is not on GitHub; your local copy differs.")
  assert.equal(githubLinkTitle(location({ at_head: false, ref: 'origin/main' })), "Links to origin/main (0123456789ab) on acme/widgets: this checkout's commit is not on GitHub; your local copy matches it.")
  assert.equal(githubLink(read(location({ at_head: false, ref: 'origin/main', differs: true })))?.title, githubLinkTitle(location({ at_head: false, ref: 'origin/main', differs: true })))
})

test('the file header shows the GitHub buttons beside Copy path with the target line', () => {
  const panel = source('files/FilePanel.tsx')
  assert.match(panel, /const githubQuery = useGitHubLocation\(target\.root, target\.path\)/)
  assert.match(panel, /<PathCopyButton key=\{absolutePath\} path=\{absolutePath\} \/><GitHubLinkButtons query=\{githubQuery\} lines=\{gitState\.revision \? null : selectedCurrentLines\(target\.line\)\} \/>/)
  assert.match(panel, /const refresh = \(\) => \{\n {4}void statusQuery\.refetch\(\)\n {4}void githubQuery\.refetch\(\)/)
})

test('the folder header shows the GitHub buttons for the directory it is showing', () => {
  const panel = source('folders/FolderPanel.tsx')
  assert.match(panel, /const githubQuery = useGitHubLocation\(target\.root, currentDir\)/)
  assert.match(panel, /<PathCopyButton key=\{absolutePath\} path=\{absolutePath\} \/><GitHubLinkButtons query=\{githubQuery\} \/>/)
})

test('the buttons render nothing without a link, open GitHub in a new tab, and copy the same URL', () => {
  const buttons = source('git/GitHubLinkButtons.tsx')
  assert.match(buttons, /const link = githubLink\(query\.data, lines\)\n {2}if \(!link\) return null/)
  assert.match(buttons, /<a className="path-copy github-open" href=\{link\.url\} target="_blank" rel="noopener noreferrer" title=\{link\.title\} aria-label="Open on GitHub">/)
  assert.match(buttons, /<PathCopyButton key=\{link\.url\} path=\{link\.url\} label="Copy GitHub link" title=\{link\.copyTitle\}/)
  assert.match(buttons, /queryFn: \(\{ signal \}\) => getGitHub\(root, path, fetch, signal\)/)
})
