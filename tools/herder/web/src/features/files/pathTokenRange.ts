import { hasPathSignal, pathTokenSpanAt, structuralDelimiter } from './fileResolution.ts'

type TextPoint = { node: Node, offset: number }
type Character = { value: string, point: TextPoint | null }

const filenameOrLineSuffix = /\.[\p{L}\p{N}]+$|:\d+$/u
// TEXT_NODE's numeric value also works in Node tests without a DOM global.
const isText = (node: Node) => node.nodeType === 3
const isBreak = (node: Node) => node.nodeName === 'BR'

// DOM-range twin of pathTokenSpanAt. Join one logical wrap only when the left
// fragment is already path-like, has no filename/line suffix or trailing period,
// and neither neighbor is structural. The offset map preserves DOM selection.
export function pathTokenRangeAt(point: TextPoint, { renderedCode = false, softWrap = true }: { renderedCode?: boolean, softWrap?: boolean } = {}) {
  if (renderedCode || !softWrap) {
    const token = pathTokenSpanAt(point.node.textContent ?? '', point.offset, renderedCode)
    return { text: token.text, start: { node: point.node, offset: token.start }, end: { node: point.node, offset: token.end } }
  }
  let firstSibling = point.node
  while (firstSibling.previousSibling && (isText(firstSibling.previousSibling) || isBreak(firstSibling.previousSibling))) firstSibling = firstSibling.previousSibling
  const characters: Character[] = []
  let caret = 0
  for (let node: Node | null = firstSibling; node && (isText(node) || isBreak(node)); node = node.nextSibling) {
    if (isBreak(node)) {
      characters.push({ value: '\n', point: null })
      continue
    }
    const text = node.textContent ?? ''
    // ReactMarkdown emits BR followed by a formatting newline for one break.
    const startOffset = node.previousSibling && isBreak(node.previousSibling) && text.startsWith('\n') ? 1 : 0
    if (node === point.node) caret = characters.length + Math.max(0, point.offset - startOffset)
    for (let offset = startOffset; offset < text.length; offset++) characters.push({ value: text[offset], point: { node, offset } })
  }
  const text = characters.map((character) => character.value).join('')
  const joins = new Set<number>()
  let fragmentStart = 0
  for (let index = 0; index < text.length; index++) {
    if (!structuralDelimiter.test(text[index])) continue
    const left = text.slice(fragmentStart, index).replace(/\n/gu, '')
    const extendsPath = hasPathSignal(left, false) && !filenameOrLineSuffix.test(left) && !left.endsWith('.')
    if (text[index] === '\n' && index > 0 && index < text.length - 1 &&
      !structuralDelimiter.test(text[index - 1]) && !structuralDelimiter.test(text[index + 1]) && extendsPath) joins.add(index)
    else fragmentStart = index + 1
  }
  const kept = characters.filter((_, index) => !joins.has(index))
  const joinedCaret = caret - [...joins].filter((index) => index < caret).length
  const token = pathTokenSpanAt(kept.map((character) => character.value).join(''), joinedCaret)
  const start = kept[token.start]?.point ?? point
  const last = kept[token.end - 1]?.point
  return { text: token.text, start, end: last ? { node: last.node, offset: last.offset + 1 } : point }
}
