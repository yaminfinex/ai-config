import assert from 'node:assert/strict'
import test from 'node:test'
import { pathTokenRangeAt } from '../src/features/files/pathTokenRange.ts'
import { pathFromHref } from '../src/shared/pathHref.ts'

function siblings(...values: (string | null)[]) {
  const nodes = values.map((value) => ({
    nodeType: value === null ? 1 : 3, nodeName: value === null ? 'BR' : '#text', textContent: value ?? '',
    previousSibling: null, nextSibling: null,
  })) as unknown as Node[]
  nodes.forEach((node, index) => Object.assign(node, { previousSibling: nodes[index - 1] ?? null, nextSibling: nodes[index + 1] ?? null }))
  return nodes
}

test('href decoding keeps local paths and excludes URL schemes', () => {
  assert.equal(pathFromHref('file:///home/u/x%20y.md'), '/home/u/x y.md')
  assert.equal(pathFromHref('docs/x%2520y.md:12'), 'docs/x%20y.md:12')
  assert.equal(pathFromHref('/home/u/%ZZ.md'), '/home/u/%ZZ.md')
  for (const href of ['', 'https://a.b/c', 'mailto:u@a.b', 'herder-agent:kila', 'javascript:alert', 'C:/x.md']) assert.equal(pathFromHref(href), null)
})

test('soft wraps map selection across text siblings, a newline, or a BR', () => {
  for (const separator of [[], ['\n'], [null], [null, '\n']] as (string | null)[][]) {
    const nodes = siblings('trail (/home/u/proj/', ...separator, 'sub/x.md) done')
    for (const point of [{ node: nodes[0], offset: 12 }, { node: nodes.at(-1)!, offset: 3 }]) {
      const token = pathTokenRangeAt(point)
      assert.equal(token.text, '/home/u/proj/sub/x.md')
      assert.deepEqual(token.start, { node: nodes[0], offset: 7 })
      assert.deepEqual(token.end, { node: nodes.at(-1), offset: 8 })
    }
  }
})

test('same-node newlines map back to original offsets', () => {
  const [node] = siblings('trail (/home/u/proj/\nsub/x.md) done')
  const token = pathTokenRangeAt({ node, offset: 23 })
  assert.equal(token.text, '/home/u/proj/sub/x.md')
  assert.equal(token.start.offset, 7)
  assert.equal(token.end.offset, node.textContent!.indexOf(')'))
})

test('soft wraps stop at whitespace, blank lines, structural delimiters, and elements', () => {
  for (const separator of [' ', '\n\n', '\n ', ')\n(']) {
    const nodes = siblings('/home/u/proj/', separator, 'sub/x.md')
    assert.equal(pathTokenRangeAt({ node: nodes[0], offset: 3 }).text, '/home/u/proj/')
  }
  const nodes = siblings('/home/u/proj/', null, 'sub/x.md')
  Object.assign(nodes[1], { nodeName: 'EM' })
  assert.equal(pathTokenRangeAt({ node: nodes[0], offset: 3 }).text, '/home/u/proj/')
  const blank = siblings('/home/u/proj/', null, null, 'sub/x.md')
  assert.equal(pathTokenRangeAt({ node: blank[0], offset: 3 }).text, '/home/u/proj/')
  const blankWithFormatting = siblings('/home/u/proj/', null, '\n\n', 'sub/x.md')
  assert.equal(pathTokenRangeAt({ node: blankWithFormatting[0], offset: 3 }).text, '/home/u/proj/')
})

test('fenced code keeps separate lines and inline code keeps its literal spaces', () => {
  const [node] = siblings('/home/u/proj/\nsub/x.md')
  assert.equal(pathTokenRangeAt({ node, offset: 3 }, false, false).text, '/home/u/proj/')
  const [inline] = siblings('docs/my file.md')
  assert.equal(pathTokenRangeAt({ node: inline, offset: 3 }, true).text, 'docs/my file.md')
})
